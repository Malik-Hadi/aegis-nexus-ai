$ErrorActionPreference = "Continue"
Set-Location (Split-Path $PSScriptRoot -Parent)
$root = (Get-Location).Path
$compose = @("compose","-f","deploy/docker-compose.yml","--env-file",".env")
function Say($m) { Write-Host "[aegis] $m" -ForegroundColor Cyan }
function Fail($m) { Write-Host "[aegis] STOP: $m" -ForegroundColor Red; exit 1 }

# 0. clear stale overrides, then load .env into this process (children inherit it)
foreach ($n in "POSTGRES_PORT","DATABASE_URL","POSTGRES_USER","POSTGRES_DB","POSTGRES_PASSWORD") { Remove-Item "Env:$n" -ErrorAction SilentlyContinue }
if (-not (Test-Path .env)) { Fail ".env is missing in $root" }
Get-Content .env | ForEach-Object {
  if ($_ -notmatch '^\s*#' -and $_ -match '^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$') {
    [Environment]::SetEnvironmentVariable($matches[1], $matches[2].Trim().Trim('"'), "Process") } }
$dbu = if ($env:POSTGRES_USER) { $env:POSTGRES_USER } else { "aegis" }
$dbn = if ($env:POSTGRES_DB) { $env:POSTGRES_DB } else { "aegis" }
function Q($sql) { (docker @compose exec -T db psql -U $dbu -d $dbn -t -A -c $sql 2>$null | Out-String).Trim() }

# 1. Docker engine
docker info 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) {
  Say "Starting Docker Desktop (can take a minute)..."
  Start-Process "$env:ProgramFiles\Docker\Docker\Docker Desktop.exe"
  for ($i=0; $i -lt 60; $i++) { Start-Sleep 3; docker info 2>$null | Out-Null; if ($LASTEXITCODE -eq 0) { break } }
  docker info 2>$null | Out-Null
  if ($LASTEXITCODE -ne 0) { Fail "Docker did not start. Open Docker Desktop manually and rerun." }
}

# 2. Database on the correct port
Say "Starting database..."
docker @compose up -d db | Out-Null
$ok = $false
for ($i=0; $i -lt 40; $i++) { if ((docker inspect --format "{{.State.Health.Status}}" deploy-db-1 2>$null) -eq "healthy") { $ok = $true; break }; Start-Sleep 2 }
if (-not $ok) { Fail "Database is not healthy. Run: docker compose -f deploy/docker-compose.yml --env-file .env logs db" }
if (-not ((docker @compose ps | Out-String) -match '5432->5432')) { Fail "Database is not published on port 5432. Run: docker compose -f deploy/docker-compose.yml --env-file .env down, then rerun." }

# 3. Schema and lab users (only if missing)
$tables = 0; [int]::TryParse((Q "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'"), [ref]$tables) | Out-Null
if ($tables -lt 12) {
  Say "Applying migrations 003 onwards (found $tables tables)..."
  Get-ChildItem db\migrations\*.sql | Where-Object { $_.Name -ge "003" } | Sort-Object Name | ForEach-Object {
    Get-Content $_.FullName -Raw | docker @compose exec -T db psql -U $dbu -d $dbn 2>&1 | Out-Null }
}
$users = 0; [int]::TryParse((Q "SELECT count(*) FROM users"), [ref]$users) | Out-Null
if ($users -eq 0) { Say "No lab users found, seeding..."; go run ./cmd/seed }

# 4. API
function ApiUp { try { (Invoke-WebRequest -UseBasicParsing "http://localhost:8080/readyz" -TimeoutSec 3).StatusCode -eq 200 } catch { $false } }
if (ApiUp) { Say "API already running." } else {
  Say "Starting API in a new window..."
  Start-Process powershell -ArgumentList "-NoExit","-Command","Set-Location '$root'; go run ./services/api"
  $up = $false; for ($i=0; $i -lt 45; $i++) { Start-Sleep 2; if (ApiUp) { $up = $true; break } }
  if (-not $up) { Fail "API did not become ready. Read the API window for the error." }
}

# 5. Dashboard
function DashUp { [bool](Get-NetTCPConnection -LocalPort 3000 -State Listen -ErrorAction SilentlyContinue) }
if (DashUp) { Say "Dashboard already running." } else {
  if (-not (Test-Path "apps\dashboard\.next")) { Say "Building dashboard (first time)..."; powershell -ExecutionPolicy Bypass -File scripts\build-dashboard.ps1 }
  Say "Starting dashboard in a new window..."
  Start-Process powershell -ArgumentList "-NoExit","-Command","Set-Location '$root\apps\dashboard'; npm.cmd start"
  for ($i=0; $i -lt 30; $i++) { Start-Sleep 2; if (DashUp) { break } }
}

Say "API ready: $(ApiUp) | Dashboard listening: $(DashUp)"
Say "Open http://localhost:3000"
Start-Process "http://localhost:3000"
