# Add api.dadiary.vn on Railway, print PA Vietnam DNS records, update Vercel API URL.
#
# Auth (do NOT paste token in chat):
#   cd backend
#   railway login
#   # or: $env:RAILWAY_TOKEN = "project-token-from-railway-settings"
#
# Usage:
#   pwsh -File scripts/setup-api-domain.ps1
#   pwsh -File scripts/setup-api-domain.ps1 -SkipVercel   # DNS only, no Vercel env change

param(
    [switch]$SkipDnsWait,
    [switch]$SkipVercel
)

$ErrorActionPreference = "Stop"

$ProjectID = "d256f6f2-651d-4c8a-b880-95e19c9ce09c"
$Service = "backend"
$Environment = "production"
$ApiDomain = "api.dadiary.vn"
$ApiUrl = "https://$ApiDomain"
$HealthPath = "/api/v1/health"

$backendRoot = Split-Path $PSScriptRoot -Parent
$frontendRoot = Join-Path (Split-Path $backendRoot -Parent) "frontend"

if (-not $env:RAILWAY_TOKEN -and -not $env:RAILWAY_API_TOKEN) {
    Write-Error @"
Chưa login Railway. Chạy một trong hai:
  railway login
  `$env:RAILWAY_TOKEN = '<project-token>'   # Railway → Project → Settings → Tokens
"@
}

Write-Host "=== Railway: add custom domain $ApiDomain ===" -ForegroundColor Cyan
railway whoami

$domainJson = railway domain $ApiDomain `
    --service $Service `
    --environment $Environment `
    --project $ProjectID `
    --json 2>&1 | Out-String

Write-Host $domainJson

Write-Host ""
Write-Host "=== PA Việt Nam — thêm 2 bản ghi (Cấu hình bản ghi tên miền) ===" -ForegroundColor Yellow
Write-Host "Copy đúng CNAME + TXT từ output Railway phía trên."
Write-Host "  api  | CNAME | <xxxxx.up.railway.app>"
Write-Host "  <txt host Railway đưa> | TXT | <txt value Railway đưa>"
Write-Host "Thiếu TXT → api.dadiary.vn trả 404."
Write-Host ""

if ($SkipDnsWait) {
    Write-Host "SkipDnsWait: bỏ qua chờ DNS. Chạy lại script (không -SkipDnsWait) sau khi DNS xong."
    exit 0
}

Write-Host "Sau khi Lưu cấu hình trên PA Việt Nam, nhấn Enter để kiểm tra DNS + health..." -ForegroundColor Green
[void][System.Console]::ReadLine()

$maxAttempts = 36
$ok = $false
for ($i = 1; $i -le $maxAttempts; $i++) {
    Write-Host "[$i/$maxAttempts] GET $ApiUrl$HealthPath"
    try {
        $resp = Invoke-WebRequest -Uri "$ApiUrl$HealthPath" -Method GET -TimeoutSec 15 -UseBasicParsing
        if ($resp.StatusCode -ge 200 -and $resp.StatusCode -lt 300) {
            Write-Host "OK — API health: $($resp.StatusCode)" -ForegroundColor Green
            $ok = $true
            break
        }
    } catch {
        Write-Host "Chưa sẵn sàng: $($_.Exception.Message)"
    }
    Start-Sleep -Seconds 10
}

if (-not $ok) {
    Write-Error "api.dadiary.vn chưa trả health OK. Kiểm tra CNAME + TXT trên PA Việt Nam, đợi propagate, chạy lại script."
}

if ($SkipVercel) {
    Write-Host "SkipVercel: bỏ qua cập nhật Vercel."
    exit 0
}

Write-Host ""
Write-Host "=== Vercel: NEXT_PUBLIC_API_URL -> $ApiUrl ===" -ForegroundColor Cyan
Push-Location $frontendRoot
try {
    vercel whoami | Out-Null
    vercel env rm NEXT_PUBLIC_API_URL production -y 2>$null
    vercel env add NEXT_PUBLIC_API_URL production --value $ApiUrl --yes --force
    Write-Host "Redeploy production..."
    vercel deploy --prod
    Write-Host ""
    Write-Host "Xong. Frontend gọi API qua $ApiUrl" -ForegroundColor Green
} finally {
    Pop-Location
}
