# Stops the API and console. Add -All to stop the database too.
param([switch]$All)
Set-Location (Split-Path $PSScriptRoot -Parent)
Get-Process aegis-api* -ErrorAction SilentlyContinue | Stop-Process -Force
Get-NetTCPConnection -LocalPort 3000,8080 -State Listen -ErrorAction SilentlyContinue | ForEach-Object { Stop-Process -Id $_.OwningProcess -Force -ErrorAction SilentlyContinue }
if ($All) { docker compose -f deploy/docker-compose.yml --env-file .env stop }
Write-Host "Stopped."