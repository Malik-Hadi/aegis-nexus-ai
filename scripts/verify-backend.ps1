# AEGIS NEXUS AI - full backend verification (Blocks 1 and 2).
# Run from the project root, with Docker running, the DB healthy and the API running (task "API: run").
# NOTE: this RESETS the four lab-user passwords (it runs the seed command to get known passwords).
param([string]$Base = "http://localhost:8080", [switch]$SkipRestart)
$ErrorActionPreference = "Continue"
$script:pass = 0; $script:fail = 0
$script:failed = @()

function Chk($name, $ok, $hint = "") {
  if ([bool]$ok) { $script:pass++; Write-Host "PASS  $name" -ForegroundColor Green }
  else { $script:fail++; $script:failed += $name; Write-Host "FAIL  $name  -> $hint" -ForegroundColor Red }
}
function Section($t) { Write-Host "`n--- $t ---" -ForegroundColor Cyan }

if (-not (Test-Path go.mod) -or -not (Test-Path .env)) { Write-Host "Run this from the project root (go.mod and .env must be here)." -ForegroundColor Red; exit 2 }
$envs = @{}
Get-Content .env | ForEach-Object { if ($_ -match '^\s*([^#=]+)=(.*)$') { $envs[$matches[1].Trim()] = $matches[2].Trim() } }
$dbu = $envs['POSTGRES_USER']; $dbn = $envs['POSTGRES_DB']
$compose = @("compose", "-f", "deploy/docker-compose.yml", "--env-file", ".env")

function Sql($q) {
  $out = & docker @compose exec -T db psql -U $dbu -d $dbn -t -A -c $q 2>&1 | Out-String
  return $out.Trim()
}

function Call($method, $path, $body = $null, $session = $null, $headers = @{}) {
  $p = @{ Uri = "$Base$path"; Method = $method; UseBasicParsing = $true; Headers = $headers; ErrorAction = "Stop" }
  if ($null -ne $body) { $p.Body = ($body | ConvertTo-Json -Depth 6); $p.ContentType = "application/json" }
  if ($null -ne $session) { $p.WebSession = $session }
  try {
    $r = Invoke-WebRequest @p
    return @{ Code = [int]$r.StatusCode; Body = [string]$r.Content }
  } catch {
    $resp = $_.Exception.Response
    if ($null -ne $resp) {
      $b = ""
      if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $b = $_.ErrorDetails.Message }
      elseif ($resp -is [System.Net.HttpWebResponse]) { $sr = New-Object System.IO.StreamReader($resp.GetResponseStream()); $b = $sr.ReadToEnd() }
      return @{ Code = [int]$resp.StatusCode; Body = $b }
    }
    return @{ Code = 0; Body = $_.Exception.Message }
  }
}

function Login($tenant, $user, $password) {
  $s = New-Object Microsoft.PowerShell.Commands.WebRequestSession
  $r = Call "POST" "/v1/auth/login" @{ tenant_id = $tenant; subject = $user; password = $password } $s
  return @{ Session = $s; Code = $r.Code; Body = $r.Body }
}

function Ev($key, $kind = "auth.failed_login", $time = $null, $payload = $null) {
  if (-not $time) { $time = [DateTime]::UtcNow.ToString("o") }
  if (-not $payload) { $payload = @{ user = "lab-user-01"; source_ip = "192.0.2.10" } }
  return @{ kind = $kind; occurred_at = $time; severity = "low"; payload = $payload; idempotency_key = $key }
}

$TA = "11111111-1111-1111-1111-111111111111"
$TB = "22222222-2222-2222-2222-222222222222"
$run = [guid]::NewGuid().ToString("N").Substring(0, 8)

# ------------------------------------------------------------------
Section "1. CODE QUALITY"
& go build ./... 2>&1 | Out-Null
Chk "go build" ($LASTEXITCODE -eq 0) "run: go build ./...  and read the error"
& go vet ./... 2>&1 | Out-Null
Chk "go vet (static checks)" ($LASTEXITCODE -eq 0) "run: go vet ./...  and read the warning"
$tv = & go test ./services/api/ -count=1 -v 2>&1 | Out-String
Chk "go test passes" ($tv -match '(?m)^ok\s') "run: go test ./services/api/ -v"
$np = ([regex]::Matches($tv, '--- PASS')).Count
Chk "at least 51 tests passed ($np found)" ($np -ge 51) "some tests are missing"
Chk "no tests were skipped (DB tests really ran)" ($tv -notmatch '--- SKIP') "start the database and rerun"
Chk "no test failed" ($tv -notmatch '--- FAIL') "run go test ./services/api/ -v"

# ------------------------------------------------------------------
Section "2. DATABASE"
$h = (& docker @compose ps 2>&1 | Out-String)
Chk "db container healthy" ($h -match 'healthy') "run task DB: start and wait 15 s"
Chk "11 tables exist (10 core + enrolment_tokens)" ((Sql "SELECT count(*) FROM information_schema.tables WHERE table_schema='public'") -eq "11") "apply db/migrations/003_enrolment.sql (Block 2, Step 3)"
Chk "2 lab tenants seeded" ((Sql "SELECT count(*) FROM tenants") -eq "2") "run task DB: reset then DB: start"
Chk "events table has idempotency uniqueness" ((Sql "SELECT count(*) FROM pg_indexes WHERE tablename='events' AND indexdef ILIKE '%idempotency_key%'") -ge "1") "check migration 001"

# ------------------------------------------------------------------
Section "3. API IS UP"
$r = Call "GET" "/healthz"
Chk "GET /healthz = 200" ($r.Code -eq 200) "start the task API: run"
if ($r.Code -ne 200) { Write-Host "`nAPI is not running. Start it (task API: run) and rerun this script." -ForegroundColor Red; exit 2 }
$r = Call "GET" "/readyz"
Chk "GET /readyz = 200 and database up" (($r.Code -eq 200) -and ($r.Body -match 'up')) "database not reachable by the API"

# ------------------------------------------------------------------
Section "4. LAB USERS (resets the 4 lab passwords)"
$seed = & go run ./cmd/seed 2>&1 | Out-String
$pw = @{}
foreach ($m in [regex]::Matches($seed, 'user=(\S+)\s+role=\S+\s+password=(\S+)')) { $pw[$m.Groups[1].Value] = $m.Groups[2].Value }
Chk "seed created 4 users" ($pw.Count -eq 4) "run: go run ./cmd/seed  and read the error"
if ($pw.Count -ne 4) { Write-Host "Cannot continue without lab users." -ForegroundColor Red; exit 2 }
Chk "passwords stored as bcrypt hashes" ((Sql 'SELECT count(*) FROM users WHERE password_hash IS NOT NULL AND password_hash NOT LIKE ''$2%''') -eq "0") "plain-text passwords found!"

# ------------------------------------------------------------------
Section "5. AUTHENTICATION AND ROLES (AT-02)"
Chk "anonymous GET /v1/me = 401" ((Call "GET" "/v1/me").Code -eq 401)
Chk "anonymous GET /v1/events = 401" ((Call "GET" "/v1/events").Code -eq 401)
Chk "anonymous GET /v1/agents = 401" ((Call "GET" "/v1/agents").Code -eq 401)
Chk "anonymous admin route = 401" ((Call "GET" "/v1/admin/ping").Code -eq 401)
Chk "wrong password = 401" ((Login $TA "analyst-a" "definitely-wrong").Code -eq 401)
Chk "unknown user = 401" ((Login $TA "nobody" "x").Code -eq 401)
Chk "unknown tenant = 401" ((Login "00000000-0000-0000-0000-000000000000" "analyst-a" "x").Code -eq 401)
Chk "bad tenant id format = 401" ((Login "not-a-uuid" "analyst-a" "x").Code -eq 401)
Chk "GET on login route is refused" ((Call "GET" "/v1/auth/login").Code -eq 405)

$anA = Login $TA "analyst-a" $pw["analyst-a"]
$adA = Login $TA "admin-a" $pw["admin-a"]
$adB = Login $TB "admin-b" $pw["admin-b"]
$anB = Login $TB "analyst-b" $pw["analyst-b"]
Chk "analyst-a login = 200" ($anA.Code -eq 200)
Chk "admin-a login = 200" ($adA.Code -eq 200)
Chk "admin-b login = 200" ($adB.Code -eq 200)
Chk "analyst-b login = 200" ($anB.Code -eq 200)
$me = Call "GET" "/v1/me" $null $anA.Session
Chk "/v1/me shows analyst role and tenant A" (($me.Code -eq 200) -and ($me.Body -match 'analyst') -and ($me.Body -match $TA))
Chk "analyst on admin route = 403" ((Call "GET" "/v1/admin/ping" $null $anA.Session).Code -eq 403)
Chk "admin on admin route = 200" ((Call "GET" "/v1/admin/ping" $null $adA.Session).Code -eq 200)
Chk "analyst cannot create enrolment token = 403" ((Call "POST" "/v1/admin/enrolment-tokens" $null $anA.Session).Code -eq 403)

$cookie = $adA.Session.Cookies.GetCookies([uri]$Base)["aegis_session"]
Chk "session cookie is set" ($null -ne $cookie)
if ($null -ne $cookie) {
  Chk "cookie is HttpOnly" ($cookie.HttpOnly -eq $true) "set HttpOnly on the cookie"
  $bad = $cookie.Value.Substring(0, $cookie.Value.Length - 2) + "xx"
  Chk "tampered cookie = 401" ((Call "GET" "/v1/me" $null $null @{ Cookie = "aegis_session=$bad" }).Code -eq 401)
}
$lo = Call "POST" "/v1/auth/logout" $null $anA.Session

# ------------------------------------------------------------------
Section "6. AGENT ENROLMENT, INGESTION, VALIDATION (Block 2)"
$t1 = Call "POST" "/v1/admin/enrolment-tokens" $null $adA.Session
Chk "admin creates enrolment token = 201" ($t1.Code -eq 201)
$token = ($t1.Body | ConvertFrom-Json).enrolment_token
Chk "token is 64 characters" (($token) -and ($token.Length -eq 64))
Chk "bad hostname rejected = 400" ((Call "POST" "/v1/agents/enrol" @{ token = $token; hostname = "bad host;drop"; os = "linux" }).Code -eq 400)
Chk "bad os rejected = 400" ((Call "POST" "/v1/agents/enrol" @{ token = $token; hostname = "verify-$run"; os = "macos" }).Code -eq 400)
Chk "fake token = 401" ((Call "POST" "/v1/agents/enrol" @{ token = ("a" * 64); hostname = "verify-$run"; os = "linux" }).Code -eq 401)
$en = Call "POST" "/v1/agents/enrol" @{ token = $token; hostname = "verify-$run"; os = "linux" }
Chk "enrol with real token = 201" ($en.Code -eq 201)
$agent = $en.Body | ConvertFrom-Json
$cred = $agent.credential; $agentId = $agent.agent_id
Chk "same token cannot be used twice = 401" ((Call "POST" "/v1/agents/enrol" @{ token = $token; hostname = "verify-again-$run"; os = "linux" }).Code -eq 401)
$secretPart = if ($cred) { $cred.Split(".")[1] } else { "none" }
Chk "agent secret is NOT stored in the database" ((Sql "SELECT count(*) FROM agents WHERE credential_hash = '$secretPart'") -eq "0") "credential stored in plain text!"

$hdr = @{ Authorization = "Bearer $cred" }
$k1 = "v-$run-1"
$e1 = Call "POST" "/v1/events" (Ev $k1) $null $hdr
Chk "valid event = 202" ($e1.Code -eq 202)
Chk "response says accepted" ($e1.Body -match 'accepted')
$e2 = Call "POST" "/v1/events" (Ev $k1) $null $hdr
Chk "same idempotency key = 202 duplicate" (($e2.Code -eq 202) -and ($e2.Body -match 'duplicate'))
Chk "exactly 1 stored for that key" ((Sql "SELECT count(*) FROM events WHERE idempotency_key = '$k1'") -eq "1")
Chk "invalid kind = 400" ((Call "POST" "/v1/events" (Ev "v-$run-2" "BAD KIND;--") $null $hdr).Code -eq 400)
Chk "SQL-injection style kind = 400" ((Call "POST" "/v1/events" (Ev "v-$run-3" "x'; DROP TABLE events;--") $null $hdr).Code -eq 400)
Chk "future timestamp = 400" ((Call "POST" "/v1/events" (Ev "v-$run-4" "auth.failed_login" ([DateTime]::UtcNow.AddHours(2).ToString("o"))) $null $hdr).Code -eq 400)
Chk "very old timestamp = 400" ((Call "POST" "/v1/events" (Ev "v-$run-5" "auth.failed_login" "2020-01-01T00:00:00Z") $null $hdr).Code -eq 400)
Chk "payload over 16 KB = 400" ((Call "POST" "/v1/events" (Ev "v-$run-6" "auth.failed_login" $null @{ blob = ("x" * 20000) }) $null $hdr).Code -eq 400)
Chk "body over 64 KB = 413" ((Call "POST" "/v1/events" (Ev "v-$run-7" "auth.failed_login" $null @{ blob = ("x" * 70000) }) $null $hdr).Code -eq 413)
Chk "SQL text inside payload is stored harmlessly" ((Call "POST" "/v1/events" (Ev "v-$run-8" "auth.failed_login" $null @{ user = "x'; DROP TABLE events;--" }) $null $hdr).Code -eq 202)
Chk "events table still exists after injection attempt" ((Sql "SELECT count(*) FROM events") -ge "1")
Chk "event with no credential = 401" ((Call "POST" "/v1/events" (Ev "v-$run-9")).Code -eq 401)
$tamper = @{ Authorization = "Bearer " + $agentId + "." + ("0" * 64) }
Chk "wrong agent secret = 401" ((Call "POST" "/v1/events" (Ev "v-$run-10") $null $tamper).Code -eq 401)
Chk "browser cookie cannot post events = 401" ((Call "POST" "/v1/events" (Ev "v-$run-11") $adA.Session).Code -eq 401)
Chk "heartbeat = 200" ((Call "POST" "/v1/agents/heartbeat" $null $null $hdr).Code -eq 200)
Chk "last_seen_at was updated" ((Sql "SELECT count(*) FROM agents WHERE id = '$agentId' AND last_seen_at IS NOT NULL") -eq "1")
$anA = Login $TA "analyst-a" $pw["analyst-a"]

# ------------------------------------------------------------------
Section "7. READING DATA AND PAGINATION"
$ev = Call "GET" "/v1/events?limit=200" $null $anA.Session
Chk "analyst-a reads events = 200 and sees the new event" (($ev.Code -eq 200) -and ($ev.Body -match $agentId))
Chk "limit=1 returns exactly 1 event" (@((Call "GET" "/v1/events?limit=1" $null $anA.Session).Body | ConvertFrom-Json | Select-Object -ExpandProperty events).Count -eq 1)
Chk "limit=0 = 400" ((Call "GET" "/v1/events?limit=0" $null $anA.Session).Code -eq 400)
Chk "limit=500 = 400" ((Call "GET" "/v1/events?limit=500" $null $anA.Session).Code -eq 400)
Chk "offset=-1 = 400" ((Call "GET" "/v1/events?offset=-1" $null $anA.Session).Code -eq 400)
Chk "kind filter works" ((Call "GET" "/v1/events?kind=auth.failed_login&limit=5" $null $anA.Session).Code -eq 200)
$ag = Call "GET" "/v1/agents" $null $anA.Session
Chk "analyst-a lists agents and sees the new agent" (($ag.Code -eq 200) -and ($ag.Body -match $agentId))
Chk "agent list never exposes credential hashes" ($ag.Body -notmatch 'credential')

# ------------------------------------------------------------------
Section "8. TENANT ISOLATION (AT-03)"
$evB = Call "GET" "/v1/events?limit=200" $null $adB.Session
Chk "tenant B reads events = 200" ($evB.Code -eq 200)
Chk "tenant B does NOT see tenant A events" ($evB.Body -notmatch $agentId)
$agB = Call "GET" "/v1/agents" $null $adB.Session
Chk "tenant B does NOT see tenant A agents" (($agB.Code -eq 200) -and ($agB.Body -notmatch $agentId))
Chk "tenant B admin cannot revoke tenant A agent = 404" ((Call "POST" "/v1/agents/$agentId/revoke" $null $adB.Session).Code -eq 404)
$still = (Call "GET" "/v1/agents" $null $anA.Session).Body | ConvertFrom-Json
$mine = @($still.agents | Where-Object { $_.id -eq $agentId })[0]
Chk "tenant A agent is still active after the attempt" ($mine.status -eq "active")
Chk "revoke of unknown id = 404" ((Call "POST" "/v1/agents/00000000-0000-0000-0000-000000000000/revoke" $null $adA.Session).Code -eq 404)
Chk "revoke of malformed id = 404" ((Call "POST" "/v1/agents/not-a-uuid/revoke" $null $adA.Session).Code -eq 404)
$tokB = Call "POST" "/v1/admin/enrolment-tokens" $null $adB.Session
$agB2 = Call "POST" "/v1/agents/enrol" @{ token = ($tokB.Body | ConvertFrom-Json).enrolment_token; hostname = "verify-b-$run"; os = "windows" }
Chk "tenant B can enrol its own agent" ($agB2.Code -eq 201)
$credB = ($agB2.Body | ConvertFrom-Json).credential
$ebody = Call "POST" "/v1/events" (Ev "v-$run-b1") $null @{ Authorization = "Bearer $credB" }
Chk "tenant B agent event accepted" ($ebody.Code -eq 202)
Chk "tenant A analyst does NOT see tenant B agent events" ((Call "GET" "/v1/events?limit=200" $null $anA.Session).Body -notmatch ($agB2.Body | ConvertFrom-Json).agent_id)
Chk "event stored under tenant B in the database" ((Sql "SELECT count(*) FROM events WHERE idempotency_key = 'v-$run-b1' AND tenant_id = '$TB'") -eq "1")

# ------------------------------------------------------------------
Section "9. REVOCATION (AT-06)"
Chk "analyst cannot revoke = 403" ((Call "POST" "/v1/agents/$agentId/revoke" $null $anA.Session).Code -eq 403)
Chk "agent still works before admin revokes" ((Call "POST" "/v1/events" (Ev "v-$run-12") $null $hdr).Code -eq 202)
Chk "admin revokes own tenant agent = 200" ((Call "POST" "/v1/agents/$agentId/revoke" $null $adA.Session).Code -eq 200)
Chk "revoked agent event = 401" ((Call "POST" "/v1/events" (Ev "v-$run-13") $null $hdr).Code -eq 401)
Chk "revoked agent heartbeat = 401" ((Call "POST" "/v1/agents/heartbeat" $null $null $hdr).Code -eq 401)
$after = (Call "GET" "/v1/agents" $null $anA.Session).Body | ConvertFrom-Json
Chk "agent list shows status revoked" ((@($after.agents | Where-Object { $_.id -eq $agentId })[0]).status -eq "revoked")

# ------------------------------------------------------------------
Section "10. AUDIT LOG"
foreach ($a in @("auth.login", "authz.denied", "agent.enrol", "agent.revoke", "agent.rejected", "enrolment.token_created")) {
  Chk "audit has '$a' entries" ([int](Sql "SELECT count(*) FROM audit_logs WHERE action = '$a'") -ge 1) "action not being audited"
}
Chk "failed logins are audited" ([int](Sql "SELECT count(*) FROM audit_logs WHERE action='auth.login' AND result='failure'") -ge 1)
$upd = Sql "UPDATE audit_logs SET result = result WHERE id = (SELECT min(id) FROM audit_logs)"
Chk "audit log cannot be edited (append-only)" ($upd -match 'append-only') "trigger missing from migration 001"
$del = Sql "DELETE FROM audit_logs WHERE id = (SELECT min(id) FROM audit_logs)"
Chk "audit log cannot be deleted" ($del -match 'append-only') "trigger missing from migration 001"

# ------------------------------------------------------------------
if (-not $SkipRestart) {
  Section "11. PERSISTENCE AFTER RESTART (AT-05)"
  $before = [int](Sql "SELECT count(*) FROM events")
  Write-Host "Restarting the database container (about 15 seconds)..."
  & docker @compose restart db 2>&1 | Out-Null
  $up = $false
  for ($i = 0; $i -lt 30; $i++) { Start-Sleep -Seconds 2; $rz = Call "GET" "/readyz"; if ($rz.Code -eq 200) { $up = $true; break } }
  Chk "API reports ready again after DB restart" $up "database or API did not recover in 60 s"
  $afterN = [int](Sql "SELECT count(*) FROM events")
  Chk "all $before events survived the restart" ($afterN -eq $before) "data was lost"
  $anA = Login $TA "analyst-a" $pw["analyst-a"]
  Chk "events still readable through the API" ((Call "GET" "/v1/events?limit=5" $null $anA.Session).Code -eq 200)
}

# ------------------------------------------------------------------
Section "CLEANUP"
Sql "DELETE FROM events WHERE agent_id IN (SELECT id FROM agents WHERE hostname LIKE 'verify-%')" | Out-Null
Sql "DELETE FROM agents WHERE hostname LIKE 'verify-%'" | Out-Null
Write-Host "Removed the verify-* test agents and their events (audit log rows stay, by design)."

Write-Host ""
$color = if ($script:fail -eq 0) { "Green" } else { "Red" }
Write-Host "RESULT: $($script:pass) passed, $($script:fail) failed" -ForegroundColor $color
if ($script:fail -gt 0) { Write-Host "Failed checks:"; $script:failed | ForEach-Object { Write-Host " - $_" -ForegroundColor Red } }
exit $script:fail
