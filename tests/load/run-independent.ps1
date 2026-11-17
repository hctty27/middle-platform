# Run independently on Windows only against a manually verified, isolated test environment.
# Never stores or prints test secrets. Orders, payments and admin paths are not exercised.
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$BaseUrl,
    [Parameter(Mandatory = $true)]
    [string]$FixtureFile,
    [Parameter(Mandatory = $true)]
    [ValidateSet('AUTHORIZED-ISOLATED-TEST')]
    [string]$Confirm,
    [ValidateRange(10, 60)]
    [int]$Seconds = 20,
    [ValidateRange(400, 10000)]
    [int]$MaxQPS = 5000,
    [string]$OutputDirectory = '',
    [switch]$Allow10000
)
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if ($Confirm -ne 'AUTHORIZED-ISOLATED-TEST' -or
    $BaseUrl -cnotmatch '^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?$') {
    throw 'An explicitly authorized isolated test HTTPS origin is required.'
}
$source = Join-Path $PSScriptRoot 'independent.js'
if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
    throw 'Missing independent.js beside this script.'
}
$k6 = Get-Command k6 -ErrorAction Stop
if (-not (Test-Path -LiteralPath $FixtureFile -PathType Leaf)) {
    throw 'Test channel fixture JSON is missing.'
}
$fixturePath = [System.IO.Path]::GetFullPath((Resolve-Path -LiteralPath $FixtureFile).Path)
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\..')).TrimEnd('\', '/')
$repoPrefix = $repoRoot + [System.IO.Path]::DirectorySeparatorChar
if ($fixturePath.StartsWith($repoPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Fixture file must be outside the Git repository.'
}

# Verify structure without echoing the synthetic test keys.
$fixtures = @(Get-Content -LiteralPath $fixturePath -Raw -Encoding UTF8 | ConvertFrom-Json)
if ($fixtures.Count -ne 256) {
    throw 'This staged run requires exactly 256 isolated test channels.'
}
$seen = New-Object 'System.Collections.Generic.HashSet[string]'
foreach ($fixture in $fixtures) {
    foreach ($required in @('appId', 'gameSecret', 'zoneKey')) {
        if ($null -eq $fixture -or -not ($fixture.PSObject.Properties.Name -contains $required) -or
            $fixture.$required -isnot [string] -or [string]::IsNullOrWhiteSpace($fixture.$required)) {
            throw 'Fixture must include nonempty appId, gameSecret and zoneKey strings.'
        }
    }
    if ($fixture.appId -cnotmatch '^[A-Za-z0-9_-]{1,128}$' -or -not $seen.Add($fixture.appId)) {
        throw 'Duplicate or malformed test appId.'
    }
}
if ($OutputDirectory -eq '') {
    $OutputDirectory = Join-Path ([System.IO.Path]::GetTempPath()) 'middle-platform-load'
}
$reports = [System.IO.Path]::GetFullPath($OutputDirectory)
if ($reports -eq $repoRoot -or $reports.StartsWith($repoPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw 'Reports must be saved outside the Git repository.'
}
New-Item -ItemType Directory -Force -Path $reports | Out-Null
$runId = (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
if ($MaxQPS -notin @(400, 1000, 2000, 3000, 5000, 10000)) {
    throw 'MaxQPS must be one of 400, 1000, 2000, 3000, 5000 or 10000.'
}
if ($MaxQPS -eq 10000 -and -not $Allow10000) {
    throw 'The 10000 QPS stage requires the explicit -Allow10000 switch.'
}
$rates = @(400, 1000, 2000, 3000, 5000)
if ($Allow10000) { $rates += 10000 }
$rates = @($rates | Where-Object { $_ -le $MaxQPS })
$names = @('LOAD_CONFIRM', 'LOAD_BASE_URL', 'LOAD_FIXTURES', 'LOAD_TOTAL_QPS', 'LOAD_SECONDS')
$oldEnvironment = @{}
foreach ($name in $names) {
    $oldEnvironment[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}
try {
    [Environment]::SetEnvironmentVariable('LOAD_CONFIRM', $Confirm, 'Process')
    [Environment]::SetEnvironmentVariable('LOAD_BASE_URL', $BaseUrl, 'Process')
    [Environment]::SetEnvironmentVariable('LOAD_FIXTURES', $fixturePath, 'Process')
    [Environment]::SetEnvironmentVariable('LOAD_SECONDS', [string]$Seconds, 'Process')

    Write-Host 'Verifying 256-channel isolated fixture; no secrets or credentials are logged.'
    foreach ($rate in $rates) {
        [Environment]::SetEnvironmentVariable('LOAD_TOTAL_QPS', [string]$rate, 'Process')
        $summary = Join-Path $reports ("load-" + $runId + "-" + $rate + ".json")
        Write-Host ("Testing total {0} QPS: config {1}, user {1}, event {2}; {3} seconds." -f $rate, ($rate / 4), ($rate / 2), $Seconds)
        & $k6.Source run --summary-export $summary $source
        $resultCode = $LASTEXITCODE
        if ($resultCode -ne 0) {
            throw ("Stage {0} QPS failed (k6 exit {1}); stopped further escalation. Review {2}." -f $rate, $resultCode, $summary)
        }
        if (-not (Test-Path -LiteralPath $summary -PathType Leaf)) {
            throw ("Stage {0} QPS did not produce a summary; stopped." -f $rate)
        }
        Write-Host ("Stage {0} QPS passed. Summary: {1}" -f $rate, $summary)
    }
    Write-Host 'All requested stages passed. This does not certify production capacity.'
} finally {
    foreach ($name in $names) {
        [Environment]::SetEnvironmentVariable($name, $oldEnvironment[$name], 'Process')
    }
}
