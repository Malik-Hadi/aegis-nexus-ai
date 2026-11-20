# Windows: run in PowerShell from the repo root
foreach ($c in @("go version","node -v","npm -v","git --version","docker --version","docker compose version")) {
  Write-Host "> $c"; try { Invoke-Expression $c } catch { Write-Host "MISSING: $c" -ForegroundColor Red }
}
