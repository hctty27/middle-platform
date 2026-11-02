# 将 Go 服务和 Vue 前端交叉编译、校验，并打包为 Linux 发布归档。
# Windows / Linux 统一生成 tar.gz，显式设置 bin/ 下 Linux ELF 的 0755 执行权限。
# 用法：.\scripts\package.ps1 [-Arch amd64|arm64] [-Version v1] [-SkipFrontend]
# 依赖：Go；启用前端构建时还需要 npm。真实凭据和生产 YAML 不打包。
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
$outputRoot = Join-Path $repoRoot 'dist'
$gitCommit = 'unknown'

# 校验本地构建工具是否存在，缺失时直接失败，避免生成不完整发布包。
function Require-Command {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required command not found: $Name"
    }
}

# PowerShell 必须显式检查外部进程退出码，确保 Go/npm 失败会终止打包。
function Assert-ExitCode {
    param([string]$Operation)
    if ($LASTEXITCODE -ne 0) {
        throw "$Operation failed with exit code $LASTEXITCODE"
    }
}

Require-Command 'go'
$goHost = @(& go env GOHOSTOS GOHOSTARCH)
Assert-ExitCode 'go env GOHOSTOS GOHOSTARCH'
if ($goHost.Count -ne 2) {
    throw 'Could not determine Go host platform'
}
if (-not $SkipFrontend) {
    $npmCommand = if ($env:OS -eq 'Windows_NT') { 'npm.cmd' } else { 'npm' }
    Require-Command $npmCommand
}

# Git 提交信息仅用于版本追踪；工作树不可读取时仍允许构建。
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
# 临时构建目录不放进 dist，避免留下解压目录或中间文件。
$stageDir = Join-Path ([System.IO.Path]::GetTempPath()) ("middle-platform-" + [guid]::NewGuid().ToString('N'))
$binDir = Join-Path $stageDir 'bin'
$archivePath = Join-Path $outputRoot "$releaseName.tar.gz"

New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
New-Item -ItemType Directory -Force -Path $binDir | Out-Null

# 保存调用者的 Go 环境变量；结束时恢复，避免跨平台编译设置残留。
$originalGoOS = [Environment]::GetEnvironmentVariable('GOOS', 'Process')
$originalGoArch = [Environment]::GetEnvironmentVariable('GOARCH', 'Process')
$originalCgo = [Environment]::GetEnvironmentVariable('CGO_ENABLED', 'Process')

try {
    $env:GOOS = 'linux'
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = '0'

    # 三个 Go 服务加迁移工具使用统一的交叉编译参数。
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
            # 关闭自动 VCS stamping，避免本机 Git 元数据错误导致构建 exit status 128。
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

    # npm ci 使用锁文件安装，构建失败即终止打包，避免混入旧版前端文件。
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

    # 发布包只包含 bin/ 和可选的 admin/，不附加校验文件或构建说明。
    # 在本机运行 Go 打包辅助程序，避免 Windows 交叉编译环境阻止 go run。
    $env:GOOS = ([string]$goHost[0]).Trim()
    $env:GOARCH = ([string]$goHost[1]).Trim()
    $env:CGO_ENABLED = '0'

    $archiveTool = Join-Path $PSScriptRoot 'package-archive.go'
    Write-Host "Creating $archivePath..."
    & go run -buildvcs=false $archiveTool -source $stageDir -output $archivePath
    Assert-ExitCode 'create Linux tar.gz'
    if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
        throw "Archive was not created: $archivePath"
    }

    # 仅清理本脚本的历史归档和旧版 dist/supervisor 中间目录。
    # 不删除用户自己放在 dist 下的其他文件。
    Get-ChildItem -LiteralPath $outputRoot -File |
        Where-Object {
            $_.Name -match '^middle-platform-.*-linux-(amd64|arm64)\.(zip|tar\.gz)$' -and
            $_.FullName -ne $archivePath
        } |
        Remove-Item -Force

    $legacyDir = Join-Path $outputRoot 'supervisor'
    if (Test-Path -LiteralPath $legacyDir) {
        Remove-Item -LiteralPath $legacyDir -Recurse -Force
    }

    Write-Host ''
    Write-Host "Package ready: $archivePath"
    Write-Host 'Archive layout: bin/, admin/ (unless skipped)'
    Write-Host 'All Linux binaries carry 0755 executable permissions.'

}
# 无论前端或后端构建是否成功，都恢复进程原有的 Go 环境变量。
finally {
    [Environment]::SetEnvironmentVariable('GOOS', $originalGoOS, 'Process')
    [Environment]::SetEnvironmentVariable('GOARCH', $originalGoArch, 'Process')
    [Environment]::SetEnvironmentVariable('CGO_ENABLED', $originalCgo, 'Process')
    if (Test-Path -LiteralPath $stageDir) {
        Remove-Item -LiteralPath $stageDir -Recurse -Force
    }
}
