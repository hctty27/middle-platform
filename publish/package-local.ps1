# Run UTF-8-compatible temporary copies without modifying the repository's build scripts.
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
$sourceDir = Join-Path $repoRoot 'scripts'
$utf8 = New-Object System.Text.UTF8Encoding($false, $true)
$packageText = [System.IO.File]::ReadAllText((Join-Path $sourceDir 'package.ps1'), $utf8)
$archiveBytes = [System.IO.File]::ReadAllBytes((Join-Path $sourceDir 'package-archive.go'))
if ($archiveBytes -contains [byte]0) {
    throw 'The archive helper contains NUL bytes. Original files were not changed.'
}
$null = $utf8.GetString($archiveBytes)

# The temporary script has a different PSScriptRoot; keep its build inputs in this repository.
$rootAssignment = '$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot ''..'')).Path'
if ([regex]::Matches($packageText, [regex]::Escape($rootAssignment)).Count -ne 1) {
    throw 'The original package.ps1 layout has changed. Update the local compatibility wrapper before building.'
}
$packageText = $packageText.Replace($rootAssignment, ('$repoRoot = ''' + $repoRoot.Replace("'", "''") + "'"))

$runtimeDir = Join-Path ([System.IO.Path]::GetTempPath()) ('middle-platform-build-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $runtimeDir | Out-Null
try {
    $runtimeScript = Join-Path $runtimeDir 'package.ps1'
    # Windows PowerShell 5.1 requires a BOM to recognize UTF-8 script literals.
    [System.IO.File]::WriteAllText($runtimeScript, $packageText, (New-Object System.Text.UTF8Encoding($true)))
    [System.IO.File]::WriteAllBytes((Join-Path $runtimeDir 'package-archive.go'), $archiveBytes)
    & $runtimeScript -Arch $Arch -Version $Version -SkipFrontend:$SkipFrontend
}
finally {
    Remove-Item -LiteralPath $runtimeDir -Recurse -Force
}
