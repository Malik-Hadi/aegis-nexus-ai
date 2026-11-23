# Starts everything for a local demo: database, API (8080) and console (3000).
$ErrorActionPreference = "Continue"
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root
function Fail($m) { Write-Host "`nSTOP: $m" -ForegroundColor Red; exit 1 }
if (-not (Test-Path .env)) { Fail "Missing .env. Copy .env.example to .env and set the passwords first." }

# 1. Docker Desktop
docker info *> $null
if ($LASTEXITCODE -ne 0) {
  Write-Host "Starting Docker Desktop (this can take a minute)..."
  $dd = "C:\Program Files\Docker\Docker\Docker Desktop.exe"
  if (Test-Path $dd) { Start-Process $dd }
  for ($i = 0; $i -lt 60; $i++) { Start-Sleep 3; docker info *> $null; if ($LASTEXITCODE -eq 0) { break } }
}
docker info *> $null
if ($LASTEXITCODE -ne 0) { Fail "Docker is not running. Open Docker Desktop, wait for 'Engine running', then run this again." }

# 2. Free the ports and stop any Docker copy of the API/console
docker compose -f deploy/docker-compose.yml --env-file .env --profile full stop api dashboard *> $null
Get-Process aegis-api* -ErrorAction SilentlyContinue | Stop-Process -Force
Get-NetTCPConnection -LocalPort 3000,8080 -State Listen -ErrorAction SilentlyContinue | ForEach-Object { Stop-Process -Id $_.OwningProcess -Force -ErrorAction SilentlyContinue }
Start-Sleep 2

# 3. Database
Write-Host "Starting the database..."
docker compose -f deploy/docker-compose.yml --env-file .env up -d --wait db
if ($LASTEXITCODE -ne 0) { Fail "The database did not start. Check Docker Desktop and POSTGRES_PORT in .env." }

# 4. Build the API; build the console only if needed
$bin = Join-Path $root ".local-verification\startup\aegis-api.exe"
New-Item -ItemType Directory -Force (Split-Path $bin) | Out-Null
Write-Host "Building the API..."
go build -buildvcs=false -o $bin ./services/api
if ($LASTEXITCODE -ne 0) { Fail "The API did not build. Paste the error above to Claude." }
if (-not (Test-Path apps\dashboard\node_modules)) { Push-Location apps\dashboard; npm.cmd ci --no-audit --no-fund; Pop-Location }
if (-not (Test-Path apps\dashboard\.next\BUILD_ID)) { Write-Host "Building the console..."; Push-Location apps\dashboard; npm.cmd run build; $b = $LASTEXITCODE; Pop-Location; if ($b -ne 0) { Fail "The console did not build. Paste the error above to Claude." } }

# 4b. Make sure the lab database has the analysis table (safe to repeat)
docker compose -f deploy/docker-compose.yml --env-file .env exec -T db sh -c 'psql -X -q -U "$POSTGRES_USER" -d "$POSTGRES_DB" -f /docker-entrypoint-initdb.d/006_analysis_jobs.sql' *> $null

# 5. Start the API and the console, each in its own window
Start-Process powershell -ArgumentList '-NoExit','-Command',"`$Host.UI.RawUI.WindowTitle='AEGIS API'; Set-Location '$root'; & '$bin'"
$ok = $false
for ($i = 0; $i -lt 45; $i++) { Start-Sleep 2; if ((curl.exe -s http://localhost:8080/readyz) -match '"status":"ready"') { $ok = $true; break } }
if (-not $ok) { Fail "The API did not become ready. Look at the 'AEGIS API' window for the error." }
Write-Host "API ready."
Start-Process powershell -ArgumentList '-NoExit','-Command',"`$Host.UI.RawUI.WindowTitle='AEGIS Console'; Set-Location '$root\apps\dashboard'; node node_modules\next\dist\bin\next start --hostname localhost"
$ok = $false
for ($i = 0; $i -lt 45; $i++) { Start-Sleep 2; if ((curl.exe -s -o NUL -w "%{http_code}" http://localhost:3000) -eq "200") { $ok = $true; break } }
if (-not $ok) { Fail "The console did not start. Look at the 'AEGIS Console' window for the error." }

# 6. Fresh lab logins
$seed = (go run ./cmd/seed 2>&1 | Out-String)
Write-Host "`n==== LOGINS (new passwords, save them privately) ===="
$seed -split "`n" | Select-String "user=" | ForEach-Object { $_.Line.Trim() }

Write-Host "`nEVERYTHING IS RUNNING. Open http://localhost:3000 and sign in as admin-a." -ForegroundColor Green
Write-Host "Leave the 'AEGIS API' and 'AEGIS Console' windows open. To stop: scripts\stop-everything.ps1"
Start-Process "http://localhost:3000"