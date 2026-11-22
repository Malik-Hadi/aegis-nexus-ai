# Live view of the real-time stream. Needs the CURRENT lab password (run "go run ./cmd/seed" if unknown; this resets all 4).
# Leave this window open, then send events from another terminal and watch the messages appear instantly.
param([string]$Base = "http://localhost:8080", [string]$User = "analyst-a", [string]$Tenant = "11111111-1111-1111-1111-111111111111")
$pw = Read-Host "Password for $User"
$s = New-Object Microsoft.PowerShell.Commands.WebRequestSession
$body = @{ tenant_id = $Tenant; subject = $User; password = $pw.Trim() } | ConvertTo-Json
try {
  Invoke-RestMethod -Uri "$Base/v1/auth/login" -Method Post -Body $body -ContentType "application/json" -WebSession $s | Out-Null
} catch { Write-Host "Login failed: $($_.Exception.Message)" -ForegroundColor Red; exit 1 }
$c = $s.Cookies.GetCookies([uri]$Base)["aegis_session"].Value
Write-Host "Connected. Waiting for live messages (Ctrl+C to stop)..." -ForegroundColor Green
curl.exe -sN -H "Cookie: aegis_session=$c" "$Base/v1/stream"
