# Compiles the three Linux programs inside a Go container into deploy\bin. Needs only Docker.
Set-Location (Split-Path $PSScriptRoot -Parent)
New-Item -ItemType Directory -Force deploy\bin | Out-Null
docker run --rm -v "${PWD}:/src" -w /src -e CGO_ENABLED=0 -e GOOS=linux golang:1.26-alpine sh -c "go build -o deploy/bin/api ./services/api && go build -o deploy/bin/seed ./cmd/seed && go build -o deploy/bin/agent ./agents/endpoint && ls -l deploy/bin"
if ($LASTEXITCODE -ne 0) { Write-Host "Binary build failed" -ForegroundColor Red; exit 1 }