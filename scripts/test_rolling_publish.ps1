# Offline smoke tests for rolling.ps1. No SSH, CLB or actual services.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$workspace = Join-Path ([IO.Path]::GetTempPath()) ('rolling-publish-test-' + [Guid]::NewGuid().ToString('N'))
$publishDir = Join-Path $workspace 'publish'
$distDir = Join-Path $workspace 'dist'
New-Item -ItemType Directory -Path $publishDir, $distDir -Force | Out-Null
try {
    Copy-Item -LiteralPath (Join-Path $repo 'publish/rolling.ps1') -Destination (Join-Path $publishDir 'rolling.ps1')
    Set-Content -LiteralPath (Join-Path $publishDir 'publish.config.json') -Value '{}' -Encoding UTF8

    @'
param([string]$Version, [string]$Service, [switch]$SkipFrontend, [switch]$Fast)
Add-Content -LiteralPath $env:ROLLING_TEST_EVENTS -Value "package:$Version"
Set-Content -LiteralPath (Join-Path (Join-Path $PSScriptRoot '..\dist') "middle-platform-$Version-linux-amd64.tar.gz") -Value 'fake archive'
'@ | Set-Content -LiteralPath (Join-Path $publishDir 'package.ps1') -Encoding UTF8

    @'
param([string]$Version, [string]$Service, [switch]$SkipBuild, [string]$ServerHost, [int]$Port, [string]$User, [string]$RemoteDirectory)
if (-not $SkipBuild) { throw 'Expected -SkipBuild on every node' }
if ($RemoteDirectory -ne '/data/middle-platform') { throw 'Incorrect remote directory' }
Add-Content -LiteralPath $env:ROLLING_TEST_EVENTS -Value ('publish:{0}:{1}:{2}:{3}' -f $ServerHost, $Port, $Version, $Service)
if ($env:ROLLING_TEST_FAIL_HOST -and $ServerHost -eq $env:ROLLING_TEST_FAIL_HOST) { throw 'simulated node failure' }
'@ | Set-Content -LiteralPath (Join-Path $publishDir 'publish.ps1') -Encoding UTF8

    $configPath = Join-Path $publishDir 'rolling.config.json'
    $eventsPath = Join-Path $workspace 'events.txt'
    $env:ROLLING_TEST_EVENTS = $eventsPath
    $nodes = @(
        @{ name = 'node-1'; host = '10.0.0.11'; sshPort = 16333; user = 'deploy' },
        @{ name = 'node-2'; host = '10.0.0.12'; sshPort = 16333; user = 'deploy' },
        @{ name = 'node-3'; host = '10.0.0.13'; sshPort = 16333; user = 'deploy' }
    )
    $config = @{ remoteDirectory = '/data/middle-platform'; nodes = $nodes }
    $config | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $configPath -Encoding UTF8
    $script = Join-Path $publishDir 'rolling.ps1'

    function Assert-Events {
        param([string[]]$Expected)
        $actual = @()
        if (Test-Path -LiteralPath $eventsPath) { $actual = @(Get-Content -LiteralPath $eventsPath) }
        if (($actual -join '|') -ne ($Expected -join '|')) {
            throw "Unexpected calls. Expected $($Expected -join '|') ; actual $($actual -join '|')"
        }
    }
    function Clear-Events {
        if (Test-Path -LiteralPath $eventsPath) { Remove-Item -LiteralPath $eventsPath }
    }

    & $script -Version smoke -CheckOnly
    Assert-Events @()

    & $script -Version smoke -Service sdk-service
    Assert-Events @(
        'package:smoke',
        'publish:10.0.0.11:16333:smoke:sdk-service',
        'publish:10.0.0.12:16333:smoke:sdk-service',
        'publish:10.0.0.13:16333:smoke:sdk-service'
    )
    Clear-Events

    & $script -Version smoke -SkipBuild
    Assert-Events @(
        'publish:10.0.0.11:16333:smoke:all',
        'publish:10.0.0.12:16333:smoke:all',
        'publish:10.0.0.13:16333:smoke:all'
    )
    Clear-Events

    $env:ROLLING_TEST_FAIL_HOST = '10.0.0.12'
    $failed = $false
    try { & $script -Version smoke -SkipBuild } catch {
        $failed = $true
        if ([string]$_ -notmatch 'node-2') { throw "Did not report failing node: $_" }
    }
    if (-not $failed) { throw 'Expected failure was not propagated' }
    Assert-Events @(
        'publish:10.0.0.11:16333:smoke:all',
        'publish:10.0.0.12:16333:smoke:all'
    )
    Remove-Item Env:ROLLING_TEST_FAIL_HOST -ErrorAction SilentlyContinue
    Clear-Events

    $config.nodes[2].host = '10.0.0.11'
    $config.nodes[2].sshPort = 16333
    $config | ConvertTo-Json -Depth 10 | Set-Content -LiteralPath $configPath -Encoding UTF8
    $failed = $false
    try { & $script -Version smoke } catch { $failed = $true }
    if (-not $failed) { throw 'Expected rejection of duplicate SSH destination' }
    Assert-Events @()

    Write-Host 'Multi-node rolling publisher smoke tests passed.'
} finally {
    Remove-Item Env:ROLLING_TEST_EVENTS -ErrorAction SilentlyContinue
    Remove-Item Env:ROLLING_TEST_FAIL_HOST -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $workspace -Recurse -Force -ErrorAction SilentlyContinue
}
