# Runs EVERY check for the whole project so far. Run it after each block.
# Prerequisites: Docker running, DB healthy, Go API and production Next.js dashboard running.
# NOTE: resets the four lab-user passwords (each script reseeds).
param([switch]$SkipRestart)
$names = @("verify-backend.ps1", "verify-features.ps1")
$total = 0
foreach ($n in $names) {
  Write-Host "`n==================== $n ====================" -ForegroundColor Cyan
  $a = @("-ExecutionPolicy", "Bypass", "-File", "scripts\$n")
  if ($SkipRestart -and $n -eq "verify-backend.ps1") { $a += "-SkipRestart" }
  & powershell @a
  $code = $LASTEXITCODE
  $total += $code
  Write-Host ">> $n finished with $code failed check(s)" -ForegroundColor $(if ($code -eq 0) { "Green" } else { "Red" })
}
Write-Host ""
if ($total -eq 0) { Write-Host "ALL CHECKS PASSED. Safe to move to the next block." -ForegroundColor Green }
else { Write-Host "$total check(s) failed. Fix them before the next block (send me the FAIL lines)." -ForegroundColor Red }
exit $total
