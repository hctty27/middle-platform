# Simple multi-node sequential publisher: reuses existing publish.ps1 per host.
# CLB is NOT modified. No version directories or automatic rollback.
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
if ($Service -eq 'admin' -and $SkipFrontend) { throw 'Cannot use -SkipFrontend with admin.' }

function Required-String {
    param($Object, [string]$Property)
    $item = $Object.PSObject.Properties[$Property]
    if ($null -eq $item -or $item.Value -isnot [string] -or [string]::IsNullOrWhiteSpace($item.Value)) {
        throw "Required string '$Property' is missing or empty in rolling configuration."
    }
    return [string]$item.Value
}

if (-not (Test-Path -LiteralPath $ConfigPath -PathType Leaf)) {
    throw "Missing rolling config: $ConfigPath. Copy rolling.config.example.json to rolling.config.json."
}
try {
    $config = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
} catch {
    throw "Invalid rolling configuration JSON: $($_.Exception.Message)"
}
if ($null -eq $config -or $config -isnot [pscustomobject]) {
    throw 'Rolling configuration must be a JSON object.'
}

$remoteDirectory = (Required-String $config 'remoteDirectory').TrimEnd('/')
if ($remoteDirectory -notmatch '^/[A-Za-z0-9_/-]+$' -or $remoteDirectory -match '(^|/)\.\.?(/|$)') {
    throw 'remoteDirectory must be an absolute non-root path without dot segments.'
}
if ($null -eq $config.PSObject.Properties['nodes'] -or $config.nodes -isnot [array]) {
    throw "Rolling configuration must contain a 'nodes' array."
}
$nodes = @($config.nodes)
if ($nodes.Count -lt 2) { throw 'Multi-node rolling deployment requires at least two nodes.' }
$names = @{}
$destinations = @{}
foreach ($node in $nodes) {
    if ($null -eq $node -or $node -isnot [pscustomobject]) { throw 'Every node must be a JSON object.' }
    $name = Required-String $node 'name'
    $hostName = Required-String $node 'host'
    $userName = Required-String $node 'user'
    if ($name -notmatch '^[A-Za-z0-9_-]+$' -or
        $hostName -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$' -or
        $userName -notmatch '^[A-Za-z_][A-Za-z0-9_-]*$') {
        throw "Invalid name, host or user on node '$name'."
    }
    $portProperty = $node.PSObject.Properties['sshPort']
    if ($null -eq $portProperty -or
        ($portProperty.Value -isnot [int] -and $portProperty.Value -isnot [long]) -or
        [long]$portProperty.Value -lt 1 -or [long]$portProperty.Value -gt 65535) {
        throw "Invalid sshPort for node '$name'."
    }
    $destination = "{0}:{1}" -f $hostName.ToLowerInvariant(), $portProperty.Value
    if ($names.ContainsKey($name) -or $destinations.ContainsKey($destination)) {
        throw "Duplicate node name or SSH destination: '$name' ($destination)."
    }
    $names[$name] = $true
    $destinations[$destination] = $true
}
Write-Host "Validated $($nodes.Count) nodes. CLB will not be modified."
if ($CheckOnly) {
    Write-Host 'CheckOnly: no build, upload, SSH or restart was performed.'
    return
}

# Existing publish.ps1 reads this file even with all SSH connection overrides.
if (-not (Test-Path -LiteralPath (Join-Path $PSScriptRoot 'publish.config.json') -PathType Leaf)) {
    throw 'Missing publish/publish.config.json, required by the single-host publisher.'
}
$archivePath = Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz"

# Local mutex only: use one designated Windows deploy machine or one CI environment.
$lockPath = Join-Path ([IO.Path]::GetTempPath()) 'middle-platform-rolling.lock'
try {
    $lock = [IO.File]::Open($lockPath, [IO.FileMode]::OpenOrCreate, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
} catch {
    throw "Another rolling deployment may be running on this computer: $lockPath"
}
try {
    if (-not $SkipBuild) {
        Write-Host "Building archive for $Version (once)..."
        $buildArguments = @{ Version = $Version; Service = $Service; SkipFrontend = $SkipFrontend; Fast = $Fast }
        & (Join-Path $PSScriptRoot 'package.ps1') @buildArguments
        if (-not $?) { throw 'Package build failed. No server was updated.' }
    }
    if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
        throw "Missing release archive: $archivePath"
    }

    $index = 0
    foreach ($node in $nodes) {
        $index++
        $name = [string]$node.name
        Write-Host "[$index/$($nodes.Count)] Publishing $name ($($node.host):$($node.sshPort))..."
        try {
            $publishArguments = @{
                Version = $Version
                Service = $Service
                SkipBuild = $true
                ServerHost = [string]$node.host
                Port = [int]$node.sshPort
                User = [string]$node.user
                RemoteDirectory = $remoteDirectory
            }
            & (Join-Path $PSScriptRoot 'publish.ps1') @publishArguments
            if (-not $?) { throw 'Single-host publisher returned failure.' }
        } catch {
            throw "Rolling publish stopped at '$name': $($_.Exception.Message). Later nodes were not updated; CLB was not modified. Inspect the failed node manually."
        }
        Write-Host "[$index/$($nodes.Count)] $name published and verified by the single-host publisher."
    }
    Write-Host "Completed sequential deployment of $($nodes.Count) nodes, version $Version."
} finally {
    $lock.Dispose()
}
