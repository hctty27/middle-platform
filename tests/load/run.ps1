[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$BaseUrl,

    [ValidateSet("smoke", "config-center", "sdk-core", "sdk-core-orders", "sdk-service-user-check", "mixed", "ramp", "soak", "availability-watch")]
    [string]$Scenario = "smoke",

    [ValidateRange(1, 200)]
    [int]$MaxRps = 5,

    [string]$Duration = "5m",

    [switch]$ConfirmProduction,
    [switch]$AllowOrderWrites,
    [switch]$ConfirmOrderNotifySink,
    [switch]$AllowServiceUserCheck,
    [switch]$ConfirmSkipUserCheck,
    [switch]$IncludeServiceInMix
)

$ErrorActionPreference = "Stop"

$localConfig = Join-Path $PSScriptRoot "config.local.ps1"
if (Test-Path $localConfig) {
    . $localConfig
}

if (-not $ConfirmProduction) {
    throw "This runner targets the production ingress. Re-run with -ConfirmProduction after checking the maintenance window."
}

$uri = [Uri]$BaseUrl
if ($uri.Scheme -ne "https" -or $uri.AbsolutePath -ne "/" -or $uri.UserInfo) {
    throw "BaseUrl must be an HTTPS origin only, for example https://sdk.example.com"
}

if ($Duration -notmatch '^\d+[smh]$') {
    throw "Duration must use a simple value such as 5m, 30m, or 1h."
}

if (-not (Get-Command k6 -ErrorAction SilentlyContinue)) {
    throw "k6 was not found. Install it first: winget install k6 --source winget"
}

function Require-Env([string]$Name) {
    $value = [Environment]::GetEnvironmentVariable($Name)
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "Missing environment variable: $Name. Copy config.example.ps1 to config.local.ps1 and fill TEST credentials."
    }
}

function Require-TestEnv([string]$Name) {
    Require-Env $Name
    if ([Environment]::GetEnvironmentVariable($Name) -ne "test") {
        throw "$Name must be test. Do not send test scripts to prod/review game environments."
    }
}

$needsConfig = @("smoke", "config-center", "mixed", "ramp", "soak", "availability-watch")
$needsCore = @("smoke", "sdk-core", "sdk-core-orders", "mixed", "ramp", "soak", "availability-watch")
if ($needsConfig -contains $Scenario) {
    Require-Env "CONFIG_SUBSCRIBER_CODE"
    Require-Env "CONFIG_SUBSCRIBER_SECRET"
}
if ($needsCore -contains $Scenario) {
    Require-TestEnv "CORE_ENV"
    Require-Env "CORE_APP_ID"
}
if ($Scenario -eq "sdk-core-orders") {
    if (-not $AllowOrderWrites -or -not $ConfirmOrderNotifySink) {
        throw "Order test requires both -AllowOrderWrites and -ConfirmOrderNotifySink."
    }
    Require-Env "CORE_GAME_SECRET"
    Require-Env "ORDER_ID_PREFIX"
    $env:ALLOW_ORDER_WRITES = "true"
    $env:ORDER_NOTIFY_SINK_CONFIRMED = "true"
}
if ($Scenario -eq "sdk-service-user-check" -or $IncludeServiceInMix) {
    if (-not $AllowServiceUserCheck -or -not $ConfirmSkipUserCheck) {
        throw "SDK Service test requires -AllowServiceUserCheck and -ConfirmSkipUserCheck."
    }
    Require-TestEnv "SDK_SERVICE_ENV"
    Require-Env "SDK_SERVICE_APP_ID"
    Require-Env "SDK_SERVICE_GAME_SECRET"
    Require-Env "OPEN_ID_PREFIX"
    Require-Env "SDK_SERVICE_SKIP_USER_CHECK_CONFIRMED"
    if ([Environment]::GetEnvironmentVariable("SDK_SERVICE_SKIP_USER_CHECK_CONFIRMED") -ne "true") {
        throw "Set SDK_SERVICE_SKIP_USER_CHECK_CONFIRMED=true only after verifying the TEST channel does not call the real platform."
    }
}
if ($Scenario -eq "sdk-service-user-check" -and $IncludeServiceInMix) {
    throw "Choose sdk-service-user-check or -IncludeServiceInMix, not both."
}

if ($Scenario -eq "availability-watch") {
    $MaxRps = 1
}
if ($Scenario -eq "sdk-core-orders" -and $MaxRps -gt 5) {
    throw "Order-write pressure is capped at 5 RPS to limit production database growth."
}
if ($Scenario -eq "sdk-service-user-check" -and $MaxRps -gt 50) {
    throw "SDK Service test traffic is capped at 50 RPS."
}

$env:BASE_URL = $BaseUrl.TrimEnd("/")
$env:MAX_RPS = [string]$MaxRps
$env:DURATION = $Duration
$env:LOADTEST_SCENARIO = $Scenario
$env:LOADTEST_RUN_ID = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssfffZ")
$env:MIX_INCLUDE_SERVICE = if ($IncludeServiceInMix) { "true" } else { "false" }

$reports = Join-Path $PSScriptRoot "reports"
New-Item -ItemType Directory -Force -Path $reports | Out-Null

Push-Location $PSScriptRoot
try {
    $scriptPath = Join-Path $PSScriptRoot ("scenarios\" + $Scenario + ".js")
    Write-Host "Scenario: $Scenario"
    Write-Host "Target: $BaseUrl"
    Write-Host "Max RPS: $MaxRps"
    Write-Host "Duration: $Duration"
    Write-Host "Run ID: $env:LOADTEST_RUN_ID"
    Write-Host "Reports: $reports"

    & k6 run --summary-mode=full $scriptPath
    if ($LASTEXITCODE -ne 0) {
        throw "k6 exited with code $LASTEXITCODE. Check thresholds and reports."
    }
}
finally {
    Pop-Location
}
