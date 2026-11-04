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
        throw "未找到必需的命令: $Name"
    }
}

# PowerShell 必须显式检查外部进程退出码，确保 Go/npm 失败会终止打包。
function Assert-ExitCode {
    param([string]$Operation)
    if ($LASTEXITCODE -ne 0) {
        throw "$Operation 执行失败，退出码: $LASTEXITCODE"
    }
}

Require-Command 'go'
$goHost = @(& go env GOHOSTOS GOHOSTARCH)
Assert-ExitCode 'go env GOHOSTOS GOHOSTARCH'
if ($goHost.Count -ne 2) {
    throw '无法识别当前 Go 主机平台'
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
    throw "发布版本号不合法: $Version"
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
$originalConsoleOutputEncoding = [Console]::OutputEncoding
$originalOutputEncoding = $OutputEncoding

try {
    # Windows PowerShell 5.1 默认按系统代码页解码原生程序输出；Go/npm 统一使用 UTF-8。
    if ($env:OS -eq 'Windows_NT') {
        $utf8 = New-Object System.Text.UTF8Encoding($false)
        [Console]::OutputEncoding = $utf8
        $OutputEncoding = $utf8
    }
    $env:GOOS = 'linux'
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = '0'

    # 四个 Go 服务使用统一的交叉编译参数；数据库由人工执行 SQL。
    $targets = @(
        @{ Module = 'config-center';        Command = 'config-center';        Output = 'config-center' },
        @{ Module = 'config-center-access'; Command = 'config-center-access'; Output = 'config-center-access' },
        @{ Module = 'sdk-core';             Command = 'sdk-core';             Output = 'sdk-core' },
        @{ Module = 'sdk-service';          Command = 'sdk-service';          Output = 'sdk-service' }
    )

    foreach ($target in $targets) {
        Write-Host "正在为 linux/$Arch 构建 $($target.Output)..."
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
            throw "缺少编译产物: $outputFile"
        }
    }

    # 发布包携带人工审核的 SQL，不会在安装或运行服务时执行。
    $sqlDir = Join-Path $stageDir 'sql'
    $authorityDir = Join-Path $sqlDir 'config-center'
    $accessDir = Join-Path $sqlDir 'config-center-access'
    New-Item -ItemType Directory -Force -Path $authorityDir, $accessDir | Out-Null
    Copy-Item -LiteralPath (Join-Path $repoRoot 'config-center/db/sql/initialize.sql') -Destination $authorityDir
    Copy-Item -LiteralPath (Join-Path $repoRoot 'config-center/db/sql/add_game_order.sql') -Destination $authorityDir
    Copy-Item -LiteralPath (Join-Path $repoRoot 'config-center/db/sql/add_sdk_services.sql') -Destination $authorityDir
    Copy-Item -LiteralPath (Join-Path $repoRoot 'config-center/db/sql/add_sdk_user_check.sql') -Destination $authorityDir
    Copy-Item -LiteralPath (Join-Path $repoRoot 'config-center-access/db/sql/initialize.sql') -Destination $accessDir

    # npm ci 使用锁文件安装，构建失败即终止打包，避免混入旧版前端文件。
    if (-not $SkipFrontend) {
        $frontendDir = Join-Path $repoRoot 'config-center-admin'
        Write-Host '正在构建 Vue 管理端...'
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
            throw "前端构建未生成 dist/index.html"
        }
        $adminDir = Join-Path $stageDir 'admin'
        New-Item -ItemType Directory -Force -Path $adminDir | Out-Null
        Copy-Item -Path (Join-Path $distDir '*') -Destination $adminDir -Recurse -Force
    }

    # 发布包只包含 bin/、sql/ 和可选的 admin/，不附加校验文件或构建说明。
    # 在本机运行 Go 打包辅助程序，避免 Windows 交叉编译环境阻止 go run。
    $env:GOOS = ([string]$goHost[0]).Trim()
    $env:GOARCH = ([string]$goHost[1]).Trim()
    $env:CGO_ENABLED = '0'

    $archiveTool = Join-Path $PSScriptRoot 'package-archive.go'
    $archiveToolToRun = $archiveTool
    $archiveBytes = [System.IO.File]::ReadAllBytes($archiveTool)

    # 兼容本地被 Windows PowerShell 重定向为 UTF-16 的 Go 源码；
    # 只使用临时 UTF-8 副本，不覆盖开发者的工作区文件。
    $isUtf16Le = $archiveBytes.Length -ge 2 -and $archiveBytes[0] -eq 0xFF -and $archiveBytes[1] -eq 0xFE
    $isUtf16Be = $archiveBytes.Length -ge 2 -and $archiveBytes[0] -eq 0xFE -and $archiveBytes[1] -eq 0xFF
    if ($isUtf16Le -or $isUtf16Be) {
        $sourceEncoding = if ($isUtf16Le) { [System.Text.Encoding]::Unicode } else { [System.Text.Encoding]::BigEndianUnicode }
        $sourceText = $sourceEncoding.GetString($archiveBytes, 2, $archiveBytes.Length - 2)
        $archiveToolToRun = Join-Path $stageDir 'package-archive.go'
        [System.IO.File]::WriteAllText($archiveToolToRun, $sourceText, (New-Object System.Text.UTF8Encoding($false)))
        Write-Warning '检测到 package-archive.go 是 UTF-16，已使用临时 UTF-8 副本执行。请检查本地源文件编码。'
    } elseif ([System.Array]::IndexOf($archiveBytes, [byte]0) -ge 0) {
        throw 'package-archive.go 包含 NUL 字节，无法作为 Go 源码执行。请检查文件编码；确认无本地修改后可执行 git restore -- scripts/package-archive.go。'
    }

    Write-Host "正在创建 $archivePath..."
    & go run -buildvcs=false $archiveToolToRun -source $stageDir -output $archivePath
    Assert-ExitCode 'create Linux tar.gz'
    if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
        throw "归档文件未生成: $archivePath"
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
    Write-Host "打包完成: $archivePath"
    Write-Host '归档目录: bin/、sql/、admin/（跳过前端时无 admin/）'
    Write-Host '所有 Linux 可执行文件均设置为 0755 权限。'

}
# 无论前端或后端构建是否成功，都恢复进程原有的 Go 环境变量。
finally {
    [Environment]::SetEnvironmentVariable('GOOS', $originalGoOS, 'Process')
    [Environment]::SetEnvironmentVariable('GOARCH', $originalGoArch, 'Process')
    [Environment]::SetEnvironmentVariable('CGO_ENABLED', $originalCgo, 'Process')
    [Console]::OutputEncoding = $originalConsoleOutputEncoding
    $OutputEncoding = $originalOutputEncoding
    if (Test-Path -LiteralPath $stageDir) {
        Remove-Item -LiteralPath $stageDir -Recurse -Force
    }
}
