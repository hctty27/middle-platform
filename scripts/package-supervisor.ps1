# 将 Go 服务和 Vue 前端交叉编译、校验，并打包为 Linux 发布归档。
# 用法：.\scripts\package-supervisor.ps1 [-Arch amd64|arm64] [-Version v1] [-SkipFrontend]
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
$outputRoot = Join-Path $repoRoot 'dist/supervisor'
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
$stageDir = Join-Path $outputRoot $releaseName
$binDir = Join-Path $stageDir 'bin'
$archivePath = Join-Path $outputRoot "$releaseName.zip"

New-Item -ItemType Directory -Force -Path $outputRoot | Out-Null
if (Test-Path -LiteralPath $stageDir) {
    Remove-Item -LiteralPath $stageDir -Recurse -Force
}
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

    # 随发布包携带原生 Linux 的 systemd 和 Nginx 模板，服务器无需另外拉取源码。
    $serviceDir = Join-Path $stageDir 'deploy/systemd'
    $nginxDir = Join-Path $stageDir 'deploy/nginx'
    New-Item -ItemType Directory -Force -Path $serviceDir, $nginxDir | Out-Null
    Copy-Item -Path (Join-Path $repoRoot 'deploy/systemd/*.service') -Destination $serviceDir -Force
    Copy-Item -Path (Join-Path $repoRoot 'deploy/nginx/*') -Destination $nginxDir -Force

    # 记录版本、Git 提交与编译平台；文件不包含生产密钥。
    $buildInfo = @(
        "Version: $Version"
        "GitCommit: $gitCommit"
        "Platform: linux/$Arch"
        "BuiltAtUTC: $((Get-Date).ToUniversalTime().ToString('o'))"
        "FrontendIncluded: $(-not $SkipFrontend)"
        'Production credentials and YAML files are intentionally excluded.'
    )
    $buildInfo | Set-Content -LiteralPath (Join-Path $stageDir 'BUILD_INFO.txt') -Encoding UTF8

    # 对所有待打包文件计算 SHA-256，方便在生产服务器验证传输完整性。
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
    Write-Host 'Archive layout: bin/, admin/ (unless skipped), deploy/, BUILD_INFO.txt, SHA256SUMS.txt'
    Write-Host 'After extracting on Linux, run: chmod +x bin/*'
}
# 无论前端或后端构建是否成功，都恢复进程原有的 Go 环境变量。
finally {
    [Environment]::SetEnvironmentVariable('GOOS', $originalGoOS, 'Process')
    [Environment]::SetEnvironmentVariable('GOARCH', $originalGoArch, 'Process')
    [Environment]::SetEnvironmentVariable('CGO_ENABLED', $originalCgo, 'Process')
}
