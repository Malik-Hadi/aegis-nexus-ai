# Prints a one-time enrolment token (valid 1 hour). The API must be running.
# Needs the CURRENT admin-a password: run "go run ./cmd/seed" first if you do not know it (this resets all 4 lab passwords).
param([string]$Base = "http://localhost:8080", [string]$User = "admin-a", [string]$Tenant = "11111111-1111-1111-1111-111111111111")
$pw = Read-Host "Password for $User"
$s = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$body = @{ tenant_id = $Tenant; subject = $User; password = $pw.Trim() } | ConvertTo-Json
try {
  Invoke-RestMethod -Uri "$Base/v1/auth/login" -Method Post -Body $body -ContentType "application/json" -WebSession $s | Out-Null
  $t = Invoke-RestMethod -Uri "$Base/v1/admin/enrolment-tokens" -Method Post -WebSession $s
  Write-Host "Enrolment token (valid 1 hour, one use):" -ForegroundColor Green
  Write-Host $t.enrolment_token
} catch {
  Write-Host "Failed: $($_.Exception.Message)" -ForegroundColor Red
  Write-Host "Check: API running? Password current (run go run ./cmd/seed)?" -ForegroundColor Yellow
}
