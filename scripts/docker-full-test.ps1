# AT-01 check: one Docker command -> healthy stack. Run from the repo root.
# WARNING: without -KeepData this runs "down -v", which deletes the lab database volume.
param([switch]$KeepData)
$ErrorActionPreference = "Continue"
$c = @("-f","deploy/docker-compose.yml","--env-file",".env","--profile","full")
$script:fail = 0
function Check($name, $ok, $detail) { if ($ok) { Write-Host "PASS  $name" -ForegroundColor Green } else { Write-Host "FAIL  $name  $detail" -ForegroundColor Red; $script:fail++ } }
if (-not (Test-Path .env)) { Write-Host "Create .env from .env.example first." -ForegroundColor Red; exit 1 }
if (-not $KeepData) { docker compose @c down -v 2>&1 | Out-Null }
& powershell -ExecutionPolicy Bypass -File scripts\build-binaries.ps1
docker compose @c up -d --build --wait 2>&1 | Select-Object -Last 15
Check "compose up --wait succeeded" ($LASTEXITCODE -eq 0) "see: docker compose logs"
$r = (docker compose @c exec -T api wget -qO- http://127.0.0.1:8080/readyz 2>&1 | Out-String)
Check "API /readyz ready, database up" ($r -match '"status":"ready"' -and $r -match '"database":"up"') $r
try { $w = Invoke-WebRequest -UseBasicParsing http://localhost:3000 -TimeoutSec 10; Check "SOC console answers on :3000" ($w.StatusCode -eq 200) "" } catch { Check "SOC console answers on :3000" $false $_.Exception.Message }
$seed = (docker compose @c exec -T api /app/seed 2>&1 | Out-String)
Check "seed created 4 lab users" (([regex]::Matches($seed,"user=")).Count -eq 4) $seed
Write-Host "`n$seed"
docker compose @c restart api 2>&1 | Out-Null
Start-Sleep 8
$r2 = (docker compose @c exec -T api wget -qO- http://127.0.0.1:8080/readyz 2>&1 | Out-String)
Check "API healthy again after restart" ($r2 -match '"status":"ready"') $r2
Write-Host "`nRESULT: $script:fail failed" -ForegroundColor $(if ($script:fail -eq 0) {"Green"} else {"Red"})
exit $script:fail
