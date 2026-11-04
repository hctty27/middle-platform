# Build or reuse a Linux package, upload it and optionally restart checked Supervisor services.
# Copy publish.config.example.json to publish.config.json and fill it in.
# That file is gitignored. Parameters override it. An empty password is typed interactively.
# Usage: .\publish\publish.ps1 [-Arch amd64|arm64] [-Version v1] [-SkipFrontend]
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = 'amd64',
    [string]$Version = '',
    [switch]$SkipFrontend,
    [switch]$SkipBuild,
    [switch]$SkipRestart,
    [switch]$Fast,
    [ValidateSet('all', 'config-center', 'config-center-access', 'sdk-core', 'sdk-service', 'admin')]
    [string]$Service = 'all',
    [ValidatePattern('^[A-Za-z0-9][A-Za-z0-9.-]*$')]
    [string]$ServerHost,
    [ValidateRange(1, 65535)]
    [int]$Port,
    [ValidatePattern('^[A-Za-z_][A-Za-z0-9_-]*$')]
    [string]$User,
    [ValidatePattern('^/[A-Za-z0-9_/-]+$')]
    [string]$RemoteDirectory
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
if ($Service -eq 'admin' -and $SkipFrontend) { throw '发布 admin 时不能跳过前端构建。' }
if ($SkipBuild -and [string]::IsNullOrWhiteSpace($Version)) { throw '-SkipBuild 必须同时指定 -Version，以准确选择已存在的归档。' }

$ConfigPath = Join-Path $PSScriptRoot 'publish.config.json'

function Read-PublishConfig {
    if (-not (Test-Path -LiteralPath $ConfigPath -PathType Leaf)) {
        throw "Missing ${ConfigPath}. Copy publish.config.example.json to publish.config.json and fill it in."
    }
    $utf8 = New-Object System.Text.UTF8Encoding($false, $true)
    try {
        $text = [System.IO.File]::ReadAllText($ConfigPath, $utf8)
    } catch {
        throw "Cannot read ${ConfigPath}: $($_.Exception.Message)"
    }
    try {
        $parsed = $text | ConvertFrom-Json
    } catch {
        throw "Invalid JSON in ${ConfigPath}: $($_.Exception.Message)"
    }
    if ($parsed -isnot [System.Management.Automation.PSCustomObject]) {
        throw "Publish config must be a JSON object: $ConfigPath"
    }
    return $parsed
}

function Get-ConfigProperty {
    param($Config, [string]$Name)
    if ($null -eq $Config) { return $null }
    return $Config.PSObject.Properties[$Name]
}

function Get-ConfigString {
    param($Config, [string]$Name)
    $property = Get-ConfigProperty $Config $Name
    if ($null -eq $property -or $null -eq $property.Value -or ($property.Value -is [string] -and $property.Value -eq '')) {
        throw "$Name in ${ConfigPath} is required. See publish.config.example.json."
    }
    if ($property.Value -isnot [string]) {
        throw "$Name in ${ConfigPath} must be a string."
    }
    return [string]$property.Value
}

# Supervisor names and health ports are local-only settings; never upload the full config.
function Get-RestartPlan {
    param($Config, [string]$SelectedService)

    $property = Get-ConfigProperty $Config 'services'
    if ($null -eq $property -or $property.Value -isnot [System.Management.Automation.PSCustomObject]) {
        throw "publish.config.json 必须配置 services 对象（Supervisor 进程名: 健康检查端口）。"
    }
    $selected = New-Object 'System.Collections.Generic.List[string]'
    foreach ($item in $property.Value.PSObject.Properties) {
        $name = [string]$item.Name
        if ($name -match '^(config-center-access|config-center|sdk-core|sdk-service)(-[1-9][0-9]*)?$') {
            $module = $Matches[1]
        } else {
            throw "services 包含不支持的进程名: $name"
        }
        $value = $item.Value
        if (($value -isnot [int] -and $value -isnot [long]) -or $value -lt 1 -or $value -gt 65535) {
            throw "services.$name 的端口必须为 1 到 65535 的整数"
        }
        if ($SelectedService -eq 'all' -or $SelectedService -eq $module) {
            $selected.Add(('{0}:{1}' -f $name, $value))
        }
    }
    if ($selected.Count -eq 0) {
        throw "services 未配置 $SelectedService 对应的 Supervisor 进程，无法执行自动重启。"
    }
    return ($selected -join ',')
}

function Test-ByteMatch {
    param([byte[]]$Actual, [byte[]]$Expected)
    if ($Actual.Length -ne $Expected.Length) { return $false }
    for ($i = 0; $i -lt $Actual.Length; $i++) {
        if ($Actual[$i] -ne $Expected[$i]) { return $false }
    }
    return $true
}

function New-PublishAskPass {
    param([string]$Stage)
    # csc keeps the helper out of this process, so the temp directory can be deleted.
    # Add-Type would load and lock the exe until PowerShell exits.
    if (-not $env:SystemRoot) { throw 'Password helper requires Windows; use SSH keys or interactive password on this host.' }
    $csc = Join-Path $env:SystemRoot 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
    if (-not (Test-Path -LiteralPath $csc)) {
        $csc = Join-Path $env:SystemRoot 'Microsoft.NET\Framework\v4.0.30319\csc.exe'
    }
    if (-not (Test-Path -LiteralPath $csc)) {
        throw 'Could not find csc.exe. Leave password empty to type it interactively.'
    }
    $sourcePath = Join-Path $Stage 'askpass.cs'
    $exePath = Join-Path $Stage 'askpass.exe'
    # Host-key confirmation and password prompts both go through SSH_ASKPASS when it is
    # forced. Answer only password prompts so a new host key is never accepted blindly.
    $source = @'
using System;
using System.Text;

public class PublishAskPass {
    public static int Main(string[] args) {
        string prompt = args.Length > 0 ? args[0] : "";
        if (prompt.IndexOf("password", StringComparison.OrdinalIgnoreCase) < 0) {
            Console.Error.WriteLine("Refusing to answer a non-password SSH prompt. Verify the host key with ssh once, then publish again.");
            return 2;
        }
        string password = Environment.GetEnvironmentVariable("PUBLISH_SSH_PASSWORD");
        if (password == null) {
            Console.Error.WriteLine("PUBLISH_SSH_PASSWORD is not set.");
            return 2;
        }
        byte[] bytes = new UTF8Encoding(false).GetBytes(password + "\n");
        using (var stdout = Console.OpenStandardOutput()) {
            stdout.Write(bytes, 0, bytes.Length);
        }
        return 0;
    }
}
'@
    [System.IO.File]::WriteAllText($sourcePath, $source, (New-Object System.Text.UTF8Encoding($false)))
    $compile = & $csc /nologo /optimize+ /target:exe "/out:$exePath" $sourcePath 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) {
        throw "Could not build the SSH password helper. Leave password empty to type it interactively. $compile"
    }
    return $exePath
}

function Assert-PublishAskPass {
    param([string]$Exe, [string]$Password)
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    $directory = Split-Path -Parent $Exe
    $accepted = Join-Path $directory 'askpass-ok.bin'
    $rejected = Join-Path $directory 'askpass-no.bin'
    $acceptedError = Join-Path $directory 'askpass-ok.err'
    $rejectedError = Join-Path $directory 'askpass-no.err'
    $ok = Start-Process -FilePath $Exe -ArgumentList 'password:' -WorkingDirectory $directory -RedirectStandardOutput $accepted -RedirectStandardError $acceptedError -NoNewWindow -Wait -PassThru
    $actual = [System.IO.File]::ReadAllBytes($accepted)
    $expected = $utf8.GetBytes($Password + "`n")
    if ($ok.ExitCode -ne 0 -or -not (Test-ByteMatch $actual $expected)) {
        throw 'SSH password helper failed its self-check.'
    }
    $no = Start-Process -FilePath $Exe -ArgumentList 'continue?' -WorkingDirectory $directory -RedirectStandardOutput $rejected -RedirectStandardError $rejectedError -NoNewWindow -Wait -PassThru
    if ($no.ExitCode -eq 0) {
        throw 'SSH password helper answered a non-password prompt.'
    }
}

function Restore-EnvValue {
    param([string]$Name, $Value)
    if ($null -eq $Value -or $Value -eq '') {
        Remove-Item "Env:$Name" -ErrorAction SilentlyContinue
    } else {
        Set-Item "Env:$Name" -Value $Value
    }
}

foreach ($command in @('ssh', 'scp')) {
    if (-not (Get-Command $command -ErrorAction SilentlyContinue)) {
        throw "Required command not found: $command. Install the OpenSSH client first."
    }
}

$config = Read-PublishConfig
if ($PSBoundParameters.ContainsKey('ServerHost')) {
    $ServerHost = [string]$PSBoundParameters['ServerHost']
} else {
    $ServerHost = Get-ConfigString $config 'host'
    if ($ServerHost -notmatch '^[A-Za-z0-9][A-Za-z0-9.-]*$') {
        throw "Invalid host in ${ConfigPath}: $ServerHost"
    }
}
if ($PSBoundParameters.ContainsKey('Port')) {
    $Port = [int]$PSBoundParameters['Port']
} else {
    $portProperty = Get-ConfigProperty $config 'port'
    if ($null -eq $portProperty -or $null -eq $portProperty.Value -or ($portProperty.Value -is [string] -and $portProperty.Value -eq '')) {
        throw "port in ${ConfigPath} is required. See publish.config.example.json."
    } elseif ($portProperty.Value -isnot [int] -and $portProperty.Value -isnot [long]) {
        throw "port in ${ConfigPath} must be an integer."
    } else {
        $Port = [int]$portProperty.Value
        if ($Port -lt 1 -or $Port -gt 65535) {
            throw "Invalid port in ${ConfigPath}: $Port"
        }
    }
}
if ($PSBoundParameters.ContainsKey('User')) {
    $User = [string]$PSBoundParameters['User']
} else {
    $User = Get-ConfigString $config 'user'
    if ($User -notmatch '^[A-Za-z_][A-Za-z0-9_-]*$') {
        throw "Invalid user in ${ConfigPath}: $User"
    }
}
if ($PSBoundParameters.ContainsKey('RemoteDirectory')) {
    $RemoteDirectory = [string]$PSBoundParameters['RemoteDirectory']
} else {
    $RemoteDirectory = Get-ConfigString $config 'remoteDirectory'
}
$RemoteDirectory = $RemoteDirectory.TrimEnd('/')
if ($RemoteDirectory -notmatch '^/[A-Za-z0-9_/-]+$' -or $RemoteDirectory -match '(^|/)\.\.?(/|$)') {
    throw 'RemoteDirectory must be an absolute non-root directory without dot segments.'
}
$passwordProperty = Get-ConfigProperty $config 'password'
if ($null -eq $passwordProperty -or $null -eq $passwordProperty.Value) {
    $Password = ''
} elseif ($passwordProperty.Value -isnot [string]) {
    throw "password in ${ConfigPath} must be a string."
} elseif ($passwordProperty.Value -match '[\r\n]') {
    throw "password in ${ConfigPath} must be a single line."
} else {
    $Password = [string]$passwordProperty.Value
}

$restartPlan = if ($SkipRestart -or $Service -eq 'admin') {
    '-'
} else {
    Get-RestartPlan $config $Service
}

$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$remoteScript = Join-Path $PSScriptRoot 'publish-remote.sh'
if (-not (Test-Path -LiteralPath $remoteScript -PathType Leaf)) {
    throw "Missing remote extraction script: $remoteScript"
}

# Pass an explicit version so a failed build can never select an older archive.
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = 'publish-' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '-' + [guid]::NewGuid().ToString('N').Substring(0, 8)
}
if ($Version -notmatch '^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$') {
    throw "不合法的发布版本号: $Version"
}

$localStage = $null
$publishAuthTouched = $false
$createdDisplay = $false
$priorSshAskPass = $null
$priorAskPassRequire = $null
$priorDisplay = $null
try {
    $localStage = Join-Path ([System.IO.Path]::GetTempPath()) ('middle-platform-publish-' + [guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $localStage | Out-Null
    $sshOptions = @()
    $askPassExe = $null
    if ($Password -ne '') {
        $askPassExe = New-PublishAskPass $localStage
        # Self-check needs the password, but the build's go/npm children must not inherit it.
        $env:PUBLISH_SSH_PASSWORD = $Password
        try {
            Assert-PublishAskPass $askPassExe $Password
        } finally {
            Remove-Item Env:PUBLISH_SSH_PASSWORD -ErrorAction SilentlyContinue
        }
        # Skip private keys so an encrypted local key cannot steal the password prompt.
        $sshOptions = @(
            '-o', 'PreferredAuthentications=password,keyboard-interactive',
            '-o', 'PubkeyAuthentication=no',
            '-o', 'NumberOfPasswordPrompts=1'
        )
        Write-Host 'Using the password from publish.config.json.'
    }

    if (-not $SkipBuild) {
        & (Join-Path $PSScriptRoot 'package-local.ps1') -Arch $Arch -Version $Version -SkipFrontend:$SkipFrontend -Service $Service -Fast:$Fast
    } else {
        Write-Host "跳过构建，使用已存在的 $Version 归档。"
    }
    $archiveName = "middle-platform-$Version-linux-$Arch.tar.gz"
    $archivePath = Join-Path (Join-Path $repoRoot 'dist') $archiveName
    if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
        throw "Build did not produce the expected archive: $archivePath"
    }
    $hashStream = [System.IO.File]::OpenRead($archivePath)
    $hasher = [System.Security.Cryptography.SHA256]::Create()
    try {
        $sha256 = ([System.BitConverter]::ToString($hasher.ComputeHash($hashStream))).Replace('-', '').ToLowerInvariant()
    } finally {
        $hashStream.Dispose()
        $hasher.Dispose()
    }
    $destination = "$User@$ServerHost"
    $remoteStage = "$RemoteDirectory/.publish-$([guid]::NewGuid().ToString('N'))"

    function Invoke-Remote {
        param([string]$Command)
        & ssh @sshOptions -p $Port $destination $Command
        if ($LASTEXITCODE -ne 0) {
            throw "Remote command failed (exit $LASTEXITCODE). Upload directory: $remoteStage"
        }
    }

    Write-Host "Uploading to ${destination}:$RemoteDirectory (SSH port $Port)."
    if ($Password -eq '') {
        Write-Host 'SSH may request a password more than once. Existing host-key verification is retained.'
    } else {
        $priorSshAskPass = $env:SSH_ASKPASS
        $priorAskPassRequire = $env:SSH_ASKPASS_REQUIRE
        $priorDisplay = $env:DISPLAY
        $env:PUBLISH_SSH_PASSWORD = $Password
        $env:SSH_ASKPASS = $askPassExe
        $env:SSH_ASKPASS_REQUIRE = 'force'
        if ([string]::IsNullOrEmpty($priorDisplay)) {
            $env:DISPLAY = 'localhost:0.0'
            $createdDisplay = $true
        }
        $publishAuthTouched = $true
        Write-Host 'Existing host-key verification is retained. A new host key must be accepted with ssh first.'
    }
    Invoke-Remote "mkdir -p -- '$RemoteDirectory' && mkdir -- '$remoteStage'"

    # Upload an LF-only script even when the checkout uses Windows line endings.
    $uploadScript = Join-Path $localStage 'publish-remote.sh'
    $scriptText = [System.IO.File]::ReadAllText($remoteScript).Replace("`r`n", "`n")
    [System.IO.File]::WriteAllText($uploadScript, $scriptText, (New-Object System.Text.UTF8Encoding($false)))
    & scp @sshOptions -P $Port $archivePath $uploadScript "${destination}:${remoteStage}/"
    if ($LASTEXITCODE -ne 0) {
        throw "Upload failed (exit $LASTEXITCODE). No application files were updated. Temporary directory: $remoteStage"
    }
    $restartFlag = if ($SkipRestart -or $Service -eq 'admin') { '0' } else { '1' }
    Invoke-Remote "bash '$remoteStage/publish-remote.sh' '$RemoteDirectory' '$remoteStage/$archiveName' '$sha256' '$restartFlag' '$Service' '$restartPlan'"
} finally {
    if ($publishAuthTouched) {
        Restore-EnvValue 'PUBLISH_SSH_PASSWORD' $null
        Restore-EnvValue 'SSH_ASKPASS' $priorSshAskPass
        Restore-EnvValue 'SSH_ASKPASS_REQUIRE' $priorAskPassRequire
        if ($createdDisplay) {
            Restore-EnvValue 'DISPLAY' $null
        }
    }
    if ($localStage -and (Test-Path -LiteralPath $localStage)) {
        try {
            Remove-Item -LiteralPath $localStage -Recurse -Force -ErrorAction Stop
        } catch {
            Write-Warning "Could not delete temporary directory: $localStage"
        }
    }
}

Write-Host ''
Write-Host "Published archive: $RemoteDirectory/packages/$archiveName"
Write-Host '发布包已更新到服务器，未执行数据库 SQL。'
if ($SkipRestart -or $Service -eq 'admin') {
    Write-Host '已跳过 Supervisor 重启，现有进程仍需人工重启才能加载新二进制。'
} else {
    Write-Host '已执行 Supervisor 重启与健康检查；失败会中止发布并报错，不提供自动回滚。'
}
