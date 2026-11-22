# Run from the repository root. The dashboard runs as a separate Next.js service.
$ErrorActionPreference = "Stop"
Push-Location (Join-Path $PSScriptRoot '..\apps\dashboard')
try {
    npm.cmd ci --no-audit --no-fund
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    npm.cmd test
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    npm.cmd run build
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    node scripts/test-production.mjs
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    Write-Host 'Build and fixture checks passed. Run npm.cmd start from apps\dashboard.'
} finally {
    Pop-Location
}