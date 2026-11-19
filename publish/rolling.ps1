# Single controller for Tencent Cloud CLB + existing single-host rolling publisher.
# No version directories or automatic binary rollback.
# CLB weight 0 stops NEW connections, not existing long-lived connections.
# Use -CheckOnly to verify CLB bindings/health without modifying production.
# The controller must be the ONLY active publisher; the lock is local to this host.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Za-z0-9._-]+$')]
    [string]$Version,
    [ValidateSet('all', 'config-center', 'config-center-access', 'sdk-core', 'sdk-service', 'admin')]
    [string]$Service = 'all',
    [string]$ConfigPath = (Join-Path $PSScriptRoot 'rolling.config.json'),
    [switch]$SkipBuild,
    [switch]$SkipFrontend,
    [switch]$Fast,
    [switch]$CheckOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($Service -eq 'admin' -and $SkipFrontend) { throw 'admin publishing cannot skip frontend build.' }

function Require-String {
    param($Object, [string]$Name)
    $property = $Object.PSObject.Properties[$Name]
    if ($null -eq $property -or $property.Value -isnot [string] -or
        [string]::IsNullOrWhiteSpace([string]$property.Value)) {
        throw "Missing or invalid string '$Name' in rolling configuration."
    }
    return [string]$property.Value
}

if (-not (Test-Path -LiteralPath $ConfigPath -PathType Leaf)) {
    throw "Missing $ConfigPath. Copy rolling.config.example.json to rolling.config.json and fill in real CLB/node IDs."
}
try {
    $cfg = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
} catch {
    throw "Invalid rolling configuration: $($_.Exception.Message)"
}
if ($null -eq $cfg -or $cfg -isnot [pscustomobject]) { throw 'Rolling configuration must be an object.' }

$region = Require-String $cfg 'region'
$lbId = Require-String $cfg 'loadBalancerId'
$remoteDir = Require-String $cfg 'remoteDirectory'
if ($region -notmatch '^[a-z]{2}-[a-z0-9-]+$' -or $lbId -notmatch '^lb-[A-Za-z0-9]+$') {
    throw 'Invalid Tencent Cloud region or CLB ID.'
}
if ($remoteDir -notmatch '^/[A-Za-z0-9_/-]+$' -or $remoteDir -match '(^|/)\.\.?(/|$)') {
    throw 'remoteDirectory must be an absolute safe non-root directory.'
}
$drainSeconds = 90
if ($null -ne $cfg.PSObject.Properties['drainSeconds']) { $drainSeconds = [int]$cfg.drainSeconds }
if ($drainSeconds -lt 90 -or $drainSeconds -gt 3600) {
    throw 'drainSeconds must be 90-3600. Long-lived connections may still remain after this delay.'
}

$bindings = @($cfg.bindings | Where-Object { $null -ne $_ })
$nodes = @($cfg.nodes | Where-Object { $null -ne $_ })
if ($bindings.Count -lt 1 -or $nodes.Count -lt 2) {
    throw 'At least one CLB binding and two application nodes are required.'
}
$bindingKeys = @{}
foreach ($binding in $bindings) {
    $listener = Require-String $binding 'listenerId'
    if ($listener -notmatch '^lbl-[A-Za-z0-9]+$') { throw "Invalid listener ID: $listener" }
    if ($null -eq $binding.PSObject.Properties['backendPort'] -or
        ([string]$binding.backendPort) -notmatch '^[0-9]{1,5}
        throw "Invalid backendPort for $listener."
    }
    if ($null -eq $binding.PSObject.Properties['locationId'] -or
        $binding.locationId -isnot [string] -or
        ($binding.locationId -and $binding.locationId -notmatch '^loc-[A-Za-z0-9]+
    $key = "$listener/$($binding.locationId)"
    if ($bindingKeys.ContainsKey($key)) { throw "Duplicate CLB binding: $key" }
    $bindingKeys[$key] = $true
}
$nodeNames = @{}
$nodeIds = @{}
foreach ($node in $nodes) {
    $name = Require-String $node 'name'
    $hostName = Require-String $node 'host'
    $userName = Require-String $node 'user'
    $instanceId = Require-String $node 'instanceId'
    if ($name -notmatch '^[A-Za-z0-9_-]+$' -or
        $hostName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$' -or
        $userName -notmatch '^[A-Za-z_][A-Za-z0-9_-]*$' -or
        $instanceId -notmatch '^ins-[A-Za-z0-9]+$') {
        throw "Invalid node fields: $name."
    }
    if ($null -eq $node.PSObject.Properties['sshPort'] -or
        ([string]$node.sshPort) -notmatch '^[0-9]{1,5}
        throw "Invalid sshPort for node $name."
    }
    if ($nodeNames.ContainsKey($name) -or $nodeIds.ContainsKey($instanceId)) {
        throw "Duplicate node name or CVM ID: $name."
    }
    $nodeNames[$name] = $true
    $nodeIds[$instanceId] = $true
}
$tccliCommand = Get-Command tccli -ErrorAction Stop
$tccli = $tccliCommand.Source

# CLI JSON arguments are passed via a temporary UTF-8 file to avoid
# Windows PowerShell 5.1 native-command JSON quoting problems.
function Invoke-Clb {
    param([string]$Action, [hashtable]$Arguments)
    $temp = [IO.Path]::GetTempFileName()
    try {
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [IO.File]::WriteAllText($temp, ($Arguments | ConvertTo-Json -Depth 12 -Compress), $utf8)
        $path = [IO.Path]::GetFullPath($temp).Replace('\', '/')
        if ($path -match '^[A-Za-z]:/') { $path = '/' + $path }
        $uri = 'file://' + $path
        $output = @(& $tccli clb $Action --region $region --cli-input-json $uri --output json --timeout 30 2>&1)
        if ($LASTEXITCODE -ne 0) {
            throw "tccli clb $Action failed: $($output -join ' ')"
        }
        $body = ($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine
        $parsed = $body | ConvertFrom-Json
        if ($null -eq $parsed -or $null -eq $parsed.Response) {
            throw "Invalid tccli $Action JSON response."
        }
        if ($null -ne $parsed.Response.Error) {
            throw "CLB $Action API error: $($parsed.Response.Error.Code)"
        }
        return $parsed.Response
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
}

function Get-BindingTargets {
    param($Binding)
    $r = Invoke-Clb 'DescribeTargets' @{
        LoadBalancerId = $lbId
        ListenerIds = @([string]$Binding.listenerId)
    }
    $listeners = @($r.Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { throw "Missing or duplicate CLB listener $($Binding.listenerId)." }
    $listener = $listeners[0]
    if ($Binding.locationId) {
        $rules = @($listener.Rules | Where-Object { $_.LocationId -eq $Binding.locationId })
        if ($rules.Count -ne 1) { throw "Cannot find CLB rule $($Binding.locationId)." }
        $items = @($rules[0].Targets)
    } else {
        if ($listener.Protocol -in @('HTTP', 'HTTPS')) {
            throw "HTTP/HTTPS listener $($Binding.listenerId) requires locationId."
        }
        $items = @($listener.Targets)
    }
    foreach ($item in $items) { if ($null -ne $item) { Write-Output $item } }
}

function Get-Target {
    param($Binding, $Node)
    $matches = @(Get-BindingTargets $Binding | Where-Object {
        $_.InstanceId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    if ($matches.Count -ne 1) {
        throw "CLB target mismatch for $($Node.name) / $($Binding.listenerId):$($Binding.backendPort)."
    }
    if ($null -eq $matches[0].Weight) { throw "Missing CLB weight for $($Node.name)." }
    return $matches[0]
}

function Get-TargetHealth {
    param($Binding, $Node)
    $args = @{
        LoadBalancerIds = @($lbId)
        ListenerIds = @([string]$Binding.listenerId)
    }
    if ($Binding.locationId) { $args.LocationIds = @([string]$Binding.locationId) }
    $r = Invoke-Clb 'DescribeTargetHealth' $args
    $loadBalancers = @($r.LoadBalancers | Where-Object { $_.LoadBalancerId -eq $lbId })
    if ($loadBalancers.Count -ne 1) { return $false }
    $listeners = @($loadBalancers[0].Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { return $false }
    $rules = @($listeners[0].Rules)
    if ($Binding.locationId) {
        $rules = @($rules | Where-Object { $_.LocationId -eq $Binding.locationId })
    }
    $targets = @($rules | ForEach-Object { $_.Targets } | Where-Object { $null -ne $_ })
    $matches = @($targets | Where-Object {
        $_.TargetId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    return ($matches.Count -eq 1 -and $matches[0].HealthStatus -eq $true)
}

function Assert-NodeHealthy {
    param($Node)
    foreach ($binding in $bindings) {
        if (-not (Get-TargetHealth $binding $Node)) {
            throw "CLB health check failed for $($Node.name) on $($binding.listenerId). Ensure CLB health checks are enabled."
        }
    }
}

function Wait-NodeHealthy {
    param($Node)
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $consecutive = 0
    while ([DateTime]::UtcNow -lt $deadline) {
        $ok = $true
        foreach ($binding in $bindings) {
            if (-not (Get-TargetHealth $binding $Node)) { $ok = $false; break }
        }
        if ($ok) {
            $consecutive++
            if ($consecutive -ge 3) { return }
        } else {
            $consecutive = 0
        }
        Start-Sleep -Seconds 3
    }
    throw "CLB health did not recover for $($Node.name)."
}

function Set-Weight {
    param($Binding, $Node, [int]$Weight)
    $args = @{
        LoadBalancerId = $lbId
        ListenerId = [string]$Binding.listenerId
        Targets = @(@{
            InstanceId = [string]$Node.instanceId
            Port = [int]$Binding.backendPort
        })
        Weight = $Weight
    }
    if ($Binding.locationId) { $args.LocationId = [string]$Binding.locationId }
    $r = Invoke-Clb 'ModifyTargetWeight' $args
    if ([string]::IsNullOrWhiteSpace([string]$r.RequestId)) {
        throw "CLB ModifyTargetWeight returned no RequestId for $($Node.name)."
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $done = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        $status = Invoke-Clb 'DescribeTaskStatus' @{ TaskId = [string]$r.RequestId }
        if ($null -eq $status.PSObject.Properties['Status']) { throw 'CLB task response has no Status.' }
        if ([int]$status.Status -eq 0) { $done = $true; break }
        if ([int]$status.Status -eq 1) { throw "CLB task failed: $($status.Message)" }
        if ([int]$status.Status -ne 2) { throw "Unexpected CLB task status $($status.Status)." }
        Start-Sleep -Seconds 2
    }
    if (-not $done) { throw "CLB task timed out for $($Node.name)." }
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        $current = Get-Target $Binding $Node
        if ([int]$current.Weight -eq $Weight) { return }
        Start-Sleep -Seconds 2
    }
    throw "CLB weight verification timed out for $($Node.name), expected $Weight."
}

# This lock prevents concurrent runs from the same Windows machine.
# Do not run a second controller from another workstation/CI concurrently.
$lockPath = Join-Path ([IO.Path]::GetTempPath()) "middle-platform-rolling-$lbId.lock"
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
} catch {
    throw "Another local rolling deployment is active: $lockPath"
}
try {
    $originalWeights = @{}
    Write-Host "Preflight: CLB $lbId in $region, $($nodes.Count) nodes, $($bindings.Count) bindings."
    foreach ($binding in $bindings) {
        $targets = @(Get-BindingTargets $binding)
        if ($targets.Count -ne $nodes.Count) {
            throw "CLB binding $($binding.listenerId) has $($targets.Count) backends but configuration lists $($nodes.Count). Refusing partial coverage."
        }
        foreach ($node in $nodes) {
            $target = Get-Target $binding $node
            $weight = [int]$target.Weight
            if ($weight -le 0 -or $weight -gt 100) {
                throw "Node $($node.name) is not active in CLB (weight $weight)."
            }
            $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
            $originalWeights[$key] = $weight
        }
    }
    foreach ($node in $nodes) { Assert-NodeHealthy $node }
    Write-Host 'Preflight passed. All configured CLB backends are healthy and have positive weight.'
    if ($CheckOnly) {
        Write-Host 'CheckOnly: no package build, CLB change, upload, or restart performed.'
        return
    }

    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'publish.config.json'))) {
        throw 'Existing publish.config.json is required by publish.ps1.'
    }
    if (-not $SkipBuild) {
        Write-Host "Packaging version $Version once..."
        & (Join-Path $PSScriptRoot 'package.ps1') -Version $Version -Service $Service -SkipFrontend:$SkipFrontend -Fast:$Fast
        if (-not $?) { throw 'Local package step failed.' }
    }
    $archive = Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz"
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) {
        throw "Missing Linux amd64 archive: $archive"
    }

    foreach ($node in $nodes) {
        Write-Host "=== Rolling node $($node.name) ($($node.host)) ==="
        foreach ($other in $nodes) {
            if ($other.name -ne $node.name) {
                Assert-NodeHealthy $other
                foreach ($binding in $bindings) {
                    if ([int](Get-Target $binding $other).Weight -le 0) {
                        throw "Another node is drained: $($other.name). Refusing to continue."
                    }
                }
            }
        }
        $changed = $false
        try {
            foreach ($binding in $bindings) {
                # Refuse unexpected/manual weight changes during the deployment.
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                if ([int](Get-Target $binding $node).Weight -ne [int]$originalWeights[$key]) {
                    throw "CLB weight changed externally for $($node.name)."
                }
                $changed = $true
                Set-Weight $binding $node 0
            }
            Write-Host "Node $($node.name): CLB weight is 0 on every configured binding."
            Write-Warning "Waiting $drainSeconds seconds for existing connections. This does not prove long-lived TCP connections are fully drained."
            Start-Sleep -Seconds $drainSeconds
            $arguments = @{
                Version = $Version
                Service = $Service
                SkipBuild = $true
                ServerHost = [string]$node.host
                Port = [int]$node.sshPort
                User = [string]$node.user
                RemoteDirectory = $remoteDir
            }
            & (Join-Path $PSScriptRoot 'publish.ps1') @arguments
            if (-not $?) { throw "Single-host publish failed for $($node.name)." }

            Assert-NodeHealthy $node
            foreach ($binding in $bindings) {
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                Set-Weight $binding $node ([int]$originalWeights[$key])
            }
            Wait-NodeHealthy $node
            Write-Host "Node $($node.name) restored and healthy."
        } catch {
            if ($changed) {
                Write-Error "STOPPED on $($node.name): $($_.Exception.Message). CLB bindings may be partially drained/restored. Inspect target weights manually; no automatic rollback is attempted." -ErrorAction Continue
            }
            throw
        }
    }
    Write-Host "Completed: $($nodes.Count) nodes published successfully."
} finally {
    $lock.Dispose()
}
 -or
        [int]$binding.backendPort -lt 1 -or [int]$binding.backendPort -gt 65535) {
        throw "Invalid backendPort for $listener."
    }
    if ($null -ne $binding.PSObject.Properties['locationId'] -and
        $binding.locationId -and $binding.locationId -notmatch '^loc-[A-Za-z0-9]+$') {
        throw "Invalid locationId for $listener."
    }
    $key = "$listener/$($binding.locationId)"
    if ($bindingKeys.ContainsKey($key)) { throw "Duplicate CLB binding: $key" }
    $bindingKeys[$key] = $true
}
$nodeNames = @{}
$nodeIds = @{}
foreach ($node in $nodes) {
    $name = Require-String $node 'name'
    $hostName = Require-String $node 'host'
    $userName = Require-String $node 'user'
    $instanceId = Require-String $node 'instanceId'
    if ($name -notmatch '^[A-Za-z0-9_-]+$' -or
        $hostName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$' -or
        $userName -notmatch '^[A-Za-z_][A-Za-z0-9_-]*$' -or
        $instanceId -notmatch '^ins-[A-Za-z0-9]+$') {
        throw "Invalid node fields: $name."
    }
    if ($null -eq $node.PSObject.Properties['sshPort'] -or
        $node.sshPort -isnot [int] -or $node.sshPort -lt 1 -or $node.sshPort -gt 65535) {
        throw "Invalid sshPort for node $name."
    }
    if ($nodeNames.ContainsKey($name) -or $nodeIds.ContainsKey($instanceId)) {
        throw "Duplicate node name or CVM ID: $name."
    }
    $nodeNames[$name] = $true
    $nodeIds[$instanceId] = $true
}
$tccliCommand = Get-Command tccli -ErrorAction Stop
$tccli = $tccliCommand.Source

# CLI JSON arguments are passed via a temporary UTF-8 file to avoid
# Windows PowerShell 5.1 native-command JSON quoting problems.
function Invoke-Clb {
    param([string]$Action, [hashtable]$Arguments)
    $temp = [IO.Path]::GetTempFileName()
    try {
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [IO.File]::WriteAllText($temp, ($Arguments | ConvertTo-Json -Depth 12 -Compress), $utf8)
        $path = [IO.Path]::GetFullPath($temp).Replace('\', '/')
        if ($path -match '^[A-Za-z]:/') { $path = '/' + $path }
        $uri = 'file://' + $path
        $output = @(& $tccli clb $Action --region $region --cli-input-json $uri --output json --timeout 30 2>&1)
        if ($LASTEXITCODE -ne 0) {
            throw "tccli clb $Action failed: $($output -join ' ')"
        }
        $body = ($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine
        $parsed = $body | ConvertFrom-Json
        if ($null -eq $parsed -or $null -eq $parsed.Response) {
            throw "Invalid tccli $Action JSON response."
        }
        if ($null -ne $parsed.Response.Error) {
            throw "CLB $Action API error: $($parsed.Response.Error.Code)"
        }
        return $parsed.Response
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
}

function Get-BindingTargets {
    param($Binding)
    $r = Invoke-Clb 'DescribeTargets' @{
        LoadBalancerId = $lbId
        ListenerIds = @([string]$Binding.listenerId)
    }
    $listeners = @($r.Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { throw "Missing or duplicate CLB listener $($Binding.listenerId)." }
    $listener = $listeners[0]
    if ($Binding.locationId) {
        $rules = @($listener.Rules | Where-Object { $_.LocationId -eq $Binding.locationId })
        if ($rules.Count -ne 1) { throw "Cannot find CLB rule $($Binding.locationId)." }
        $items = @($rules[0].Targets)
    } else {
        if ($listener.Protocol -in @('HTTP', 'HTTPS')) {
            throw "HTTP/HTTPS listener $($Binding.listenerId) requires locationId."
        }
        $items = @($listener.Targets)
    }
    foreach ($item in $items) { if ($null -ne $item) { Write-Output $item } }
}

function Get-Target {
    param($Binding, $Node)
    $matches = @(Get-BindingTargets $Binding | Where-Object {
        $_.InstanceId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    if ($matches.Count -ne 1) {
        throw "CLB target mismatch for $($Node.name) / $($Binding.listenerId):$($Binding.backendPort)."
    }
    if ($null -eq $matches[0].Weight) { throw "Missing CLB weight for $($Node.name)." }
    return $matches[0]
}

function Get-TargetHealth {
    param($Binding, $Node)
    $args = @{
        LoadBalancerIds = @($lbId)
        ListenerIds = @([string]$Binding.listenerId)
    }
    if ($Binding.locationId) { $args.LocationIds = @([string]$Binding.locationId) }
    $r = Invoke-Clb 'DescribeTargetHealth' $args
    $loadBalancers = @($r.LoadBalancers | Where-Object { $_.LoadBalancerId -eq $lbId })
    if ($loadBalancers.Count -ne 1) { return $false }
    $listeners = @($loadBalancers[0].Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { return $false }
    $rules = @($listeners[0].Rules)
    if ($Binding.locationId) {
        $rules = @($rules | Where-Object { $_.LocationId -eq $Binding.locationId })
    }
    $targets = @($rules | ForEach-Object { $_.Targets } | Where-Object { $null -ne $_ })
    $matches = @($targets | Where-Object {
        $_.TargetId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    return ($matches.Count -eq 1 -and $matches[0].HealthStatus -eq $true)
}

function Assert-NodeHealthy {
    param($Node)
    foreach ($binding in $bindings) {
        if (-not (Get-TargetHealth $binding $Node)) {
            throw "CLB health check failed for $($Node.name) on $($binding.listenerId). Ensure CLB health checks are enabled."
        }
    }
}

function Wait-NodeHealthy {
    param($Node)
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $consecutive = 0
    while ([DateTime]::UtcNow -lt $deadline) {
        $ok = $true
        foreach ($binding in $bindings) {
            if (-not (Get-TargetHealth $binding $Node)) { $ok = $false; break }
        }
        if ($ok) {
            $consecutive++
            if ($consecutive -ge 3) { return }
        } else {
            $consecutive = 0
        }
        Start-Sleep -Seconds 3
    }
    throw "CLB health did not recover for $($Node.name)."
}

function Set-Weight {
    param($Binding, $Node, [int]$Weight)
    $args = @{
        LoadBalancerId = $lbId
        ListenerId = [string]$Binding.listenerId
        Targets = @(@{
            InstanceId = [string]$Node.instanceId
            Port = [int]$Binding.backendPort
        })
        Weight = $Weight
    }
    if ($Binding.locationId) { $args.LocationId = [string]$Binding.locationId }
    $r = Invoke-Clb 'ModifyTargetWeight' $args
    if ([string]::IsNullOrWhiteSpace([string]$r.RequestId)) {
        throw "CLB ModifyTargetWeight returned no RequestId for $($Node.name)."
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $done = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        $status = Invoke-Clb 'DescribeTaskStatus' @{ TaskId = [string]$r.RequestId }
        if ([int]$status.Status -eq 0) { $done = $true; break }
        if ([int]$status.Status -eq 1) { throw "CLB task failed: $($status.Message)" }
        if ([int]$status.Status -ne 2) { throw "Unexpected CLB task status $($status.Status)." }
        Start-Sleep -Seconds 2
    }
    if (-not $done) { throw "CLB task timed out for $($Node.name)." }
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        $current = Get-Target $Binding $Node
        if ([int]$current.Weight -eq $Weight) { return }
        Start-Sleep -Seconds 2
    }
    throw "CLB weight verification timed out for $($Node.name), expected $Weight."
}

# This lock prevents concurrent runs from the same Windows machine.
# Do not run a second controller from another workstation/CI concurrently.
$lockPath = Join-Path ([IO.Path]::GetTempPath()) "middle-platform-rolling-$lbId.lock"
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
} catch {
    throw "Another local rolling deployment is active: $lockPath"
}
try {
    $originalWeights = @{}
    Write-Host "Preflight: CLB $lbId in $region, $($nodes.Count) nodes, $($bindings.Count) bindings."
    foreach ($binding in $bindings) {
        $targets = @(Get-BindingTargets $binding)
        if ($targets.Count -ne $nodes.Count) {
            throw "CLB binding $($binding.listenerId) has $($targets.Count) backends but configuration lists $($nodes.Count). Refusing partial coverage."
        }
        foreach ($node in $nodes) {
            $target = Get-Target $binding $node
            $weight = [int]$target.Weight
            if ($weight -le 0 -or $weight -gt 100) {
                throw "Node $($node.name) is not active in CLB (weight $weight)."
            }
            $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
            $originalWeights[$key] = $weight
        }
    }
    foreach ($node in $nodes) { Assert-NodeHealthy $node }
    Write-Host 'Preflight passed. All configured CLB backends are healthy and have positive weight.'
    if ($CheckOnly) {
        Write-Host 'CheckOnly: no package build, CLB change, upload, or restart performed.'
        return
    }

    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'publish.config.json'))) {
        throw 'Existing publish.config.json is required by publish.ps1.'
    }
    if (-not $SkipBuild) {
        Write-Host "Packaging version $Version once..."
        & (Join-Path $PSScriptRoot 'package.ps1') -Version $Version -Service $Service -SkipFrontend:$SkipFrontend -Fast:$Fast
        if (-not $?) { throw 'Local package step failed.' }
    }
    $archive = Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz"
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) {
        throw "Missing Linux amd64 archive: $archive"
    }

    foreach ($node in $nodes) {
        Write-Host "=== Rolling node $($node.name) ($($node.host)) ==="
        foreach ($other in $nodes) {
            if ($other.name -ne $node.name) {
                Assert-NodeHealthy $other
                foreach ($binding in $bindings) {
                    if ([int](Get-Target $binding $other).Weight -le 0) {
                        throw "Another node is drained: $($other.name). Refusing to continue."
                    }
                }
            }
        }
        $changed = $false
        try {
            foreach ($binding in $bindings) {
                # Refuse unexpected/manual weight changes during the deployment.
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                if ([int](Get-Target $binding $node).Weight -ne [int]$originalWeights[$key]) {
                    throw "CLB weight changed externally for $($node.name)."
                }
                Set-Weight $binding $node 0
                $changed = $true
            }
            Write-Host "Node $($node.name): CLB weight is 0 on every configured binding."
            Write-Warning "Waiting $drainSeconds seconds for existing connections. This does not prove long-lived TCP connections are fully drained."
            Start-Sleep -Seconds $drainSeconds
            $arguments = @{
                Version = $Version
                Service = $Service
                SkipBuild = $true
                ServerHost = [string]$node.host
                Port = [int]$node.sshPort
                User = [string]$node.user
                RemoteDirectory = $remoteDir
            }
            & (Join-Path $PSScriptRoot 'publish.ps1') @arguments
            if (-not $?) { throw "Single-host publish failed for $($node.name)." }

            Assert-NodeHealthy $node
            foreach ($binding in $bindings) {
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                Set-Weight $binding $node ([int]$originalWeights[$key])
            }
            Wait-NodeHealthy $node
            Write-Host "Node $($node.name) restored and healthy."
        } catch {
            if ($changed) {
                Write-Error "STOPPED on $($node.name): $($_.Exception.Message). CLB bindings may be partially drained/restored. Inspect target weights manually; no automatic rollback is attempted." -ErrorAction Continue
            }
            throw
        }
    }
    Write-Host "Completed: $($nodes.Count) nodes published successfully."
} finally {
    $lock.Dispose()
}
)) {
        throw "Invalid locationId for $listener (use an empty string for TCP listeners)."
    }
    $key = "$listener/$($binding.locationId)"
    if ($bindingKeys.ContainsKey($key)) { throw "Duplicate CLB binding: $key" }
    $bindingKeys[$key] = $true
}
$nodeNames = @{}
$nodeIds = @{}
foreach ($node in $nodes) {
    $name = Require-String $node 'name'
    $hostName = Require-String $node 'host'
    $userName = Require-String $node 'user'
    $instanceId = Require-String $node 'instanceId'
    if ($name -notmatch '^[A-Za-z0-9_-]+$' -or
        $hostName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$' -or
        $userName -notmatch '^[A-Za-z_][A-Za-z0-9_-]*$' -or
        $instanceId -notmatch '^ins-[A-Za-z0-9]+$') {
        throw "Invalid node fields: $name."
    }
    if ($null -eq $node.PSObject.Properties['sshPort'] -or
        $node.sshPort -isnot [int] -or $node.sshPort -lt 1 -or $node.sshPort -gt 65535) {
        throw "Invalid sshPort for node $name."
    }
    if ($nodeNames.ContainsKey($name) -or $nodeIds.ContainsKey($instanceId)) {
        throw "Duplicate node name or CVM ID: $name."
    }
    $nodeNames[$name] = $true
    $nodeIds[$instanceId] = $true
}
$tccliCommand = Get-Command tccli -ErrorAction Stop
$tccli = $tccliCommand.Source

# CLI JSON arguments are passed via a temporary UTF-8 file to avoid
# Windows PowerShell 5.1 native-command JSON quoting problems.
function Invoke-Clb {
    param([string]$Action, [hashtable]$Arguments)
    $temp = [IO.Path]::GetTempFileName()
    try {
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [IO.File]::WriteAllText($temp, ($Arguments | ConvertTo-Json -Depth 12 -Compress), $utf8)
        $path = [IO.Path]::GetFullPath($temp).Replace('\', '/')
        if ($path -match '^[A-Za-z]:/') { $path = '/' + $path }
        $uri = 'file://' + $path
        $output = @(& $tccli clb $Action --region $region --cli-input-json $uri --output json --timeout 30 2>&1)
        if ($LASTEXITCODE -ne 0) {
            throw "tccli clb $Action failed: $($output -join ' ')"
        }
        $body = ($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine
        $parsed = $body | ConvertFrom-Json
        if ($null -eq $parsed -or $null -eq $parsed.Response) {
            throw "Invalid tccli $Action JSON response."
        }
        if ($null -ne $parsed.Response.Error) {
            throw "CLB $Action API error: $($parsed.Response.Error.Code)"
        }
        return $parsed.Response
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
}

function Get-BindingTargets {
    param($Binding)
    $r = Invoke-Clb 'DescribeTargets' @{
        LoadBalancerId = $lbId
        ListenerIds = @([string]$Binding.listenerId)
    }
    $listeners = @($r.Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { throw "Missing or duplicate CLB listener $($Binding.listenerId)." }
    $listener = $listeners[0]
    if ($Binding.locationId) {
        $rules = @($listener.Rules | Where-Object { $_.LocationId -eq $Binding.locationId })
        if ($rules.Count -ne 1) { throw "Cannot find CLB rule $($Binding.locationId)." }
        $items = @($rules[0].Targets)
    } else {
        if ($listener.Protocol -in @('HTTP', 'HTTPS')) {
            throw "HTTP/HTTPS listener $($Binding.listenerId) requires locationId."
        }
        $items = @($listener.Targets)
    }
    foreach ($item in $items) { if ($null -ne $item) { Write-Output $item } }
}

function Get-Target {
    param($Binding, $Node)
    $matches = @(Get-BindingTargets $Binding | Where-Object {
        $_.InstanceId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    if ($matches.Count -ne 1) {
        throw "CLB target mismatch for $($Node.name) / $($Binding.listenerId):$($Binding.backendPort)."
    }
    if ($null -eq $matches[0].Weight) { throw "Missing CLB weight for $($Node.name)." }
    return $matches[0]
}

function Get-TargetHealth {
    param($Binding, $Node)
    $args = @{
        LoadBalancerIds = @($lbId)
        ListenerIds = @([string]$Binding.listenerId)
    }
    if ($Binding.locationId) { $args.LocationIds = @([string]$Binding.locationId) }
    $r = Invoke-Clb 'DescribeTargetHealth' $args
    $loadBalancers = @($r.LoadBalancers | Where-Object { $_.LoadBalancerId -eq $lbId })
    if ($loadBalancers.Count -ne 1) { return $false }
    $listeners = @($loadBalancers[0].Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { return $false }
    $rules = @($listeners[0].Rules)
    if ($Binding.locationId) {
        $rules = @($rules | Where-Object { $_.LocationId -eq $Binding.locationId })
    }
    $targets = @($rules | ForEach-Object { $_.Targets } | Where-Object { $null -ne $_ })
    $matches = @($targets | Where-Object {
        $_.TargetId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    return ($matches.Count -eq 1 -and $matches[0].HealthStatus -eq $true)
}

function Assert-NodeHealthy {
    param($Node)
    foreach ($binding in $bindings) {
        if (-not (Get-TargetHealth $binding $Node)) {
            throw "CLB health check failed for $($Node.name) on $($binding.listenerId). Ensure CLB health checks are enabled."
        }
    }
}

function Wait-NodeHealthy {
    param($Node)
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $consecutive = 0
    while ([DateTime]::UtcNow -lt $deadline) {
        $ok = $true
        foreach ($binding in $bindings) {
            if (-not (Get-TargetHealth $binding $Node)) { $ok = $false; break }
        }
        if ($ok) {
            $consecutive++
            if ($consecutive -ge 3) { return }
        } else {
            $consecutive = 0
        }
        Start-Sleep -Seconds 3
    }
    throw "CLB health did not recover for $($Node.name)."
}

function Set-Weight {
    param($Binding, $Node, [int]$Weight)
    $args = @{
        LoadBalancerId = $lbId
        ListenerId = [string]$Binding.listenerId
        Targets = @(@{
            InstanceId = [string]$Node.instanceId
            Port = [int]$Binding.backendPort
        })
        Weight = $Weight
    }
    if ($Binding.locationId) { $args.LocationId = [string]$Binding.locationId }
    $r = Invoke-Clb 'ModifyTargetWeight' $args
    if ([string]::IsNullOrWhiteSpace([string]$r.RequestId)) {
        throw "CLB ModifyTargetWeight returned no RequestId for $($Node.name)."
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $done = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        $status = Invoke-Clb 'DescribeTaskStatus' @{ TaskId = [string]$r.RequestId }
        if ([int]$status.Status -eq 0) { $done = $true; break }
        if ([int]$status.Status -eq 1) { throw "CLB task failed: $($status.Message)" }
        if ([int]$status.Status -ne 2) { throw "Unexpected CLB task status $($status.Status)." }
        Start-Sleep -Seconds 2
    }
    if (-not $done) { throw "CLB task timed out for $($Node.name)." }
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        $current = Get-Target $Binding $Node
        if ([int]$current.Weight -eq $Weight) { return }
        Start-Sleep -Seconds 2
    }
    throw "CLB weight verification timed out for $($Node.name), expected $Weight."
}

# This lock prevents concurrent runs from the same Windows machine.
# Do not run a second controller from another workstation/CI concurrently.
$lockPath = Join-Path ([IO.Path]::GetTempPath()) "middle-platform-rolling-$lbId.lock"
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
} catch {
    throw "Another local rolling deployment is active: $lockPath"
}
try {
    $originalWeights = @{}
    Write-Host "Preflight: CLB $lbId in $region, $($nodes.Count) nodes, $($bindings.Count) bindings."
    foreach ($binding in $bindings) {
        $targets = @(Get-BindingTargets $binding)
        if ($targets.Count -ne $nodes.Count) {
            throw "CLB binding $($binding.listenerId) has $($targets.Count) backends but configuration lists $($nodes.Count). Refusing partial coverage."
        }
        foreach ($node in $nodes) {
            $target = Get-Target $binding $node
            $weight = [int]$target.Weight
            if ($weight -le 0 -or $weight -gt 100) {
                throw "Node $($node.name) is not active in CLB (weight $weight)."
            }
            $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
            $originalWeights[$key] = $weight
        }
    }
    foreach ($node in $nodes) { Assert-NodeHealthy $node }
    Write-Host 'Preflight passed. All configured CLB backends are healthy and have positive weight.'
    if ($CheckOnly) {
        Write-Host 'CheckOnly: no package build, CLB change, upload, or restart performed.'
        return
    }

    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'publish.config.json'))) {
        throw 'Existing publish.config.json is required by publish.ps1.'
    }
    if (-not $SkipBuild) {
        Write-Host "Packaging version $Version once..."
        & (Join-Path $PSScriptRoot 'package.ps1') -Version $Version -Service $Service -SkipFrontend:$SkipFrontend -Fast:$Fast
        if (-not $?) { throw 'Local package step failed.' }
    }
    $archive = Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz"
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) {
        throw "Missing Linux amd64 archive: $archive"
    }

    foreach ($node in $nodes) {
        Write-Host "=== Rolling node $($node.name) ($($node.host)) ==="
        foreach ($other in $nodes) {
            if ($other.name -ne $node.name) {
                Assert-NodeHealthy $other
                foreach ($binding in $bindings) {
                    if ([int](Get-Target $binding $other).Weight -le 0) {
                        throw "Another node is drained: $($other.name). Refusing to continue."
                    }
                }
            }
        }
        $changed = $false
        try {
            foreach ($binding in $bindings) {
                # Refuse unexpected/manual weight changes during the deployment.
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                if ([int](Get-Target $binding $node).Weight -ne [int]$originalWeights[$key]) {
                    throw "CLB weight changed externally for $($node.name)."
                }
                Set-Weight $binding $node 0
                $changed = $true
            }
            Write-Host "Node $($node.name): CLB weight is 0 on every configured binding."
            Write-Warning "Waiting $drainSeconds seconds for existing connections. This does not prove long-lived TCP connections are fully drained."
            Start-Sleep -Seconds $drainSeconds
            $arguments = @{
                Version = $Version
                Service = $Service
                SkipBuild = $true
                ServerHost = [string]$node.host
                Port = [int]$node.sshPort
                User = [string]$node.user
                RemoteDirectory = $remoteDir
            }
            & (Join-Path $PSScriptRoot 'publish.ps1') @arguments
            if (-not $?) { throw "Single-host publish failed for $($node.name)." }

            Assert-NodeHealthy $node
            foreach ($binding in $bindings) {
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                Set-Weight $binding $node ([int]$originalWeights[$key])
            }
            Wait-NodeHealthy $node
            Write-Host "Node $($node.name) restored and healthy."
        } catch {
            if ($changed) {
                Write-Error "STOPPED on $($node.name): $($_.Exception.Message). CLB bindings may be partially drained/restored. Inspect target weights manually; no automatic rollback is attempted." -ErrorAction Continue
            }
            throw
        }
    }
    Write-Host "Completed: $($nodes.Count) nodes published successfully."
} finally {
    $lock.Dispose()
}
 -or
        [int]$binding.backendPort -lt 1 -or [int]$binding.backendPort -gt 65535) {
        throw "Invalid backendPort for $listener."
    }
    if ($null -ne $binding.PSObject.Properties['locationId'] -and
        $binding.locationId -and $binding.locationId -notmatch '^loc-[A-Za-z0-9]+$') {
        throw "Invalid locationId for $listener."
    }
    $key = "$listener/$($binding.locationId)"
    if ($bindingKeys.ContainsKey($key)) { throw "Duplicate CLB binding: $key" }
    $bindingKeys[$key] = $true
}
$nodeNames = @{}
$nodeIds = @{}
foreach ($node in $nodes) {
    $name = Require-String $node 'name'
    $hostName = Require-String $node 'host'
    $userName = Require-String $node 'user'
    $instanceId = Require-String $node 'instanceId'
    if ($name -notmatch '^[A-Za-z0-9_-]+$' -or
        $hostName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$' -or
        $userName -notmatch '^[A-Za-z_][A-Za-z0-9_-]*$' -or
        $instanceId -notmatch '^ins-[A-Za-z0-9]+$') {
        throw "Invalid node fields: $name."
    }
    if ($null -eq $node.PSObject.Properties['sshPort'] -or
        $node.sshPort -isnot [int] -or $node.sshPort -lt 1 -or $node.sshPort -gt 65535) {
        throw "Invalid sshPort for node $name."
    }
    if ($nodeNames.ContainsKey($name) -or $nodeIds.ContainsKey($instanceId)) {
        throw "Duplicate node name or CVM ID: $name."
    }
    $nodeNames[$name] = $true
    $nodeIds[$instanceId] = $true
}
$tccliCommand = Get-Command tccli -ErrorAction Stop
$tccli = $tccliCommand.Source

# CLI JSON arguments are passed via a temporary UTF-8 file to avoid
# Windows PowerShell 5.1 native-command JSON quoting problems.
function Invoke-Clb {
    param([string]$Action, [hashtable]$Arguments)
    $temp = [IO.Path]::GetTempFileName()
    try {
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [IO.File]::WriteAllText($temp, ($Arguments | ConvertTo-Json -Depth 12 -Compress), $utf8)
        $path = [IO.Path]::GetFullPath($temp).Replace('\', '/')
        if ($path -match '^[A-Za-z]:/') { $path = '/' + $path }
        $uri = 'file://' + $path
        $output = @(& $tccli clb $Action --region $region --cli-input-json $uri --output json --timeout 30 2>&1)
        if ($LASTEXITCODE -ne 0) {
            throw "tccli clb $Action failed: $($output -join ' ')"
        }
        $body = ($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine
        $parsed = $body | ConvertFrom-Json
        if ($null -eq $parsed -or $null -eq $parsed.Response) {
            throw "Invalid tccli $Action JSON response."
        }
        if ($null -ne $parsed.Response.Error) {
            throw "CLB $Action API error: $($parsed.Response.Error.Code)"
        }
        return $parsed.Response
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
}

function Get-BindingTargets {
    param($Binding)
    $r = Invoke-Clb 'DescribeTargets' @{
        LoadBalancerId = $lbId
        ListenerIds = @([string]$Binding.listenerId)
    }
    $listeners = @($r.Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { throw "Missing or duplicate CLB listener $($Binding.listenerId)." }
    $listener = $listeners[0]
    if ($Binding.locationId) {
        $rules = @($listener.Rules | Where-Object { $_.LocationId -eq $Binding.locationId })
        if ($rules.Count -ne 1) { throw "Cannot find CLB rule $($Binding.locationId)." }
        $items = @($rules[0].Targets)
    } else {
        if ($listener.Protocol -in @('HTTP', 'HTTPS')) {
            throw "HTTP/HTTPS listener $($Binding.listenerId) requires locationId."
        }
        $items = @($listener.Targets)
    }
    foreach ($item in $items) { if ($null -ne $item) { Write-Output $item } }
}

function Get-Target {
    param($Binding, $Node)
    $matches = @(Get-BindingTargets $Binding | Where-Object {
        $_.InstanceId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    if ($matches.Count -ne 1) {
        throw "CLB target mismatch for $($Node.name) / $($Binding.listenerId):$($Binding.backendPort)."
    }
    if ($null -eq $matches[0].Weight) { throw "Missing CLB weight for $($Node.name)." }
    return $matches[0]
}

function Get-TargetHealth {
    param($Binding, $Node)
    $args = @{
        LoadBalancerIds = @($lbId)
        ListenerIds = @([string]$Binding.listenerId)
    }
    if ($Binding.locationId) { $args.LocationIds = @([string]$Binding.locationId) }
    $r = Invoke-Clb 'DescribeTargetHealth' $args
    $loadBalancers = @($r.LoadBalancers | Where-Object { $_.LoadBalancerId -eq $lbId })
    if ($loadBalancers.Count -ne 1) { return $false }
    $listeners = @($loadBalancers[0].Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { return $false }
    $rules = @($listeners[0].Rules)
    if ($Binding.locationId) {
        $rules = @($rules | Where-Object { $_.LocationId -eq $Binding.locationId })
    }
    $targets = @($rules | ForEach-Object { $_.Targets } | Where-Object { $null -ne $_ })
    $matches = @($targets | Where-Object {
        $_.TargetId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    return ($matches.Count -eq 1 -and $matches[0].HealthStatus -eq $true)
}

function Assert-NodeHealthy {
    param($Node)
    foreach ($binding in $bindings) {
        if (-not (Get-TargetHealth $binding $Node)) {
            throw "CLB health check failed for $($Node.name) on $($binding.listenerId). Ensure CLB health checks are enabled."
        }
    }
}

function Wait-NodeHealthy {
    param($Node)
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $consecutive = 0
    while ([DateTime]::UtcNow -lt $deadline) {
        $ok = $true
        foreach ($binding in $bindings) {
            if (-not (Get-TargetHealth $binding $Node)) { $ok = $false; break }
        }
        if ($ok) {
            $consecutive++
            if ($consecutive -ge 3) { return }
        } else {
            $consecutive = 0
        }
        Start-Sleep -Seconds 3
    }
    throw "CLB health did not recover for $($Node.name)."
}

function Set-Weight {
    param($Binding, $Node, [int]$Weight)
    $args = @{
        LoadBalancerId = $lbId
        ListenerId = [string]$Binding.listenerId
        Targets = @(@{
            InstanceId = [string]$Node.instanceId
            Port = [int]$Binding.backendPort
        })
        Weight = $Weight
    }
    if ($Binding.locationId) { $args.LocationId = [string]$Binding.locationId }
    $r = Invoke-Clb 'ModifyTargetWeight' $args
    if ([string]::IsNullOrWhiteSpace([string]$r.RequestId)) {
        throw "CLB ModifyTargetWeight returned no RequestId for $($Node.name)."
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $done = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        $status = Invoke-Clb 'DescribeTaskStatus' @{ TaskId = [string]$r.RequestId }
        if ([int]$status.Status -eq 0) { $done = $true; break }
        if ([int]$status.Status -eq 1) { throw "CLB task failed: $($status.Message)" }
        if ([int]$status.Status -ne 2) { throw "Unexpected CLB task status $($status.Status)." }
        Start-Sleep -Seconds 2
    }
    if (-not $done) { throw "CLB task timed out for $($Node.name)." }
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        $current = Get-Target $Binding $Node
        if ([int]$current.Weight -eq $Weight) { return }
        Start-Sleep -Seconds 2
    }
    throw "CLB weight verification timed out for $($Node.name), expected $Weight."
}

# This lock prevents concurrent runs from the same Windows machine.
# Do not run a second controller from another workstation/CI concurrently.
$lockPath = Join-Path ([IO.Path]::GetTempPath()) "middle-platform-rolling-$lbId.lock"
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
} catch {
    throw "Another local rolling deployment is active: $lockPath"
}
try {
    $originalWeights = @{}
    Write-Host "Preflight: CLB $lbId in $region, $($nodes.Count) nodes, $($bindings.Count) bindings."
    foreach ($binding in $bindings) {
        $targets = @(Get-BindingTargets $binding)
        if ($targets.Count -ne $nodes.Count) {
            throw "CLB binding $($binding.listenerId) has $($targets.Count) backends but configuration lists $($nodes.Count). Refusing partial coverage."
        }
        foreach ($node in $nodes) {
            $target = Get-Target $binding $node
            $weight = [int]$target.Weight
            if ($weight -le 0 -or $weight -gt 100) {
                throw "Node $($node.name) is not active in CLB (weight $weight)."
            }
            $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
            $originalWeights[$key] = $weight
        }
    }
    foreach ($node in $nodes) { Assert-NodeHealthy $node }
    Write-Host 'Preflight passed. All configured CLB backends are healthy and have positive weight.'
    if ($CheckOnly) {
        Write-Host 'CheckOnly: no package build, CLB change, upload, or restart performed.'
        return
    }

    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'publish.config.json'))) {
        throw 'Existing publish.config.json is required by publish.ps1.'
    }
    if (-not $SkipBuild) {
        Write-Host "Packaging version $Version once..."
        & (Join-Path $PSScriptRoot 'package.ps1') -Version $Version -Service $Service -SkipFrontend:$SkipFrontend -Fast:$Fast
        if (-not $?) { throw 'Local package step failed.' }
    }
    $archive = Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz"
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) {
        throw "Missing Linux amd64 archive: $archive"
    }

    foreach ($node in $nodes) {
        Write-Host "=== Rolling node $($node.name) ($($node.host)) ==="
        foreach ($other in $nodes) {
            if ($other.name -ne $node.name) {
                Assert-NodeHealthy $other
                foreach ($binding in $bindings) {
                    if ([int](Get-Target $binding $other).Weight -le 0) {
                        throw "Another node is drained: $($other.name). Refusing to continue."
                    }
                }
            }
        }
        $changed = $false
        try {
            foreach ($binding in $bindings) {
                # Refuse unexpected/manual weight changes during the deployment.
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                if ([int](Get-Target $binding $node).Weight -ne [int]$originalWeights[$key]) {
                    throw "CLB weight changed externally for $($node.name)."
                }
                Set-Weight $binding $node 0
                $changed = $true
            }
            Write-Host "Node $($node.name): CLB weight is 0 on every configured binding."
            Write-Warning "Waiting $drainSeconds seconds for existing connections. This does not prove long-lived TCP connections are fully drained."
            Start-Sleep -Seconds $drainSeconds
            $arguments = @{
                Version = $Version
                Service = $Service
                SkipBuild = $true
                ServerHost = [string]$node.host
                Port = [int]$node.sshPort
                User = [string]$node.user
                RemoteDirectory = $remoteDir
            }
            & (Join-Path $PSScriptRoot 'publish.ps1') @arguments
            if (-not $?) { throw "Single-host publish failed for $($node.name)." }

            Assert-NodeHealthy $node
            foreach ($binding in $bindings) {
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                Set-Weight $binding $node ([int]$originalWeights[$key])
            }
            Wait-NodeHealthy $node
            Write-Host "Node $($node.name) restored and healthy."
        } catch {
            if ($changed) {
                Write-Error "STOPPED on $($node.name): $($_.Exception.Message). CLB bindings may be partially drained/restored. Inspect target weights manually; no automatic rollback is attempted." -ErrorAction Continue
            }
            throw
        }
    }
    Write-Host "Completed: $($nodes.Count) nodes published successfully."
} finally {
    $lock.Dispose()
}
 -or
        [int]$node.sshPort -lt 1 -or [int]$node.sshPort -gt 65535) {
        throw "Invalid sshPort for node $name."
    }
    if ($nodeNames.ContainsKey($name) -or $nodeIds.ContainsKey($instanceId)) {
        throw "Duplicate node name or CVM ID: $name."
    }
    $nodeNames[$name] = $true
    $nodeIds[$instanceId] = $true
}
$tccliCommand = Get-Command tccli -ErrorAction Stop
$tccli = $tccliCommand.Source

# CLI JSON arguments are passed via a temporary UTF-8 file to avoid
# Windows PowerShell 5.1 native-command JSON quoting problems.
function Invoke-Clb {
    param([string]$Action, [hashtable]$Arguments)
    $temp = [IO.Path]::GetTempFileName()
    try {
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [IO.File]::WriteAllText($temp, ($Arguments | ConvertTo-Json -Depth 12 -Compress), $utf8)
        $path = [IO.Path]::GetFullPath($temp).Replace('\', '/')
        if ($path -match '^[A-Za-z]:/') { $path = '/' + $path }
        $uri = 'file://' + $path
        $output = @(& $tccli clb $Action --region $region --cli-input-json $uri --output json --timeout 30 2>&1)
        if ($LASTEXITCODE -ne 0) {
            throw "tccli clb $Action failed: $($output -join ' ')"
        }
        $body = ($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine
        $parsed = $body | ConvertFrom-Json
        if ($null -eq $parsed -or $null -eq $parsed.Response) {
            throw "Invalid tccli $Action JSON response."
        }
        if ($null -ne $parsed.Response.Error) {
            throw "CLB $Action API error: $($parsed.Response.Error.Code)"
        }
        return $parsed.Response
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
}

function Get-BindingTargets {
    param($Binding)
    $r = Invoke-Clb 'DescribeTargets' @{
        LoadBalancerId = $lbId
        ListenerIds = @([string]$Binding.listenerId)
    }
    $listeners = @($r.Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { throw "Missing or duplicate CLB listener $($Binding.listenerId)." }
    $listener = $listeners[0]
    if ($Binding.locationId) {
        $rules = @($listener.Rules | Where-Object { $_.LocationId -eq $Binding.locationId })
        if ($rules.Count -ne 1) { throw "Cannot find CLB rule $($Binding.locationId)." }
        $items = @($rules[0].Targets)
    } else {
        if ($listener.Protocol -in @('HTTP', 'HTTPS')) {
            throw "HTTP/HTTPS listener $($Binding.listenerId) requires locationId."
        }
        $items = @($listener.Targets)
    }
    foreach ($item in $items) { if ($null -ne $item) { Write-Output $item } }
}

function Get-Target {
    param($Binding, $Node)
    $matches = @(Get-BindingTargets $Binding | Where-Object {
        $_.InstanceId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    if ($matches.Count -ne 1) {
        throw "CLB target mismatch for $($Node.name) / $($Binding.listenerId):$($Binding.backendPort)."
    }
    if ($null -eq $matches[0].Weight) { throw "Missing CLB weight for $($Node.name)." }
    return $matches[0]
}

function Get-TargetHealth {
    param($Binding, $Node)
    $args = @{
        LoadBalancerIds = @($lbId)
        ListenerIds = @([string]$Binding.listenerId)
    }
    if ($Binding.locationId) { $args.LocationIds = @([string]$Binding.locationId) }
    $r = Invoke-Clb 'DescribeTargetHealth' $args
    $loadBalancers = @($r.LoadBalancers | Where-Object { $_.LoadBalancerId -eq $lbId })
    if ($loadBalancers.Count -ne 1) { return $false }
    $listeners = @($loadBalancers[0].Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { return $false }
    $rules = @($listeners[0].Rules)
    if ($Binding.locationId) {
        $rules = @($rules | Where-Object { $_.LocationId -eq $Binding.locationId })
    }
    $targets = @($rules | ForEach-Object { $_.Targets } | Where-Object { $null -ne $_ })
    $matches = @($targets | Where-Object {
        $_.TargetId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    return ($matches.Count -eq 1 -and $matches[0].HealthStatus -eq $true)
}

function Assert-NodeHealthy {
    param($Node)
    foreach ($binding in $bindings) {
        if (-not (Get-TargetHealth $binding $Node)) {
            throw "CLB health check failed for $($Node.name) on $($binding.listenerId). Ensure CLB health checks are enabled."
        }
    }
}

function Wait-NodeHealthy {
    param($Node)
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $consecutive = 0
    while ([DateTime]::UtcNow -lt $deadline) {
        $ok = $true
        foreach ($binding in $bindings) {
            if (-not (Get-TargetHealth $binding $Node)) { $ok = $false; break }
        }
        if ($ok) {
            $consecutive++
            if ($consecutive -ge 3) { return }
        } else {
            $consecutive = 0
        }
        Start-Sleep -Seconds 3
    }
    throw "CLB health did not recover for $($Node.name)."
}

function Set-Weight {
    param($Binding, $Node, [int]$Weight)
    $args = @{
        LoadBalancerId = $lbId
        ListenerId = [string]$Binding.listenerId
        Targets = @(@{
            InstanceId = [string]$Node.instanceId
            Port = [int]$Binding.backendPort
        })
        Weight = $Weight
    }
    if ($Binding.locationId) { $args.LocationId = [string]$Binding.locationId }
    $r = Invoke-Clb 'ModifyTargetWeight' $args
    if ([string]::IsNullOrWhiteSpace([string]$r.RequestId)) {
        throw "CLB ModifyTargetWeight returned no RequestId for $($Node.name)."
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $done = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        $status = Invoke-Clb 'DescribeTaskStatus' @{ TaskId = [string]$r.RequestId }
        if ([int]$status.Status -eq 0) { $done = $true; break }
        if ([int]$status.Status -eq 1) { throw "CLB task failed: $($status.Message)" }
        if ([int]$status.Status -ne 2) { throw "Unexpected CLB task status $($status.Status)." }
        Start-Sleep -Seconds 2
    }
    if (-not $done) { throw "CLB task timed out for $($Node.name)." }
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        $current = Get-Target $Binding $Node
        if ([int]$current.Weight -eq $Weight) { return }
        Start-Sleep -Seconds 2
    }
    throw "CLB weight verification timed out for $($Node.name), expected $Weight."
}

# This lock prevents concurrent runs from the same Windows machine.
# Do not run a second controller from another workstation/CI concurrently.
$lockPath = Join-Path ([IO.Path]::GetTempPath()) "middle-platform-rolling-$lbId.lock"
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
} catch {
    throw "Another local rolling deployment is active: $lockPath"
}
try {
    $originalWeights = @{}
    Write-Host "Preflight: CLB $lbId in $region, $($nodes.Count) nodes, $($bindings.Count) bindings."
    foreach ($binding in $bindings) {
        $targets = @(Get-BindingTargets $binding)
        if ($targets.Count -ne $nodes.Count) {
            throw "CLB binding $($binding.listenerId) has $($targets.Count) backends but configuration lists $($nodes.Count). Refusing partial coverage."
        }
        foreach ($node in $nodes) {
            $target = Get-Target $binding $node
            $weight = [int]$target.Weight
            if ($weight -le 0 -or $weight -gt 100) {
                throw "Node $($node.name) is not active in CLB (weight $weight)."
            }
            $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
            $originalWeights[$key] = $weight
        }
    }
    foreach ($node in $nodes) { Assert-NodeHealthy $node }
    Write-Host 'Preflight passed. All configured CLB backends are healthy and have positive weight.'
    if ($CheckOnly) {
        Write-Host 'CheckOnly: no package build, CLB change, upload, or restart performed.'
        return
    }

    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'publish.config.json'))) {
        throw 'Existing publish.config.json is required by publish.ps1.'
    }
    if (-not $SkipBuild) {
        Write-Host "Packaging version $Version once..."
        & (Join-Path $PSScriptRoot 'package.ps1') -Version $Version -Service $Service -SkipFrontend:$SkipFrontend -Fast:$Fast
        if (-not $?) { throw 'Local package step failed.' }
    }
    $archive = Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz"
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) {
        throw "Missing Linux amd64 archive: $archive"
    }

    foreach ($node in $nodes) {
        Write-Host "=== Rolling node $($node.name) ($($node.host)) ==="
        foreach ($other in $nodes) {
            if ($other.name -ne $node.name) {
                Assert-NodeHealthy $other
                foreach ($binding in $bindings) {
                    if ([int](Get-Target $binding $other).Weight -le 0) {
                        throw "Another node is drained: $($other.name). Refusing to continue."
                    }
                }
            }
        }
        $changed = $false
        try {
            foreach ($binding in $bindings) {
                # Refuse unexpected/manual weight changes during the deployment.
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                if ([int](Get-Target $binding $node).Weight -ne [int]$originalWeights[$key]) {
                    throw "CLB weight changed externally for $($node.name)."
                }
                Set-Weight $binding $node 0
                $changed = $true
            }
            Write-Host "Node $($node.name): CLB weight is 0 on every configured binding."
            Write-Warning "Waiting $drainSeconds seconds for existing connections. This does not prove long-lived TCP connections are fully drained."
            Start-Sleep -Seconds $drainSeconds
            $arguments = @{
                Version = $Version
                Service = $Service
                SkipBuild = $true
                ServerHost = [string]$node.host
                Port = [int]$node.sshPort
                User = [string]$node.user
                RemoteDirectory = $remoteDir
            }
            & (Join-Path $PSScriptRoot 'publish.ps1') @arguments
            if (-not $?) { throw "Single-host publish failed for $($node.name)." }

            Assert-NodeHealthy $node
            foreach ($binding in $bindings) {
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                Set-Weight $binding $node ([int]$originalWeights[$key])
            }
            Wait-NodeHealthy $node
            Write-Host "Node $($node.name) restored and healthy."
        } catch {
            if ($changed) {
                Write-Error "STOPPED on $($node.name): $($_.Exception.Message). CLB bindings may be partially drained/restored. Inspect target weights manually; no automatic rollback is attempted." -ErrorAction Continue
            }
            throw
        }
    }
    Write-Host "Completed: $($nodes.Count) nodes published successfully."
} finally {
    $lock.Dispose()
}
 -or
        [int]$binding.backendPort -lt 1 -or [int]$binding.backendPort -gt 65535) {
        throw "Invalid backendPort for $listener."
    }
    if ($null -ne $binding.PSObject.Properties['locationId'] -and
        $binding.locationId -and $binding.locationId -notmatch '^loc-[A-Za-z0-9]+$') {
        throw "Invalid locationId for $listener."
    }
    $key = "$listener/$($binding.locationId)"
    if ($bindingKeys.ContainsKey($key)) { throw "Duplicate CLB binding: $key" }
    $bindingKeys[$key] = $true
}
$nodeNames = @{}
$nodeIds = @{}
foreach ($node in $nodes) {
    $name = Require-String $node 'name'
    $hostName = Require-String $node 'host'
    $userName = Require-String $node 'user'
    $instanceId = Require-String $node 'instanceId'
    if ($name -notmatch '^[A-Za-z0-9_-]+$' -or
        $hostName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$' -or
        $userName -notmatch '^[A-Za-z_][A-Za-z0-9_-]*$' -or
        $instanceId -notmatch '^ins-[A-Za-z0-9]+$') {
        throw "Invalid node fields: $name."
    }
    if ($null -eq $node.PSObject.Properties['sshPort'] -or
        $node.sshPort -isnot [int] -or $node.sshPort -lt 1 -or $node.sshPort -gt 65535) {
        throw "Invalid sshPort for node $name."
    }
    if ($nodeNames.ContainsKey($name) -or $nodeIds.ContainsKey($instanceId)) {
        throw "Duplicate node name or CVM ID: $name."
    }
    $nodeNames[$name] = $true
    $nodeIds[$instanceId] = $true
}
$tccliCommand = Get-Command tccli -ErrorAction Stop
$tccli = $tccliCommand.Source

# CLI JSON arguments are passed via a temporary UTF-8 file to avoid
# Windows PowerShell 5.1 native-command JSON quoting problems.
function Invoke-Clb {
    param([string]$Action, [hashtable]$Arguments)
    $temp = [IO.Path]::GetTempFileName()
    try {
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [IO.File]::WriteAllText($temp, ($Arguments | ConvertTo-Json -Depth 12 -Compress), $utf8)
        $path = [IO.Path]::GetFullPath($temp).Replace('\', '/')
        if ($path -match '^[A-Za-z]:/') { $path = '/' + $path }
        $uri = 'file://' + $path
        $output = @(& $tccli clb $Action --region $region --cli-input-json $uri --output json --timeout 30 2>&1)
        if ($LASTEXITCODE -ne 0) {
            throw "tccli clb $Action failed: $($output -join ' ')"
        }
        $body = ($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine
        $parsed = $body | ConvertFrom-Json
        if ($null -eq $parsed -or $null -eq $parsed.Response) {
            throw "Invalid tccli $Action JSON response."
        }
        if ($null -ne $parsed.Response.Error) {
            throw "CLB $Action API error: $($parsed.Response.Error.Code)"
        }
        return $parsed.Response
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
}

function Get-BindingTargets {
    param($Binding)
    $r = Invoke-Clb 'DescribeTargets' @{
        LoadBalancerId = $lbId
        ListenerIds = @([string]$Binding.listenerId)
    }
    $listeners = @($r.Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { throw "Missing or duplicate CLB listener $($Binding.listenerId)." }
    $listener = $listeners[0]
    if ($Binding.locationId) {
        $rules = @($listener.Rules | Where-Object { $_.LocationId -eq $Binding.locationId })
        if ($rules.Count -ne 1) { throw "Cannot find CLB rule $($Binding.locationId)." }
        $items = @($rules[0].Targets)
    } else {
        if ($listener.Protocol -in @('HTTP', 'HTTPS')) {
            throw "HTTP/HTTPS listener $($Binding.listenerId) requires locationId."
        }
        $items = @($listener.Targets)
    }
    foreach ($item in $items) { if ($null -ne $item) { Write-Output $item } }
}

function Get-Target {
    param($Binding, $Node)
    $matches = @(Get-BindingTargets $Binding | Where-Object {
        $_.InstanceId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    if ($matches.Count -ne 1) {
        throw "CLB target mismatch for $($Node.name) / $($Binding.listenerId):$($Binding.backendPort)."
    }
    if ($null -eq $matches[0].Weight) { throw "Missing CLB weight for $($Node.name)." }
    return $matches[0]
}

function Get-TargetHealth {
    param($Binding, $Node)
    $args = @{
        LoadBalancerIds = @($lbId)
        ListenerIds = @([string]$Binding.listenerId)
    }
    if ($Binding.locationId) { $args.LocationIds = @([string]$Binding.locationId) }
    $r = Invoke-Clb 'DescribeTargetHealth' $args
    $loadBalancers = @($r.LoadBalancers | Where-Object { $_.LoadBalancerId -eq $lbId })
    if ($loadBalancers.Count -ne 1) { return $false }
    $listeners = @($loadBalancers[0].Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { return $false }
    $rules = @($listeners[0].Rules)
    if ($Binding.locationId) {
        $rules = @($rules | Where-Object { $_.LocationId -eq $Binding.locationId })
    }
    $targets = @($rules | ForEach-Object { $_.Targets } | Where-Object { $null -ne $_ })
    $matches = @($targets | Where-Object {
        $_.TargetId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    return ($matches.Count -eq 1 -and $matches[0].HealthStatus -eq $true)
}

function Assert-NodeHealthy {
    param($Node)
    foreach ($binding in $bindings) {
        if (-not (Get-TargetHealth $binding $Node)) {
            throw "CLB health check failed for $($Node.name) on $($binding.listenerId). Ensure CLB health checks are enabled."
        }
    }
}

function Wait-NodeHealthy {
    param($Node)
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $consecutive = 0
    while ([DateTime]::UtcNow -lt $deadline) {
        $ok = $true
        foreach ($binding in $bindings) {
            if (-not (Get-TargetHealth $binding $Node)) { $ok = $false; break }
        }
        if ($ok) {
            $consecutive++
            if ($consecutive -ge 3) { return }
        } else {
            $consecutive = 0
        }
        Start-Sleep -Seconds 3
    }
    throw "CLB health did not recover for $($Node.name)."
}

function Set-Weight {
    param($Binding, $Node, [int]$Weight)
    $args = @{
        LoadBalancerId = $lbId
        ListenerId = [string]$Binding.listenerId
        Targets = @(@{
            InstanceId = [string]$Node.instanceId
            Port = [int]$Binding.backendPort
        })
        Weight = $Weight
    }
    if ($Binding.locationId) { $args.LocationId = [string]$Binding.locationId }
    $r = Invoke-Clb 'ModifyTargetWeight' $args
    if ([string]::IsNullOrWhiteSpace([string]$r.RequestId)) {
        throw "CLB ModifyTargetWeight returned no RequestId for $($Node.name)."
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $done = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        $status = Invoke-Clb 'DescribeTaskStatus' @{ TaskId = [string]$r.RequestId }
        if ([int]$status.Status -eq 0) { $done = $true; break }
        if ([int]$status.Status -eq 1) { throw "CLB task failed: $($status.Message)" }
        if ([int]$status.Status -ne 2) { throw "Unexpected CLB task status $($status.Status)." }
        Start-Sleep -Seconds 2
    }
    if (-not $done) { throw "CLB task timed out for $($Node.name)." }
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        $current = Get-Target $Binding $Node
        if ([int]$current.Weight -eq $Weight) { return }
        Start-Sleep -Seconds 2
    }
    throw "CLB weight verification timed out for $($Node.name), expected $Weight."
}

# This lock prevents concurrent runs from the same Windows machine.
# Do not run a second controller from another workstation/CI concurrently.
$lockPath = Join-Path ([IO.Path]::GetTempPath()) "middle-platform-rolling-$lbId.lock"
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
} catch {
    throw "Another local rolling deployment is active: $lockPath"
}
try {
    $originalWeights = @{}
    Write-Host "Preflight: CLB $lbId in $region, $($nodes.Count) nodes, $($bindings.Count) bindings."
    foreach ($binding in $bindings) {
        $targets = @(Get-BindingTargets $binding)
        if ($targets.Count -ne $nodes.Count) {
            throw "CLB binding $($binding.listenerId) has $($targets.Count) backends but configuration lists $($nodes.Count). Refusing partial coverage."
        }
        foreach ($node in $nodes) {
            $target = Get-Target $binding $node
            $weight = [int]$target.Weight
            if ($weight -le 0 -or $weight -gt 100) {
                throw "Node $($node.name) is not active in CLB (weight $weight)."
            }
            $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
            $originalWeights[$key] = $weight
        }
    }
    foreach ($node in $nodes) { Assert-NodeHealthy $node }
    Write-Host 'Preflight passed. All configured CLB backends are healthy and have positive weight.'
    if ($CheckOnly) {
        Write-Host 'CheckOnly: no package build, CLB change, upload, or restart performed.'
        return
    }

    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'publish.config.json'))) {
        throw 'Existing publish.config.json is required by publish.ps1.'
    }
    if (-not $SkipBuild) {
        Write-Host "Packaging version $Version once..."
        & (Join-Path $PSScriptRoot 'package.ps1') -Version $Version -Service $Service -SkipFrontend:$SkipFrontend -Fast:$Fast
        if (-not $?) { throw 'Local package step failed.' }
    }
    $archive = Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz"
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) {
        throw "Missing Linux amd64 archive: $archive"
    }

    foreach ($node in $nodes) {
        Write-Host "=== Rolling node $($node.name) ($($node.host)) ==="
        foreach ($other in $nodes) {
            if ($other.name -ne $node.name) {
                Assert-NodeHealthy $other
                foreach ($binding in $bindings) {
                    if ([int](Get-Target $binding $other).Weight -le 0) {
                        throw "Another node is drained: $($other.name). Refusing to continue."
                    }
                }
            }
        }
        $changed = $false
        try {
            foreach ($binding in $bindings) {
                # Refuse unexpected/manual weight changes during the deployment.
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                if ([int](Get-Target $binding $node).Weight -ne [int]$originalWeights[$key]) {
                    throw "CLB weight changed externally for $($node.name)."
                }
                Set-Weight $binding $node 0
                $changed = $true
            }
            Write-Host "Node $($node.name): CLB weight is 0 on every configured binding."
            Write-Warning "Waiting $drainSeconds seconds for existing connections. This does not prove long-lived TCP connections are fully drained."
            Start-Sleep -Seconds $drainSeconds
            $arguments = @{
                Version = $Version
                Service = $Service
                SkipBuild = $true
                ServerHost = [string]$node.host
                Port = [int]$node.sshPort
                User = [string]$node.user
                RemoteDirectory = $remoteDir
            }
            & (Join-Path $PSScriptRoot 'publish.ps1') @arguments
            if (-not $?) { throw "Single-host publish failed for $($node.name)." }

            Assert-NodeHealthy $node
            foreach ($binding in $bindings) {
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                Set-Weight $binding $node ([int]$originalWeights[$key])
            }
            Wait-NodeHealthy $node
            Write-Host "Node $($node.name) restored and healthy."
        } catch {
            if ($changed) {
                Write-Error "STOPPED on $($node.name): $($_.Exception.Message). CLB bindings may be partially drained/restored. Inspect target weights manually; no automatic rollback is attempted." -ErrorAction Continue
            }
            throw
        }
    }
    Write-Host "Completed: $($nodes.Count) nodes published successfully."
} finally {
    $lock.Dispose()
}
)) {
        throw "Invalid locationId for $listener (use an empty string for TCP listeners)."
    }
    $key = "$listener/$($binding.locationId)"
    if ($bindingKeys.ContainsKey($key)) { throw "Duplicate CLB binding: $key" }
    $bindingKeys[$key] = $true
}
$nodeNames = @{}
$nodeIds = @{}
foreach ($node in $nodes) {
    $name = Require-String $node 'name'
    $hostName = Require-String $node 'host'
    $userName = Require-String $node 'user'
    $instanceId = Require-String $node 'instanceId'
    if ($name -notmatch '^[A-Za-z0-9_-]+$' -or
        $hostName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$' -or
        $userName -notmatch '^[A-Za-z_][A-Za-z0-9_-]*$' -or
        $instanceId -notmatch '^ins-[A-Za-z0-9]+$') {
        throw "Invalid node fields: $name."
    }
    if ($null -eq $node.PSObject.Properties['sshPort'] -or
        $node.sshPort -isnot [int] -or $node.sshPort -lt 1 -or $node.sshPort -gt 65535) {
        throw "Invalid sshPort for node $name."
    }
    if ($nodeNames.ContainsKey($name) -or $nodeIds.ContainsKey($instanceId)) {
        throw "Duplicate node name or CVM ID: $name."
    }
    $nodeNames[$name] = $true
    $nodeIds[$instanceId] = $true
}
$tccliCommand = Get-Command tccli -ErrorAction Stop
$tccli = $tccliCommand.Source

# CLI JSON arguments are passed via a temporary UTF-8 file to avoid
# Windows PowerShell 5.1 native-command JSON quoting problems.
function Invoke-Clb {
    param([string]$Action, [hashtable]$Arguments)
    $temp = [IO.Path]::GetTempFileName()
    try {
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [IO.File]::WriteAllText($temp, ($Arguments | ConvertTo-Json -Depth 12 -Compress), $utf8)
        $path = [IO.Path]::GetFullPath($temp).Replace('\', '/')
        if ($path -match '^[A-Za-z]:/') { $path = '/' + $path }
        $uri = 'file://' + $path
        $output = @(& $tccli clb $Action --region $region --cli-input-json $uri --output json --timeout 30 2>&1)
        if ($LASTEXITCODE -ne 0) {
            throw "tccli clb $Action failed: $($output -join ' ')"
        }
        $body = ($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine
        $parsed = $body | ConvertFrom-Json
        if ($null -eq $parsed -or $null -eq $parsed.Response) {
            throw "Invalid tccli $Action JSON response."
        }
        if ($null -ne $parsed.Response.Error) {
            throw "CLB $Action API error: $($parsed.Response.Error.Code)"
        }
        return $parsed.Response
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
}

function Get-BindingTargets {
    param($Binding)
    $r = Invoke-Clb 'DescribeTargets' @{
        LoadBalancerId = $lbId
        ListenerIds = @([string]$Binding.listenerId)
    }
    $listeners = @($r.Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { throw "Missing or duplicate CLB listener $($Binding.listenerId)." }
    $listener = $listeners[0]
    if ($Binding.locationId) {
        $rules = @($listener.Rules | Where-Object { $_.LocationId -eq $Binding.locationId })
        if ($rules.Count -ne 1) { throw "Cannot find CLB rule $($Binding.locationId)." }
        $items = @($rules[0].Targets)
    } else {
        if ($listener.Protocol -in @('HTTP', 'HTTPS')) {
            throw "HTTP/HTTPS listener $($Binding.listenerId) requires locationId."
        }
        $items = @($listener.Targets)
    }
    foreach ($item in $items) { if ($null -ne $item) { Write-Output $item } }
}

function Get-Target {
    param($Binding, $Node)
    $matches = @(Get-BindingTargets $Binding | Where-Object {
        $_.InstanceId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    if ($matches.Count -ne 1) {
        throw "CLB target mismatch for $($Node.name) / $($Binding.listenerId):$($Binding.backendPort)."
    }
    if ($null -eq $matches[0].Weight) { throw "Missing CLB weight for $($Node.name)." }
    return $matches[0]
}

function Get-TargetHealth {
    param($Binding, $Node)
    $args = @{
        LoadBalancerIds = @($lbId)
        ListenerIds = @([string]$Binding.listenerId)
    }
    if ($Binding.locationId) { $args.LocationIds = @([string]$Binding.locationId) }
    $r = Invoke-Clb 'DescribeTargetHealth' $args
    $loadBalancers = @($r.LoadBalancers | Where-Object { $_.LoadBalancerId -eq $lbId })
    if ($loadBalancers.Count -ne 1) { return $false }
    $listeners = @($loadBalancers[0].Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { return $false }
    $rules = @($listeners[0].Rules)
    if ($Binding.locationId) {
        $rules = @($rules | Where-Object { $_.LocationId -eq $Binding.locationId })
    }
    $targets = @($rules | ForEach-Object { $_.Targets } | Where-Object { $null -ne $_ })
    $matches = @($targets | Where-Object {
        $_.TargetId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    return ($matches.Count -eq 1 -and $matches[0].HealthStatus -eq $true)
}

function Assert-NodeHealthy {
    param($Node)
    foreach ($binding in $bindings) {
        if (-not (Get-TargetHealth $binding $Node)) {
            throw "CLB health check failed for $($Node.name) on $($binding.listenerId). Ensure CLB health checks are enabled."
        }
    }
}

function Wait-NodeHealthy {
    param($Node)
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $consecutive = 0
    while ([DateTime]::UtcNow -lt $deadline) {
        $ok = $true
        foreach ($binding in $bindings) {
            if (-not (Get-TargetHealth $binding $Node)) { $ok = $false; break }
        }
        if ($ok) {
            $consecutive++
            if ($consecutive -ge 3) { return }
        } else {
            $consecutive = 0
        }
        Start-Sleep -Seconds 3
    }
    throw "CLB health did not recover for $($Node.name)."
}

function Set-Weight {
    param($Binding, $Node, [int]$Weight)
    $args = @{
        LoadBalancerId = $lbId
        ListenerId = [string]$Binding.listenerId
        Targets = @(@{
            InstanceId = [string]$Node.instanceId
            Port = [int]$Binding.backendPort
        })
        Weight = $Weight
    }
    if ($Binding.locationId) { $args.LocationId = [string]$Binding.locationId }
    $r = Invoke-Clb 'ModifyTargetWeight' $args
    if ([string]::IsNullOrWhiteSpace([string]$r.RequestId)) {
        throw "CLB ModifyTargetWeight returned no RequestId for $($Node.name)."
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $done = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        $status = Invoke-Clb 'DescribeTaskStatus' @{ TaskId = [string]$r.RequestId }
        if ([int]$status.Status -eq 0) { $done = $true; break }
        if ([int]$status.Status -eq 1) { throw "CLB task failed: $($status.Message)" }
        if ([int]$status.Status -ne 2) { throw "Unexpected CLB task status $($status.Status)." }
        Start-Sleep -Seconds 2
    }
    if (-not $done) { throw "CLB task timed out for $($Node.name)." }
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        $current = Get-Target $Binding $Node
        if ([int]$current.Weight -eq $Weight) { return }
        Start-Sleep -Seconds 2
    }
    throw "CLB weight verification timed out for $($Node.name), expected $Weight."
}

# This lock prevents concurrent runs from the same Windows machine.
# Do not run a second controller from another workstation/CI concurrently.
$lockPath = Join-Path ([IO.Path]::GetTempPath()) "middle-platform-rolling-$lbId.lock"
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
} catch {
    throw "Another local rolling deployment is active: $lockPath"
}
try {
    $originalWeights = @{}
    Write-Host "Preflight: CLB $lbId in $region, $($nodes.Count) nodes, $($bindings.Count) bindings."
    foreach ($binding in $bindings) {
        $targets = @(Get-BindingTargets $binding)
        if ($targets.Count -ne $nodes.Count) {
            throw "CLB binding $($binding.listenerId) has $($targets.Count) backends but configuration lists $($nodes.Count). Refusing partial coverage."
        }
        foreach ($node in $nodes) {
            $target = Get-Target $binding $node
            $weight = [int]$target.Weight
            if ($weight -le 0 -or $weight -gt 100) {
                throw "Node $($node.name) is not active in CLB (weight $weight)."
            }
            $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
            $originalWeights[$key] = $weight
        }
    }
    foreach ($node in $nodes) { Assert-NodeHealthy $node }
    Write-Host 'Preflight passed. All configured CLB backends are healthy and have positive weight.'
    if ($CheckOnly) {
        Write-Host 'CheckOnly: no package build, CLB change, upload, or restart performed.'
        return
    }

    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'publish.config.json'))) {
        throw 'Existing publish.config.json is required by publish.ps1.'
    }
    if (-not $SkipBuild) {
        Write-Host "Packaging version $Version once..."
        & (Join-Path $PSScriptRoot 'package.ps1') -Version $Version -Service $Service -SkipFrontend:$SkipFrontend -Fast:$Fast
        if (-not $?) { throw 'Local package step failed.' }
    }
    $archive = Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz"
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) {
        throw "Missing Linux amd64 archive: $archive"
    }

    foreach ($node in $nodes) {
        Write-Host "=== Rolling node $($node.name) ($($node.host)) ==="
        foreach ($other in $nodes) {
            if ($other.name -ne $node.name) {
                Assert-NodeHealthy $other
                foreach ($binding in $bindings) {
                    if ([int](Get-Target $binding $other).Weight -le 0) {
                        throw "Another node is drained: $($other.name). Refusing to continue."
                    }
                }
            }
        }
        $changed = $false
        try {
            foreach ($binding in $bindings) {
                # Refuse unexpected/manual weight changes during the deployment.
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                if ([int](Get-Target $binding $node).Weight -ne [int]$originalWeights[$key]) {
                    throw "CLB weight changed externally for $($node.name)."
                }
                Set-Weight $binding $node 0
                $changed = $true
            }
            Write-Host "Node $($node.name): CLB weight is 0 on every configured binding."
            Write-Warning "Waiting $drainSeconds seconds for existing connections. This does not prove long-lived TCP connections are fully drained."
            Start-Sleep -Seconds $drainSeconds
            $arguments = @{
                Version = $Version
                Service = $Service
                SkipBuild = $true
                ServerHost = [string]$node.host
                Port = [int]$node.sshPort
                User = [string]$node.user
                RemoteDirectory = $remoteDir
            }
            & (Join-Path $PSScriptRoot 'publish.ps1') @arguments
            if (-not $?) { throw "Single-host publish failed for $($node.name)." }

            Assert-NodeHealthy $node
            foreach ($binding in $bindings) {
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                Set-Weight $binding $node ([int]$originalWeights[$key])
            }
            Wait-NodeHealthy $node
            Write-Host "Node $($node.name) restored and healthy."
        } catch {
            if ($changed) {
                Write-Error "STOPPED on $($node.name): $($_.Exception.Message). CLB bindings may be partially drained/restored. Inspect target weights manually; no automatic rollback is attempted." -ErrorAction Continue
            }
            throw
        }
    }
    Write-Host "Completed: $($nodes.Count) nodes published successfully."
} finally {
    $lock.Dispose()
}
 -or
        [int]$binding.backendPort -lt 1 -or [int]$binding.backendPort -gt 65535) {
        throw "Invalid backendPort for $listener."
    }
    if ($null -ne $binding.PSObject.Properties['locationId'] -and
        $binding.locationId -and $binding.locationId -notmatch '^loc-[A-Za-z0-9]+$') {
        throw "Invalid locationId for $listener."
    }
    $key = "$listener/$($binding.locationId)"
    if ($bindingKeys.ContainsKey($key)) { throw "Duplicate CLB binding: $key" }
    $bindingKeys[$key] = $true
}
$nodeNames = @{}
$nodeIds = @{}
foreach ($node in $nodes) {
    $name = Require-String $node 'name'
    $hostName = Require-String $node 'host'
    $userName = Require-String $node 'user'
    $instanceId = Require-String $node 'instanceId'
    if ($name -notmatch '^[A-Za-z0-9_-]+$' -or
        $hostName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$' -or
        $userName -notmatch '^[A-Za-z_][A-Za-z0-9_-]*$' -or
        $instanceId -notmatch '^ins-[A-Za-z0-9]+$') {
        throw "Invalid node fields: $name."
    }
    if ($null -eq $node.PSObject.Properties['sshPort'] -or
        $node.sshPort -isnot [int] -or $node.sshPort -lt 1 -or $node.sshPort -gt 65535) {
        throw "Invalid sshPort for node $name."
    }
    if ($nodeNames.ContainsKey($name) -or $nodeIds.ContainsKey($instanceId)) {
        throw "Duplicate node name or CVM ID: $name."
    }
    $nodeNames[$name] = $true
    $nodeIds[$instanceId] = $true
}
$tccliCommand = Get-Command tccli -ErrorAction Stop
$tccli = $tccliCommand.Source

# CLI JSON arguments are passed via a temporary UTF-8 file to avoid
# Windows PowerShell 5.1 native-command JSON quoting problems.
function Invoke-Clb {
    param([string]$Action, [hashtable]$Arguments)
    $temp = [IO.Path]::GetTempFileName()
    try {
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [IO.File]::WriteAllText($temp, ($Arguments | ConvertTo-Json -Depth 12 -Compress), $utf8)
        $path = [IO.Path]::GetFullPath($temp).Replace('\', '/')
        if ($path -match '^[A-Za-z]:/') { $path = '/' + $path }
        $uri = 'file://' + $path
        $output = @(& $tccli clb $Action --region $region --cli-input-json $uri --output json --timeout 30 2>&1)
        if ($LASTEXITCODE -ne 0) {
            throw "tccli clb $Action failed: $($output -join ' ')"
        }
        $body = ($output | ForEach-Object { [string]$_ }) -join [Environment]::NewLine
        $parsed = $body | ConvertFrom-Json
        if ($null -eq $parsed -or $null -eq $parsed.Response) {
            throw "Invalid tccli $Action JSON response."
        }
        if ($null -ne $parsed.Response.Error) {
            throw "CLB $Action API error: $($parsed.Response.Error.Code)"
        }
        return $parsed.Response
    } finally {
        Remove-Item -LiteralPath $temp -Force -ErrorAction SilentlyContinue
    }
}

function Get-BindingTargets {
    param($Binding)
    $r = Invoke-Clb 'DescribeTargets' @{
        LoadBalancerId = $lbId
        ListenerIds = @([string]$Binding.listenerId)
    }
    $listeners = @($r.Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { throw "Missing or duplicate CLB listener $($Binding.listenerId)." }
    $listener = $listeners[0]
    if ($Binding.locationId) {
        $rules = @($listener.Rules | Where-Object { $_.LocationId -eq $Binding.locationId })
        if ($rules.Count -ne 1) { throw "Cannot find CLB rule $($Binding.locationId)." }
        $items = @($rules[0].Targets)
    } else {
        if ($listener.Protocol -in @('HTTP', 'HTTPS')) {
            throw "HTTP/HTTPS listener $($Binding.listenerId) requires locationId."
        }
        $items = @($listener.Targets)
    }
    foreach ($item in $items) { if ($null -ne $item) { Write-Output $item } }
}

function Get-Target {
    param($Binding, $Node)
    $matches = @(Get-BindingTargets $Binding | Where-Object {
        $_.InstanceId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    if ($matches.Count -ne 1) {
        throw "CLB target mismatch for $($Node.name) / $($Binding.listenerId):$($Binding.backendPort)."
    }
    if ($null -eq $matches[0].Weight) { throw "Missing CLB weight for $($Node.name)." }
    return $matches[0]
}

function Get-TargetHealth {
    param($Binding, $Node)
    $args = @{
        LoadBalancerIds = @($lbId)
        ListenerIds = @([string]$Binding.listenerId)
    }
    if ($Binding.locationId) { $args.LocationIds = @([string]$Binding.locationId) }
    $r = Invoke-Clb 'DescribeTargetHealth' $args
    $loadBalancers = @($r.LoadBalancers | Where-Object { $_.LoadBalancerId -eq $lbId })
    if ($loadBalancers.Count -ne 1) { return $false }
    $listeners = @($loadBalancers[0].Listeners | Where-Object { $_.ListenerId -eq $Binding.listenerId })
    if ($listeners.Count -ne 1) { return $false }
    $rules = @($listeners[0].Rules)
    if ($Binding.locationId) {
        $rules = @($rules | Where-Object { $_.LocationId -eq $Binding.locationId })
    }
    $targets = @($rules | ForEach-Object { $_.Targets } | Where-Object { $null -ne $_ })
    $matches = @($targets | Where-Object {
        $_.TargetId -eq $Node.instanceId -and [int]$_.Port -eq [int]$Binding.backendPort
    })
    return ($matches.Count -eq 1 -and $matches[0].HealthStatus -eq $true)
}

function Assert-NodeHealthy {
    param($Node)
    foreach ($binding in $bindings) {
        if (-not (Get-TargetHealth $binding $Node)) {
            throw "CLB health check failed for $($Node.name) on $($binding.listenerId). Ensure CLB health checks are enabled."
        }
    }
}

function Wait-NodeHealthy {
    param($Node)
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $consecutive = 0
    while ([DateTime]::UtcNow -lt $deadline) {
        $ok = $true
        foreach ($binding in $bindings) {
            if (-not (Get-TargetHealth $binding $Node)) { $ok = $false; break }
        }
        if ($ok) {
            $consecutive++
            if ($consecutive -ge 3) { return }
        } else {
            $consecutive = 0
        }
        Start-Sleep -Seconds 3
    }
    throw "CLB health did not recover for $($Node.name)."
}

function Set-Weight {
    param($Binding, $Node, [int]$Weight)
    $args = @{
        LoadBalancerId = $lbId
        ListenerId = [string]$Binding.listenerId
        Targets = @(@{
            InstanceId = [string]$Node.instanceId
            Port = [int]$Binding.backendPort
        })
        Weight = $Weight
    }
    if ($Binding.locationId) { $args.LocationId = [string]$Binding.locationId }
    $r = Invoke-Clb 'ModifyTargetWeight' $args
    if ([string]::IsNullOrWhiteSpace([string]$r.RequestId)) {
        throw "CLB ModifyTargetWeight returned no RequestId for $($Node.name)."
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    $done = $false
    while ([DateTime]::UtcNow -lt $deadline) {
        $status = Invoke-Clb 'DescribeTaskStatus' @{ TaskId = [string]$r.RequestId }
        if ([int]$status.Status -eq 0) { $done = $true; break }
        if ([int]$status.Status -eq 1) { throw "CLB task failed: $($status.Message)" }
        if ([int]$status.Status -ne 2) { throw "Unexpected CLB task status $($status.Status)." }
        Start-Sleep -Seconds 2
    }
    if (-not $done) { throw "CLB task timed out for $($Node.name)." }
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        $current = Get-Target $Binding $Node
        if ([int]$current.Weight -eq $Weight) { return }
        Start-Sleep -Seconds 2
    }
    throw "CLB weight verification timed out for $($Node.name), expected $Weight."
}

# This lock prevents concurrent runs from the same Windows machine.
# Do not run a second controller from another workstation/CI concurrently.
$lockPath = Join-Path ([IO.Path]::GetTempPath()) "middle-platform-rolling-$lbId.lock"
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
} catch {
    throw "Another local rolling deployment is active: $lockPath"
}
try {
    $originalWeights = @{}
    Write-Host "Preflight: CLB $lbId in $region, $($nodes.Count) nodes, $($bindings.Count) bindings."
    foreach ($binding in $bindings) {
        $targets = @(Get-BindingTargets $binding)
        if ($targets.Count -ne $nodes.Count) {
            throw "CLB binding $($binding.listenerId) has $($targets.Count) backends but configuration lists $($nodes.Count). Refusing partial coverage."
        }
        foreach ($node in $nodes) {
            $target = Get-Target $binding $node
            $weight = [int]$target.Weight
            if ($weight -le 0 -or $weight -gt 100) {
                throw "Node $($node.name) is not active in CLB (weight $weight)."
            }
            $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
            $originalWeights[$key] = $weight
        }
    }
    foreach ($node in $nodes) { Assert-NodeHealthy $node }
    Write-Host 'Preflight passed. All configured CLB backends are healthy and have positive weight.'
    if ($CheckOnly) {
        Write-Host 'CheckOnly: no package build, CLB change, upload, or restart performed.'
        return
    }

    if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'publish.config.json'))) {
        throw 'Existing publish.config.json is required by publish.ps1.'
    }
    if (-not $SkipBuild) {
        Write-Host "Packaging version $Version once..."
        & (Join-Path $PSScriptRoot 'package.ps1') -Version $Version -Service $Service -SkipFrontend:$SkipFrontend -Fast:$Fast
        if (-not $?) { throw 'Local package step failed.' }
    }
    $archive = Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz"
    if (-not (Test-Path -LiteralPath $archive -PathType Leaf)) {
        throw "Missing Linux amd64 archive: $archive"
    }

    foreach ($node in $nodes) {
        Write-Host "=== Rolling node $($node.name) ($($node.host)) ==="
        foreach ($other in $nodes) {
            if ($other.name -ne $node.name) {
                Assert-NodeHealthy $other
                foreach ($binding in $bindings) {
                    if ([int](Get-Target $binding $other).Weight -le 0) {
                        throw "Another node is drained: $($other.name). Refusing to continue."
                    }
                }
            }
        }
        $changed = $false
        try {
            foreach ($binding in $bindings) {
                # Refuse unexpected/manual weight changes during the deployment.
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                if ([int](Get-Target $binding $node).Weight -ne [int]$originalWeights[$key]) {
                    throw "CLB weight changed externally for $($node.name)."
                }
                Set-Weight $binding $node 0
                $changed = $true
            }
            Write-Host "Node $($node.name): CLB weight is 0 on every configured binding."
            Write-Warning "Waiting $drainSeconds seconds for existing connections. This does not prove long-lived TCP connections are fully drained."
            Start-Sleep -Seconds $drainSeconds
            $arguments = @{
                Version = $Version
                Service = $Service
                SkipBuild = $true
                ServerHost = [string]$node.host
                Port = [int]$node.sshPort
                User = [string]$node.user
                RemoteDirectory = $remoteDir
            }
            & (Join-Path $PSScriptRoot 'publish.ps1') @arguments
            if (-not $?) { throw "Single-host publish failed for $($node.name)." }

            Assert-NodeHealthy $node
            foreach ($binding in $bindings) {
                $key = "$($node.name)|$($binding.listenerId)|$($binding.locationId)"
                Set-Weight $binding $node ([int]$originalWeights[$key])
            }
            Wait-NodeHealthy $node
            Write-Host "Node $($node.name) restored and healthy."
        } catch {
            if ($changed) {
                Write-Error "STOPPED on $($node.name): $($_.Exception.Message). CLB bindings may be partially drained/restored. Inspect target weights manually; no automatic rollback is attempted." -ErrorAction Continue
            }
            throw
        }
    }
    Write-Host "Completed: $($nodes.Count) nodes published successfully."
} finally {
    $lock.Dispose()
}
