# Build a Linux release archive for Supervisor deployment.
# Usage: .\scripts\package-supervisor.ps1 [-Arch amd64|arm64] [-Version v1] [-SkipFrontend]
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',

    [string]$Version = '',

    [switch]$SkipFrontend
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$outputRoot = Join-Path $repoRoot 'dist/supervisor'
$gitCommit = 'unknown'

function Require-Command {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required command not found: $Name"
    }
}

function Assert-ExitCode {
    param([string]$Operation)
    if ($LASTEXITCODE -ne 0) {
        throw "$Operation failed with exit code $LASTEXITCODE"
    }
}

Require-Command 'go'
if (-not $SkipFrontend) {
    $npmCommand = if ($env:OS -eq 'Windows_NT') { 'npm.cmd' } else { 'npm' }
    Require-Command $npmCommand
}

# Git metadata is optional: an invalid or untrusted worktree must not block packaging.
if (Get-Command 'git' -ErrorAction SilentlyContinue) {
    try {
        $gitOutput = @(& git -C $repoRoot rev-parse --verify HEAD 2>$null)
        if ($LASTEXITCODE -eq 0 -and $gitOutput.Count -gt 0) {
            $candidate = ([string]$gitOutput[0]).Trim()
            if ($candidate -match '^[0-9a-fA-F]{40}$') {
                $gitCommit = $candidate
            }
        }
    }
    catch {
        $gitCommit = 'unknown'
    }
}
if ([string]::IsNullOrWhiteSpace($Version)) {
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    if ($gitCommit -ne 'unknown') {
        $Version = 'git-' + $gitCommit.Substring(0, 12) + '-' + $stamp
    } else {
        $Version = 'local-' + $stamp
    }
}

if ($Version -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$') {
    throw "Invalid release version: $Version"
}

$releaseName = "middle-platform-$Version-linux-$Arch"
$stageDir = Join-Path $outputRoot $releaseName
$binDir = Join-Path $stageDir 'bin'
$archivePath = Join-Path $outputRoot "$releaseName.zip"

New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
if (Test-Path -LiteralPath $stageDir) {
    Remove-Item -LiteralPath $stageDir -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

$originalGoOS = [Environment]::GetEnvironmentVariable('GOOS', 'Process')
$originalGoArch = [Environment]::GetEnvironmentVariable('GOARCH', 'Process')
$originalCgo = [Environment]::GetEnvironmentVariable('CGO_ENABLED', 'Process')

try {
    $env:GOOS = 'linux'
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = '0'

    $targets = @(
        @{ Module = 'config-center';        Command = 'config-center';        Output = 'config-center' },
        @{ Module = 'config-center';        Command = 'migrate';              Output = 'migrate' },
        @{ Module = 'config-center-access'; Command = 'config-center-access'; Output = 'config-center-access' },
        @{ Module = 'sdk-core';             Command = 'sdk-core';             Output = 'sdk-core' }
    )

    foreach ($target in $targets) {
        Write-Host "Building $($target.Output) for linux/$Arch..."
        $moduleDir = Join-Path $repoRoot $target.Module
        $outputFile = Join-Path $binDir $target.Output
        Push-Location $moduleDir
        try {
            & go build -mod=readonly -buildvcs=false -trimpath '-ldflags=-s -w' -o $outputFile "./cmd/$($target.Command)"
            Assert-ExitCode "go build $($target.Output)"
        }
        finally {
            Pop-Location
        }
        if (-not (Test-Path -LiteralPath $outputFile -PathType Leaf)) {
            throw "Missing compiled binary: $outputFile"
        }
    }

    if (-not $SkipFrontend) {
        $frontendDir = Join-Path $repoRoot 'config-center-admin'
        Write-Host 'Building Vue admin...'
        Push-Location $frontendDir
        try {
            & $npmCommand ci --no-audit --no-fund
            Assert-ExitCode 'npm ci'
            & $npmCommand run build
            Assert-ExitCode 'npm run build'
        }
        finally {
            Pop-Location
        }

        $distDir = Join-Path $frontendDir 'dist'
        if (-not (Test-Path -LiteralPath (Join-Path $distDir 'index.html') -PathType Leaf)) {
            throw "Frontend build did not produce dist/index.html"
        }
        $adminDir = Join-Path $stageDir 'admin'
        New-Item -ItemType Directory -Force -Path $adminDir | Out-Null
        Copy-Item -Path (Join-Path $distDir '*') -Destination $adminDir -Recurse -Force
    }

    $buildInfo = @(
        "Version: $Version"
        "GitCommit: $gitCommit"
        "Platform: linux/$Arch"
        "BuiltAtUTC: $((Get-Date).ToUniversalTime().ToString('o'))"
        "FrontendIncluded: $(-not $SkipFrontend)"
        'Production credentials and YAML files are intentionally excluded.'
    )
    $buildInfo | Set-Content -LiteralPath (Join-Path $stageDir 'BUILD_INFO.txt') -Encoding UTF8

    $hashLines = @(Get-ChildItem -LiteralPath $stageDir -File -Recurse |
        Sort-Object FullName |
        ForEach-Object {
            $relative = $_.FullName.Substring($stageDir.Length + 1).Replace('\', '/')
            $hash = (Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
            "$hash  $relative"
        })
    $hashLines | Set-Content -LiteralPath (Join-Path $stageDir 'SHA256SUMS.txt') -Encoding Ascii

    Write-Host "Compressing $archivePath..."
    Compress-Archive -Path (Join-Path $stageDir '*') -DestinationPath $archivePath -CompressionLevel Optimal -Force
    if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
        throw "Archive was not created: $archivePath"
    }

    Write-Host ''
    Write-Host "Package ready: $archivePath"
    Write-Host 'Archive layout: bin/, admin/ (unless skipped), BUILD_INFO.txt, SHA256SUMS.txt'
    Write-Host 'After extracting on Linux, run: chmod +x bin/*'
}
finally {
    [Environment]::SetEnvironmentVariable('GOOS', $originalGoOS, 'Process')
    [Environment]::SetEnvironmentVariable('GOARCH', $originalGoArch, 'Process')
    [Environment]::SetEnvironmentVariable('CGO_ENABLED', $originalCgo, 'Process')
}
