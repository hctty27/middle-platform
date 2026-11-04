# Run UTF-8-compatible temporary copies without modifying the repository's build scripts.
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',
    [string]$Version = '',
    [switch]$SkipFrontend,
    [ValidateSet('all', 'config-center', 'config-center-access', 'sdk-core', 'sdk-service', 'admin')]
    [string]$Service = 'all',
    [switch]$Fast
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$sourceDir = Join-Path $repoRoot 'scripts'
$archiveSource = Join-Path $sourceDir 'package-archive.go'
$archiveBytes = [System.IO.File]::ReadAllBytes($archiveSource)
if ($archiveBytes -contains [byte]0) {
    throw 'The archive helper contains NUL bytes. Original files were not changed.'
}

# package.ps1 is saved as UTF-8 with BOM for Windows PowerShell 5.1 compatibility.
# Running it directly avoids copying and rewriting source code into a temp directory.
& (Join-Path $sourceDir 'package.ps1') -Arch $Arch -Version $Version -SkipFrontend:$SkipFrontend -Service $Service -Fast:$Fast
