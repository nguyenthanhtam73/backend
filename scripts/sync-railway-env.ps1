# Sync DADIARY_* vars from repo-root .env to Railway (backend / production).
# Requires auth first (do NOT paste token in chat):
#   $env:RAILWAY_TOKEN = "your-token"
#   railway whoami
#
# Usage (from backend/):
#   pwsh -File scripts/sync-railway-env.ps1

$ErrorActionPreference = "Stop"

$ProjectID = "d256f6f2-651d-4c8a-b880-95e19c9ce09c"
$Service = "backend"
$Environment = "production"

if (-not $env:RAILWAY_TOKEN -and -not $env:RAILWAY_API_TOKEN) {
    Write-Error "Set RAILWAY_TOKEN (project token) or RAILWAY_API_TOKEN (account token) first, then run: railway whoami"
}

$root = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$envPath = Join-Path $root ".env"
if (-not (Test-Path $envPath)) {
    Write-Error ".env not found at $envPath"
}

$vars = @{}
Get-Content $envPath | ForEach-Object {
    $line = $_.Trim()
    if ($line -eq "" -or $line.StartsWith("#")) { return }
    $idx = $line.IndexOf("=")
    if ($idx -lt 1) { return }
    $key = $line.Substring(0, $idx).Trim()
    $val = $line.Substring($idx + 1).Trim()
    if ($key.StartsWith("DADIARY_")) {
        $vars[$key] = $val
    }
}

# Never overwrite Railway Postgres wiring from local docker URL.
if ($vars["DADIARY_DATABASE_URL"] -match "localhost") {
    $vars.Remove("DADIARY_DATABASE_URL")
}

# Production defaults
$vars["DADIARY_ENV"] = "production"
if (-not $vars.ContainsKey("DADIARY_OPENAI_MODEL")) { $vars["DADIARY_OPENAI_MODEL"] = "gpt-4o" }
if (-not $vars.ContainsKey("DADIARY_OPENAI_VISION_MODEL")) { $vars["DADIARY_OPENAI_VISION_MODEL"] = "gpt-4o" }
if (-not $vars.ContainsKey("DADIARY_ANTHROPIC_MODEL")) { $vars["DADIARY_ANTHROPIC_MODEL"] = "claude-sonnet-4-6" }

Write-Host "Railway sync -> project=$ProjectID service=$Service env=$Environment"
railway whoami

foreach ($key in ($vars.Keys | Sort-Object)) {
    $val = $vars[$key]
    if ([string]::IsNullOrWhiteSpace($val)) {
        Write-Host "SKIP (empty): $key"
        continue
    }
    Write-Host "SET $key"
    railway variable set "${key}=$val" `
        --project $ProjectID `
        --service $Service `
        --environment $Environment `
        --json | Out-Null
}

Write-Host "Done. Current variables (keys only):"
railway variable list --project $ProjectID --service $Service --environment $Environment --json |
    ConvertFrom-Json |
    ForEach-Object { $_.name }
