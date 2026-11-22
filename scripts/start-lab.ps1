# Start PostgreSQL, the Go API and the production dashboard on a local lab.
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$dashboard = Join-Path $root 'apps/dashboard'
$logs = Join-Path $root '.local-verification/startup'
$api = $null
$web = $null

function Wait-Healthy($url, $child, $log) {
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        if ($child.HasExited) { throw "Service exited. See $log" }
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 2
            if ($response.StatusCode -eq 200) { return }
        } catch {
            Start-Sleep -Seconds 1
        }
    }
    throw "Service readiness timed out. See $log"
}

Push-Location $root
try {
    if (-not (Test-Path -LiteralPath '.env')) { throw 'Configure .env from .env.example before starting the lab.' }
    docker compose -f deploy/docker-compose.yml --env-file .env up -d --wait
    if ($LASTEXITCODE -ne 0) { throw 'Docker Compose failed to start PostgreSQL. Check Docker Desktop and the configured host port.' }

    # The image runs migrations on first initialization. Apply additive upgrades
    # explicitly so a previously initialized lab volume also has analysis jobs.
    $dbSettings = @(docker compose -f deploy/docker-compose.yml --env-file .env exec -T db printenv POSTGRES_USER POSTGRES_DB)
    if ($LASTEXITCODE -ne 0 -or $dbSettings.Count -ne 2) {
        throw 'Could not read the Compose database user and name.'
    }
    $dbUser = $dbSettings[0].Trim()
    $dbName = $dbSettings[1].Trim()
    docker compose -f deploy/docker-compose.yml --env-file .env exec -T db psql -X -v ON_ERROR_STOP=1 -U $dbUser -d $dbName -f /docker-entrypoint-initdb.d/006_analysis_jobs.sql
    if ($LASTEXITCODE -ne 0) { throw 'Analysis migration failed. Confirm migrations 001 through 005 are present in the database.' }
    if (-not (Test-Path -LiteralPath (Join-Path $dashboard '.next/BUILD_ID'))) {
        & powershell -NoProfile -File scripts/build-dashboard.ps1
        if ($LASTEXITCODE -ne 0) { throw 'Dashboard build or tests failed.' }
    }
    New-Item -ItemType Directory -Force -Path $logs | Out-Null
    $apiBin = Join-Path $logs 'aegis-api.exe'
    go build -buildvcs=false -o $apiBin ./services/api
    if ($LASTEXITCODE -ne 0) { throw 'Go API build failed.' }
    $api = Start-Process -FilePath $apiBin -WorkingDirectory $root -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $logs 'api.log')
    Wait-Healthy 'http://localhost:8080/readyz' $api (Join-Path $logs 'api.log')
    $node = (Get-Command node).Source
    $web = Start-Process -FilePath $node -ArgumentList @('node_modules/next/dist/bin/next','start','--hostname','localhost') -WorkingDirectory $dashboard -WindowStyle Hidden -PassThru -RedirectStandardError (Join-Path $logs 'dashboard.log')
    Wait-Healthy 'http://localhost:3000' $web (Join-Path $logs 'dashboard.log')
    Write-Host 'Lab ready at http://localhost:3000. Press Ctrl+C to stop the Go and Next.js processes.'
    while (-not $api.HasExited -and -not $web.HasExited) { Start-Sleep -Seconds 1 }
    throw "A lab service exited. See $logs"
} finally {
    foreach ($child in @($web, $api)) {
        if ($null -ne $child -and -not $child.HasExited) { Stop-Process -Id $child.Id }
    }
    Pop-Location
}