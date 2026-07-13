# E2E test: user submit feedback + admin list/patch
param(
  [string]$ApiBase = "https://backend-production-bfaa.up.railway.app",
  [string]$AdminEmail = ""
)

$ErrorActionPreference = "Stop"

function Invoke-Api {
  param(
    [string]$Method,
    [string]$Path,
    [hashtable]$Body = $null,
    [string]$Token = ""
  )
  $headers = @{ "Content-Type" = "application/json" }
  if ($Token) { $headers["Authorization"] = "Bearer $Token" }

  $uri = "$ApiBase$Path"
  if ($Body) {
    $json = $Body | ConvertTo-Json -Compress
    return Invoke-RestMethod -Method $Method -Uri $uri -Headers $headers -Body $json
  }
  return Invoke-RestMethod -Method $Method -Uri $uri -Headers $headers
}

$stamp = Get-Date -Format "yyyyMMddHHmmss"
$testEmail = if ($AdminEmail) { $AdminEmail } else { "dadiary-fb-e2e-$stamp@example.com" }
$password = "TestFeedback123!"

Write-Host "==> API: $ApiBase"
Write-Host "==> Test user: $testEmail"

# 1) Register (ignore if already exists)
try {
  $reg = Invoke-Api -Method POST -Path "/api/v1/auth/register" -Body @{
    email    = $testEmail
    password = $password
    display_name = "Feedback E2E"
  }
  Write-Host "[OK] Register"
} catch {
  Write-Host "[SKIP] Register (may already exist): $($_.Exception.Message)"
}

# 2) Login
$login = Invoke-Api -Method POST -Path "/api/v1/auth/login" -Body @{
  email    = $testEmail
  password = $password
}
$token = $login.data.tokens.access_token
if (-not $token) { $token = $login.tokens.access_token }
if (-not $token) { throw "No access token from login" }
Write-Host "[OK] Login"

# 3) /me — check is_admin when email is in DADIARY_ADMIN_EMAILS
$me = Invoke-Api -Method GET -Path "/api/v1/me" -Token $token
$isAdmin = [bool]$me.data.is_admin
if (-not $me.data) { $isAdmin = [bool]$me.is_admin; $meEmail = $me.email } else { $meEmail = $me.data.email }
Write-Host "[INFO] /me email=$meEmail is_admin=$isAdmin"

# 4) User submits feedback
$created = Invoke-Api -Method POST -Path "/api/v1/feedbacks" -Token $token -Body @{
  type    = "bug_report"
  comment = "E2E test feedback at $stamp — nút gửi góp ý hoạt động tốt."
}
$feedbackId = $created.data.id
if (-not $feedbackId) { $feedbackId = $created.id }
Write-Host "[OK] POST /feedbacks id=$feedbackId"

if (-not $isAdmin) {
  Write-Host "[WARN] User is not admin — skipping admin endpoints (set DADIARY_ADMIN_EMAILS=$testEmail on Railway)"
  exit 0
}

# 5) Admin list
$list = Invoke-Api -Method GET -Path "/api/v1/admin/feedbacks?page=1&page_size=20&type=bug_report" -Token $token
$items = $list.data.items
if (-not $items) { $items = $list.items }
$count = if ($list.data.total) { $list.data.total } else { $list.total }
Write-Host "[OK] GET /admin/feedbacks total=$count items=$($items.Count)"

$found = $items | Where-Object { $_.id -eq $feedbackId }
if (-not $found) { throw "Submitted feedback not found in admin list" }
Write-Host "[OK] Feedback visible in admin list"

# 6) Admin patch status
$patched = Invoke-Api -Method PATCH -Path "/api/v1/admin/feedbacks/$feedbackId" -Token $token -Body @{
  status = "read"
}
$status = if ($patched.data.status) { $patched.data.status } else { $patched.status }
if ($status -ne "read") { throw "Expected status=read, got $status" }
Write-Host "[OK] PATCH /admin/feedbacks/$feedbackId status=read"

Write-Host "==> ALL FEEDBACK E2E CHECKS PASSED"
