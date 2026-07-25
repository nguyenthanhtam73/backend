# Flip SePay from sandbox -> production on Railway (backend / production).
#
# Prerequisites:
#   - Legitimate live Merchant ID + Secret (not SP-TEST-... / spsk_test_...)
#   - SePay dashboard IPN already pointed at:
#       https://backend-production-bfaa.up.railway.app/api/v1/payment/sepay/webhook
#   - railway CLI logged in (railway whoami) OR RAILWAY_TOKEN / RAILWAY_API_TOKEN
#
# Usage (from repo root or backend/):
#   powershell -File backend/scripts/flip-sepay-production.ps1
#   powershell -File backend/scripts/flip-sepay-production.ps1 -MerchantId "SP-..." -SecretKey "spsk_..."
#
# Reads Merchant/Secret from args, else repo-root .env (must already be production values).

param(
    [string]$MerchantId = "",
    [string]$SecretKey = "",
    [switch]$SkipRailway,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"

$ProjectID = "d256f6f2-651d-4c8a-b880-95e19c9ce09c"
$Service = "backend"
$Environment = "production"
$ApiBase = "https://backend-production-bfaa.up.railway.app"
$WebBase = "https://dadiary.vn"

$root = (Resolve-Path (Join-Path $PSScriptRoot "..\..")).Path
$envPath = Join-Path $root ".env"

function Read-DotEnvValue([string]$path, [string]$key) {
    if (-not (Test-Path $path)) { return "" }
    foreach ($line in Get-Content $path) {
        $t = $line.Trim()
        if ($t -eq "" -or $t.StartsWith("#")) { continue }
        $idx = $t.IndexOf("=")
        if ($idx -lt 1) { continue }
        if ($t.Substring(0, $idx).Trim() -eq $key) {
            return $t.Substring($idx + 1).Trim()
        }
    }
    return ""
}

if ([string]::IsNullOrWhiteSpace($MerchantId)) {
    $MerchantId = Read-DotEnvValue $envPath "DADIARY_SEPAY_MERCHANT_ID"
}
if ([string]::IsNullOrWhiteSpace($SecretKey)) {
    $SecretKey = Read-DotEnvValue $envPath "DADIARY_SEPAY_SECRET_KEY"
}

if ([string]::IsNullOrWhiteSpace($MerchantId) -or [string]::IsNullOrWhiteSpace($SecretKey)) {
    throw "Missing live SePay credentials. Pass -MerchantId / -SecretKey, or set DADIARY_SEPAY_MERCHANT_ID and DADIARY_SEPAY_SECRET_KEY in .env"
}

if ($MerchantId -like "SP-TEST-*") {
    throw "Refusing sandbox merchant id ($MerchantId). Need live Merchant ID."
}
if ($SecretKey -like "spsk_test_*") {
    throw "Refusing sandbox secret (spsk_test_*). Need live Secret Key."
}

$toSet = [ordered]@{
    "DADIARY_SEPAY_ENV"         = "production"
    "DADIARY_SEPAY_MERCHANT_ID" = $MerchantId
    "DADIARY_SEPAY_SECRET_KEY"  = $SecretKey
    "DADIARY_PUBLIC_WEB_URL"    = $WebBase
    "DADIARY_SEPAY_SUCCESS_URL" = "$WebBase/payment/success"
    "DADIARY_SEPAY_ERROR_URL"   = "$WebBase/payment/error"
    "DADIARY_SEPAY_CANCEL_URL"  = "$WebBase/payment/cancel"
    "DADIARY_PUBLIC_API_URL"    = $ApiBase
}

Write-Host "Flip SePay -> production"
Write-Host "  merchant=$MerchantId"
Write-Host "  secret=***len=$($SecretKey.Length)"
Write-Host "  callbacks=$WebBase/payment/*"
Write-Host "  api=$ApiBase"

# Patch local .env (keep other keys)
if (Test-Path $envPath) {
    $lines = Get-Content $envPath
    $keysDone = @{}
    $out = foreach ($line in $lines) {
        $t = $line.Trim()
        if ($t -eq "" -or $t.StartsWith("#")) { $line; continue }
        $idx = $t.IndexOf("=")
        if ($idx -lt 1) { $line; continue }
        $k = $t.Substring(0, $idx).Trim()
        if ($toSet.Contains($k)) {
            $keysDone[$k] = $true
            "{0}={1}" -f $k, $toSet[$k]
        } else {
            $line
        }
    }
    foreach ($k in $toSet.Keys) {
        if (-not $keysDone.ContainsKey($k)) {
            $out += "{0}={1}" -f $k, $toSet[$k]
        }
    }
    if ($DryRun) {
        Write-Host "[dry-run] would update $envPath"
    } else {
        Set-Content -Path $envPath -Value ($out -join "`n") -Encoding UTF8
        Write-Host "Updated $envPath"
    }
} else {
    Write-Warning ".env not found at $envPath - skipping local write"
}

if ($SkipRailway) {
    Write-Host "SkipRailway set - not touching Railway."
    exit 0
}

railway whoami | Out-Null

foreach ($k in $toSet.Keys) {
    $val = $toSet[$k]
    if ($DryRun) {
        Write-Host "[dry-run] SET $k"
        continue
    }
    Write-Host "SET $k"
    railway variable set "${k}=${val}" `
        --project $ProjectID `
        --service $Service `
        --environment $Environment `
        --json | Out-Null
}

# E2E helpers must not stay on production
$varJson = railway variable list --project $ProjectID --service $Service --environment $Environment --json | ConvertFrom-Json
if ($varJson.PSObject.Properties.Name -contains "DADIARY_E2E_SECRET") {
    if ($DryRun) {
        Write-Host "[dry-run] DELETE DADIARY_E2E_SECRET"
    } else {
        Write-Host "DELETE DADIARY_E2E_SECRET"
        railway variable delete DADIARY_E2E_SECRET `
            --project $ProjectID `
            --service $Service `
            --environment $Environment `
            --yes 2>$null
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "Could not delete DADIARY_E2E_SECRET - unset it manually in Railway UI."
        }
    }
}

Write-Host ""
Write-Host "Next (manual):"
Write-Host "  1. SePay IPN = $ApiBase/api/v1/payment/sepay/webhook (SECRET_KEY = live secret)"
Write-Host "  2. Wait for Railway redeploy / restart"
Write-Host "  3. Smoke: login -> $WebBase/pricing -> pay small amount -> /payment/success -> /me premium"
Write-Host "  4. Full checklist: backend/docs/PRODUCTION-CHECKLIST.md"
Write-Host "Done."
