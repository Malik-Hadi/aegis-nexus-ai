# AEGIS NEXUS AI - verification of Blocks 3 to 6: agent, detection rules, lifecycle/approvals, REAL-TIME stream, dashboard.
# Run from the project root with Docker, the DB and the API (task "API: run") already running.
# NOTE: this RESETS the four lab-user passwords (seed) and creates/removes its own verify-* agents and test incidents.
param([string]$Base = "http://localhost:8080")
$ErrorActionPreference = "Continue"
$script:pass = 0; $script:fail = 0; $script:failed = @()
function Chk($name, $ok, $hint = "") {
  if ([bool]$ok) { $script:pass++; Write-Host "PASS  $name" -ForegroundColor Green }
  else { $script:fail++; $script:failed += $name; Write-Host "FAIL  $name  -> $hint" -ForegroundColor Red }
}
function Section($t) { Write-Host "`n--- $t ---" -ForegroundColor Cyan }
function Skip($t) { Write-Host "SKIP  $t" -ForegroundColor Yellow }

if (-not (Test-Path go.mod) -or -not (Test-Path .env)) { Write-Host "Run this from the project root." -ForegroundColor Red; exit 2 }
$envs = @{}
Get-Content .env | ForEach-Object { if ($_ -match '^\s*([^#=]+)=(.*)$') { $envs[$matches[1].Trim()] = $matches[2].Trim() } }
$dbu = $envs['POSTGRES_USER']; $dbn = $envs['POSTGRES_DB']
$compose = @("compose", "-f", "deploy/docker-compose.yml", "--env-file", ".env")
function Sql($q) { $o = & docker @compose exec -T db psql -U $dbu -d $dbn -t -A -c $q 2>&1 | Out-String; return $o.Trim() }
function SqlInt($q) { $v = Sql $q; $n = 0; if ([int]::TryParse($v, [ref]$n)) { return $n } else { return -1 } }

function Call($method, $path, $body = $null, $session = $null, $headers = @{}) {
  $p = @{ Uri = "$Base$path"; Method = $method; UseBasicParsing = $true; Headers = $headers; ErrorAction = "Stop" }
  if ($null -ne $body) { $p.Body = ($body | ConvertTo-Json -Depth 6); $p.ContentType = "application/json" }
  if ($null -ne $session) { $p.WebSession = $session }
  try { $r = Invoke-WebRequest @p; return @{ Code = [int]$r.StatusCode; Body = [string]$r.Content; Headers = $r.Headers } }
  catch {
    $resp = $_.Exception.Response
    if ($null -ne $resp) {
      $b = ""
      if ($_.ErrorDetails -and $_.ErrorDetails.Message) { $b = $_.ErrorDetails.Message }
      elseif ($resp -is [System.Net.HttpWebResponse]) { $sr = New-Object System.IO.StreamReader($resp.GetResponseStream()); $b = $sr.ReadToEnd() }
      return @{ Code = [int]$resp.StatusCode; Body = $b; Headers = $resp.Headers }
    }
    return @{ Code = 0; Body = $_.Exception.Message; Headers = @{} }
  }
}
function Login($tenant, $user, $password) {
  $s = New-Object Microsoft.PowerShell.Commands.WebRequestSession
  $r = Call "POST" "/v1/auth/login" @{ tenant_id = $tenant; subject = $user; password = $password } $s
  return @{ Session = $s; Code = $r.Code }
}
function CookieOf($sess) { return $sess.Cookies.GetCookies([uri]$Base)["aegis_session"].Value }
function NewAgent($adminSession, $hostName) {
  $t = Call "POST" "/v1/admin/enrolment-tokens" $null $adminSession
  $tok = ($t.Body | ConvertFrom-Json).enrolment_token
  $e = Call "POST" "/v1/agents/enrol" @{ token = $tok; hostname = $hostName; os = "linux" }
  $o = $e.Body | ConvertFrom-Json
  return @{ Id = $o.agent_id; Cred = $o.credential; Code = $e.Code; Token = $tok }
}
function PostEv($cred, $kind, $key, $payload, $sev = "low") {
  $b = @{ kind = $kind; occurred_at = [DateTime]::UtcNow.ToString("o"); severity = $sev; payload = $payload; idempotency_key = $key }
  return (Call "POST" "/v1/events" $b $null @{ Authorization = "Bearer $cred" }).Code
}
function ReadShared($path) {
  try {
    $fs = [IO.File]::Open($path, 'Open', 'Read', 'ReadWrite'); $sr = New-Object IO.StreamReader($fs)
    $t = $sr.ReadToEnd(); $sr.Close(); return $t
  } catch { return "" }
}
function WaitFor($path, $needle, $ms) {
  $sw = [Diagnostics.Stopwatch]::StartNew()
  while ($sw.ElapsedMilliseconds -lt $ms) { if ((ReadShared $path) -match [regex]::Escape($needle)) { return [int]$sw.ElapsedMilliseconds }; Start-Sleep -Milliseconds 25 }
  return -1
}

$TA = "11111111-1111-1111-1111-111111111111"; $TB = "22222222-2222-2222-2222-222222222222"
$run = [guid]::NewGuid().ToString("N").Substring(0, 8)
$tmp = Join-Path $env:TEMP "aegis-verify-$run"; New-Item -ItemType Directory -Path $tmp | Out-Null

# ------------------------------------------------------------------
Section "0. PRECONDITIONS"
Chk "API is up" ((Call "GET" "/healthz").Code -eq 200) "start the task API: run"
if ((Call "GET" "/healthz").Code -ne 200) { exit 2 }
Chk "database healthy" ((& docker @compose ps 2>&1 | Out-String) -match 'healthy') "run task DB: start"
$seed = & go run ./cmd/seed 2>&1 | Out-String
$pw = @{}
foreach ($m in [regex]::Matches($seed, 'user=(\S+)\s+role=\S+\s+password=(\S+)')) { $pw[$m.Groups[1].Value] = $m.Groups[2].Value }
Chk "lab users created" ($pw.Count -eq 4) "run: go run ./cmd/seed"
if ($pw.Count -ne 4) { exit 2 }
$adA = Login $TA "admin-a" $pw["admin-a"]; $anA = Login $TA "analyst-a" $pw["analyst-a"]; $adB = Login $TB "admin-b" $pw["admin-b"]
Chk "admin-a, analyst-a and admin-b can log in" (($adA.Code -eq 200) -and ($anA.Code -eq 200) -and ($adB.Code -eq 200))

# ------------------------------------------------------------------
Section "BLOCK 3: GO AGENT (Windows + Linux)"
& go test ./agents/endpoint/ -count=1 2>&1 | Out-Null
Chk "agent unit tests pass" ($LASTEXITCODE -eq 0) "run: go test ./agents/endpoint/ -v"
$env:GOOS = "linux"; $env:GOARCH = "amd64"; & go build -o "$tmp\agent-linux" ./agents/endpoint 2>&1 | Out-Null; $lc = $LASTEXITCODE
$env:GOOS = "windows"; & go build -o "$tmp\agent.exe" ./agents/endpoint 2>&1 | Out-Null; $wc = $LASTEXITCODE
Remove-Item Env:GOOS -ErrorAction SilentlyContinue; Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
Chk "Linux agent builds" (($lc -eq 0) -and (Test-Path "$tmp\agent-linux") -and ((Get-Item "$tmp\agent-linux").Length -gt 1000000)) "GOOS=linux go build failed"
Chk "Windows agent builds" (($wc -eq 0) -and (Test-Path "$tmp\agent.exe") -and ((Get-Item "$tmp\agent.exe").Length -gt 1000000)) "GOOS=windows go build failed"

$state = "$tmp\state.json"; $queue = "$tmp\queue.jsonl"
$t1 = Call "POST" "/v1/admin/enrolment-tokens" $null $adA.Session
$tok1 = ($t1.Body | ConvertFrom-Json).enrolment_token
$out = & go run ./agents/endpoint enrol -token $tok1 -hostname "verify-agent-$run" -state $state 2>&1 | Out-String
Chk "agent enrols with a one-time token" (($out -match 'Enrolled as agent') -and (Test-Path $state)) "output: $out"
$st = Get-Content $state -Raw | ConvertFrom-Json; $agentId = $st.agent_id
Chk "credential file is not tracked by Git" ((git check-ignore agent-state.json) -eq "agent-state.json") "add agent-state.json to .gitignore"
$out = & go run ./agents/endpoint enrol -token $tok1 -hostname "verify-agent2-$run" -state "$tmp\state2.json" 2>&1 | Out-String
Chk "the same token cannot enrol a second agent" (($out -notmatch 'Enrolled as agent') -and (-not (Test-Path "$tmp\state2.json"))) "token reuse allowed!"
$fx = (Get-Content simulations\det001_positive.json -Raw) -replace 'lab-user-01', "vf-$run-agent"
[IO.File]::WriteAllText("$tmp\fx001.json", $fx, (New-Object Text.UTF8Encoding $false))
$out = & go run ./agents/endpoint run -replay "$tmp\fx001.json" -once -state $state -queue $queue 2>&1 | Out-String
Chk "agent replays the DET-001 fixture (6 sent)" ($out -match '6 sent, 0 queued, 0 dropped') "output: $out"
Chk "agent heartbeat is accepted" ($out -match 'heartbeat ok') "output: $out"
Chk "agent last_seen_at is updated" ((SqlInt "SELECT count(*) FROM agents WHERE id = '$agentId' AND last_seen_at IS NOT NULL") -eq 1)

# offline queue: point a copy of the credential at a dead port, then recover
$dead = Get-Content $state -Raw | ConvertFrom-Json; $dead.api = "http://127.0.0.1:59999"; [IO.File]::WriteAllText("$tmp\dead.json", ($dead | ConvertTo-Json), (New-Object Text.UTF8Encoding $false))
Write-Host "      (offline test: the agent retries with backoff, this takes about 10 seconds)"
$out = & go run ./agents/endpoint run -replay simulations\det002_benign.json -once -state "$tmp\dead.json" -queue "$tmp\q2.jsonl" 2>&1 | Out-String
Chk "server down: event is queued, not lost" (($out -match '0 sent, 1 queued') -and (Test-Path "$tmp\q2.jsonl")) "output: $out"
$out = & go run ./agents/endpoint run -once -state $state -queue "$tmp\q2.jsonl" 2>&1 | Out-String
Chk "server back: queued event is delivered" (($out -match 'sent 1 queued events') -and (-not (Test-Path "$tmp\q2.jsonl"))) "output: $out"

# revocation stops the agent
Chk "admin revokes the agent" ((Call "POST" "/v1/agents/$agentId/revoke" $null $adA.Session).Code -eq 200)
$out = & go run ./agents/endpoint run -once -state $state -queue $queue 2>&1 | Out-String
Chk "revoked agent stops with 'credential rejected'" ($out -match 'credential rejected') "output: $out"

# ------------------------------------------------------------------
Section "BLOCK 4: DETECTION RULES (live, through the real API)"
$ag = NewAgent $adA.Session "verify-rules-$run"
Chk "rules test agent enrolled" ($ag.Code -eq 201)
$cred = $ag.Cred; $rid = $ag.Id
$ua = "vf-$run-a"
1..5 | ForEach-Object { PostEv $cred "auth.failed_login" "$run-a-$_" @{ user = $ua; source_ip = "192.0.2.10" } | Out-Null }
Chk "DET-001: 5 failed logins do NOT create an incident" ((SqlInt "SELECT count(*) FROM incidents WHERE dedup_key = 'DET-001:$ua'") -eq 0)
PostEv $cred "auth.failed_login" "$run-a-6" @{ user = $ua; source_ip = "192.0.2.10" } | Out-Null
Chk "DET-001: the 6th failed login creates ONE high incident" ((SqlInt "SELECT count(*) FROM incidents WHERE dedup_key = 'DET-001:$ua' AND severity = 'high' AND rule_id = 'DET-001'") -eq 1)
Chk "DET-001: incident links all 6 evidence events" ((SqlInt "SELECT count(*) FROM incident_events ie JOIN incidents i ON i.id = ie.incident_id WHERE i.dedup_key = 'DET-001:$ua'") -eq 6)
PostEv $cred "auth.failed_login" "$run-a-7" @{ user = $ua; source_ip = "192.0.2.10" } | Out-Null
Chk "DET-001: a 7th failure joins the same incident (deduplicated)" ((SqlInt "SELECT count(*) FROM incidents WHERE dedup_key = 'DET-001:$ua'") -eq 1)
$ub = "vf-$run-b"
PostEv $cred "privilege.group_added" "$run-b-1" @{ user = $ub; group = "Printer Users"; added_by = "lab-admin" } | Out-Null
Chk "DET-002: normal group does not alert" ((SqlInt "SELECT count(*) FROM incidents WHERE dedup_key LIKE 'DET-002:$ub%'") -eq 0)
PostEv $cred "privilege.group_added" "$run-b-2" @{ user = $ub; group = "Administrators"; added_by = "unknown" } | Out-Null
Chk "DET-002: privileged group creates a high incident" ((SqlInt "SELECT count(*) FROM incidents WHERE dedup_key LIKE 'DET-002:$ub%' AND severity = 'high'") -eq 1)
$ports = 22, 80, 443, 445, 3389, 8080, 5900, 21, 23, 25
$s1 = "sim-$run-1"; $s2 = "sim-$run-2"
1..10 | ForEach-Object { PostEv $cred "net.connection_failed" "$run-c-$_" @{ source_ip = $s2; dest_port = 443 } | Out-Null }
Chk "DET-003: 10 failures to ONE port is not a scan" ((SqlInt "SELECT count(*) FROM incidents WHERE dedup_key = 'DET-003:$s2'") -eq 0)
$i = 0; foreach ($p in $ports) { $i++; PostEv $cred "net.connection_failed" "$run-d-$i" @{ source_ip = $s1; dest_port = $p } | Out-Null }
Chk "DET-003: 10 failures to many ports creates a medium incident" ((SqlInt "SELECT count(*) FROM incidents WHERE dedup_key = 'DET-003:$s1' AND severity = 'medium'") -eq 1)
$ioc = (Get-Content simulations\ioc_hashes.txt | Where-Object { $_ -match '^[0-9a-fA-F]{64}$' } | Select-Object -First 1).ToLower()
$clean = ("b" * 64)
PostEv $cred "process.executable_seen" "$run-e-1" @{ host = "LAB"; path = "C:\lab\calc.exe"; sha256 = $clean } | Out-Null
Chk "DET-004: unknown hash does not alert" ((SqlInt "SELECT count(*) FROM alerts a JOIN events e ON e.id = a.event_id WHERE e.agent_id = '$rid' AND e.payload->>'sha256' = '$clean'") -eq 0)
PostEv $cred "process.executable_seen" "$run-e-2" @{ host = "LAB"; path = "C:\lab\sample.exe"; sha256 = $ioc } | Out-Null
Chk "DET-004: IOC hash alerts (high)" ((SqlInt "SELECT count(*) FROM alerts a JOIN events e ON e.id = a.event_id WHERE e.agent_id = '$rid' AND a.rule_id = 'DET-004' AND a.severity = 'high'") -eq 1)
Chk "tenant B cannot see these incidents" ((Call "GET" "/v1/incidents?limit=200" $null $adB.Session).Body -notmatch $ua)

Write-Host "      (DET-005 test: the background monitor runs every 30 seconds, waiting up to 45 s...)"
[void](Call "POST" "/v1/agents/heartbeat" $null $null @{ Authorization = "Bearer $cred" })
Sql "UPDATE agents SET last_seen_at = now() - interval '10 minutes' WHERE id = '$rid'" | Out-Null
$sw = [Diagnostics.Stopwatch]::StartNew(); $found = $false
while ($sw.Elapsed.TotalSeconds -lt 45) { if ((SqlInt "SELECT count(*) FROM incidents WHERE dedup_key = 'DET-005:$rid' AND severity = 'medium'") -eq 1) { $found = $true; break }; Start-Sleep -Seconds 3 }
Chk "DET-005: silent agent raises a medium incident by itself ($([int]$sw.Elapsed.TotalSeconds) s)" $found "the heartbeat monitor did not fire; check the API terminal"
Start-Sleep -Seconds 33
Chk "DET-005: still ONE incident and ONE alert after another check" (((SqlInt "SELECT count(*) FROM incidents WHERE dedup_key = 'DET-005:$rid'") -eq 1) -and ((SqlInt "SELECT count(*) FROM alerts WHERE agent_id = '$rid' AND rule_id = 'DET-005'") -eq 1))

# ------------------------------------------------------------------
Section "BLOCK 5: LIFECYCLE, APPROVALS, AUDIT (and who may do what)"
$inc = Sql "SELECT id FROM incidents WHERE dedup_key = 'DET-001:$ua' LIMIT 1"
Chk "found the DET-001 incident" ($inc -match '^[0-9a-f-]{36}$') "no incident to test with"
$d = Call "GET" "/v1/incidents/$inc" $null $anA.Session
$dj = $d.Body | ConvertFrom-Json
Chk "detail returns incident, evidence events, alerts, approvals and audit trail" (($d.Code -eq 200) -and (@($dj.events).Count -ge 6) -and ($null -ne $dj.approvals) -and ($null -ne $dj.audit_trail))
Chk "tenant B gets 404 for the incident" ((Call "GET" "/v1/incidents/$inc" $null $adB.Session).Code -eq 404)
Chk "tenant B cannot change it (404)" ((Call "PATCH" "/v1/incidents/$inc" @{ status = "investigating" } $adB.Session).Code -eq 404)
$analysis = Call "POST" "/v1/incidents/$inc/analyse" $null $anA.Session
Chk "analysis accepted as a persisted job" ($analysis.Code -eq 202)
if ($analysis.Code -eq 202) {
  $jobId = ($analysis.Body | ConvertFrom-Json).job_id
  $jobPath = "/v1/incidents/$inc/analysis/$jobId"
  Chk "tenant B cannot read the analysis job" ((Call "GET" $jobPath $null $adB.Session).Code -eq 404)
  $result = $null
  for ($i = 0; $i -lt 40; $i++) {
    $status = Call "GET" $jobPath $null $anA.Session
    if ($status.Code -ne 200) { break }
    $job = $status.Body | ConvertFrom-Json
    if ($job.status -eq 'completed') { $result = $job.result; break }
    if ($job.status -eq 'failed') { break }
    Start-Sleep -Milliseconds 250
  }
  $knownIds = @($dj.events | ForEach-Object { [string]$_.id })
  $unknownIds = @($result.referenced_event_ids | Where-Object { $knownIds -notcontains $_ })
  Chk "analysis completed and cites incident evidence only" (($null -ne $result) -and (@($result.referenced_event_ids).Count -gt 0) -and ($unknownIds.Count -eq 0))
}

Chk "invalid status = 400" ((Call "PATCH" "/v1/incidents/$inc" @{ status = "nonsense" } $anA.Session).Code -eq 400)
Chk "analyst: open -> investigating = 200" ((Call "PATCH" "/v1/incidents/$inc" @{ status = "investigating" } $anA.Session).Code -eq 200)
Chk "same status again = 409" ((Call "PATCH" "/v1/incidents/$inc" @{ status = "investigating" } $anA.Session).Code -eq 409)
Chk "investigating -> closed is not allowed = 409" ((Call "PATCH" "/v1/incidents/$inc" @{ status = "closed" } $anA.Session).Code -eq 409)
Chk "analyst assigns the incident to self = 200" ((Call "PATCH" "/v1/incidents/$inc" @{ owner = "me" } $anA.Session).Code -eq 200)
Chk "owner is stored" ((SqlInt "SELECT count(*) FROM incidents WHERE id = '$inc' AND owner_id IS NOT NULL") -eq 1)

$r1 = Call "POST" "/v1/incidents/$inc/approvals" @{ action = "isolate_host" } $anA.Session
Chk "analyst requests simulated isolation = 201 (pending)" ($r1.Code -eq 201)
$ap1 = ($r1.Body | ConvertFrom-Json).approval_id
Chk "unknown action refused = 400" ((Call "POST" "/v1/incidents/$inc/approvals" @{ action = "rm_rf" } $anA.Session).Code -eq 400)
Chk "second pending request refused = 409" ((Call "POST" "/v1/incidents/$inc/approvals" @{ action = "block_ip" } $anA.Session).Code -eq 409)
Chk "nothing executed while pending (AT-10)" ((SqlInt "SELECT count(*) FROM approvals WHERE id = '$ap1' AND executed_at IS NOT NULL") -eq 0)
Chk "analyst cannot approve = 403" ((Call "POST" "/v1/approvals/$ap1/decision" @{ decision = "approved" } $anA.Session).Code -eq 403)
Chk "other tenant cannot approve = 404" ((Call "POST" "/v1/approvals/$ap1/decision" @{ decision = "approved" } $adB.Session).Code -eq 404)
Chk "admin DENIES = 200" ((Call "POST" "/v1/approvals/$ap1/decision" @{ decision = "denied" } $adA.Session).Code -eq 200)
Chk "denied request changed nothing (AT-10)" (((SqlInt "SELECT count(*) FROM approvals WHERE id = '$ap1' AND executed_at IS NOT NULL") -eq 0) -and ((SqlInt "SELECT count(*) FROM audit_logs WHERE target = '$inc' AND action = 'containment.simulated'") -eq 0) -and ((SqlInt "SELECT count(*) FROM incidents WHERE id = '$inc' AND status = 'investigating'") -eq 1))
Chk "a decided request cannot be decided again = 409" ((Call "POST" "/v1/approvals/$ap1/decision" @{ decision = "approved" } $adA.Session).Code -eq 409)
$r2 = Call "POST" "/v1/incidents/$inc/approvals" @{ action = "isolate_host" } $anA.Session
$ap2 = ($r2.Body | ConvertFrom-Json).approval_id
$dec = Call "POST" "/v1/approvals/$ap2/decision" @{ decision = "approved" } $adA.Session
Chk "admin APPROVES = 200 with a SIMULATION note" (($dec.Code -eq 200) -and ($dec.Body -match 'SIMULATED'))
Chk "approved request records exactly one simulated execution" (((SqlInt "SELECT count(*) FROM approvals WHERE id = '$ap2' AND executed_at IS NOT NULL") -eq 1) -and ((SqlInt "SELECT count(*) FROM audit_logs WHERE target = '$inc' AND action = 'containment.simulated'") -eq 1))
$r3 = Call "POST" "/v1/incidents/$inc/approvals" @{ action = "block_ip" } $adA.Session
$ap3 = ($r3.Body | ConvertFrom-Json).approval_id
Chk "requester cannot approve their OWN request = 403" ((Call "POST" "/v1/approvals/$ap3/decision" @{ decision = "approved" } $adA.Session).Code -eq 403)
Chk "analyst: investigating -> resolved = 200" ((Call "PATCH" "/v1/incidents/$inc" @{ status = "resolved" } $anA.Session).Code -eq 200)
Chk "analyst cannot close = 403" ((Call "PATCH" "/v1/incidents/$inc" @{ status = "closed" } $anA.Session).Code -eq 403)
Chk "admin closes = 200" ((Call "PATCH" "/v1/incidents/$inc" @{ status = "closed" } $adA.Session).Code -eq 200)
Chk "closed incident is final = 409" ((Call "PATCH" "/v1/incidents/$inc" @{ status = "investigating" } $adA.Session).Code -eq 409)
Chk "AT-08: 3 status changes recorded with who and when" ((SqlInt "SELECT count(*) FROM audit_logs WHERE target = '$inc' AND action = 'incident.status' AND result = 'success' AND actor <> '' AND timestamp IS NOT NULL") -eq 3)
Chk "denied attempts are audited" ((SqlInt "SELECT count(*) FROM audit_logs WHERE target = '$inc' AND action IN ('authz.denied','approval.self_decision')") -ge 2)

# ------------------------------------------------------------------
Section "REAL-TIME: live stream (Server-Sent Events)"
$curl = Get-Command curl.exe -ErrorAction SilentlyContinue
if (-not $curl) { Skip "curl.exe not found; live-stream checks skipped (Windows 10/11 normally has it)" }
else {
  $anB = Login $TB "analyst-b" $pw["analyst-b"]
  $fA = "$tmp\streamA.txt"; $fB = "$tmp\streamB.txt"
  $cA = CookieOf $anA.Session; $cB = CookieOf $anB.Session
  $pA = Start-Process curl.exe -ArgumentList "-sN -H `"Cookie: aegis_session=$cA`" $Base/v1/stream" -RedirectStandardOutput $fA -PassThru -WindowStyle Hidden
  $pB = Start-Process curl.exe -ArgumentList "-sN -H `"Cookie: aegis_session=$cB`" $Base/v1/stream" -RedirectStandardOutput $fB -PassThru -WindowStyle Hidden
  Chk "analyst-a opens the live stream" ((WaitFor $fA ": connected" 4000) -ge 0) "stream did not connect"
  Chk "analyst-b (other tenant) opens its own stream" ((WaitFor $fB ": connected" 4000) -ge 0)
  $ms = -1
  [void](PostEv $cred "auth.failed_login" "$run-live-1" @{ user = "vf-$run-live"; source_ip = "192.0.2.77" })
  $ms = WaitFor $fA '"type":"event.new"' 3000
  Chk "a new event reaches the analyst's browser feed live ($ms ms)" (($ms -ge 0) -and ($ms -lt 2000)) "no live message within 2 s"
  $ug = "vf-$run-live2"
  [void](PostEv $cred "privilege.group_added" "$run-live-2" @{ user = $ug; group = "sudo"; added_by = "unknown" })
  $ms2 = WaitFor $fA '"type":"incident.created"' 3000
  Chk "a new incident is pushed live ($ms2 ms)" (($ms2 -ge 0) -and ($ms2 -lt 2000)) "no incident.created within 2 s"
  Start-Sleep -Milliseconds 800
  $textB = ReadShared $fB
  Chk "tenant B stream received NOTHING from tenant A" (($textB -notmatch 'event\.new') -and ($textB -notmatch 'incident\.created') -and ($textB -notmatch 'agent\.')) "cross-tenant leak!"
  $ag2 = $ag.Id
  [void](Call "POST" "/v1/agents/heartbeat" $null $null @{ Authorization = "Bearer $cred" })
  Chk "heartbeats are pushed live" ((WaitFor $fA '"type":"agent.heartbeat"' 2000) -ge 0)
  Stop-Process -Id $pA.Id, $pB.Id -Force -ErrorAction SilentlyContinue
  Chk "a stream without a login is refused (401)" ((Call "GET" "/v1/stream").Code -eq 401)
}

# ------------------------------------------------------------------
Section "BLOCK 6: NEXT.JS DASHBOARD AND WEB SECURITY"
Chk "Next.js dashboard source exists" (Test-Path apps\dashboard\app\dashboard.tsx)
$dashboardOrigin = if ($env:SOC_PUBLIC_ORIGIN) { $env:SOC_PUBLIC_ORIGIN } else { 'http://localhost:3000' }
& node apps/dashboard/scripts/smoke.mjs $dashboardOrigin
Chk "Next.js dashboard serves the UI and protects its API" ($LASTEXITCODE -eq 0) "start the production Next.js server at $dashboardOrigin"
$unsafeSource = Get-ChildItem apps\dashboard\app, apps\dashboard\components, apps\dashboard\lib -Recurse -Include *.ts, *.tsx, *.js, *.jsx, *.mjs | Select-String -Pattern 'dangerouslySetInnerHTML|innerHTML|outerHTML|document\.write|eval\('
Chk "dashboard source displays telemetry without raw HTML injection" (-not $unsafeSource)

$bad = Call "POST" "/v1/auth/login" @{ tenant_id = $TA; subject = "admin-a"; password = $pw["admin-a"] } $null @{ Origin = "http://evil.example" }
Chk "login from a foreign origin is refused (CSRF defence)" (($bad.Code -eq 403) -and ($bad.Body -match 'cross-origin'))
$good = Call "POST" "/v1/auth/login" @{ tenant_id = $TA; subject = "admin-a"; password = $pw["admin-a"] } $null @{ Origin = $Base }
Chk "login from the dashboard's own origin works" ($good.Code -eq 200)
Chk "state-changing call with a foreign Origin is refused" ((Call "POST" "/v1/admin/enrolment-tokens" $null $adA.Session @{ Origin = "http://evil.example" }).Code -eq 403)
Chk "API responses are never cached" ([string](Call "GET" "/v1/me" $null $anA.Session).Headers["Cache-Control"] -match 'no-store')
Write-Host "      Open $dashboardOrigin/ in your browser for the manual click-through (see the checklist)." -ForegroundColor Yellow

# ------------------------------------------------------------------
Section "CLEANUP"
$incIds = Sql "SELECT COALESCE(string_agg(DISTINCT id::text, ','), '') FROM (SELECT ie.incident_id AS id FROM incident_events ie JOIN events e ON e.id = ie.event_id JOIN agents a ON a.id = e.agent_id WHERE a.hostname LIKE 'verify-%' AND NOT EXISTS (SELECT 1 FROM incident_events ie2 JOIN events e2 ON e2.id = ie2.event_id JOIN agents a2 ON a2.id = e2.agent_id WHERE ie2.incident_id = ie.incident_id AND a2.hostname NOT LIKE 'verify-%') UNION SELECT i.id FROM incidents i JOIN agents a ON i.dedup_key = 'DET-005:' || a.id::text WHERE a.hostname LIKE 'verify-%') x"
if ($incIds -match '^[0-9a-f,-]+$') {
  Sql "DELETE FROM approvals WHERE incident_id = ANY(string_to_array('$incIds', ',')::uuid[])" | Out-Null
  Sql "DELETE FROM incident_events WHERE incident_id = ANY(string_to_array('$incIds', ',')::uuid[])" | Out-Null
  Sql "DELETE FROM alerts WHERE incident_id = ANY(string_to_array('$incIds', ',')::uuid[])" | Out-Null
  Sql "DELETE FROM incidents WHERE id = ANY(string_to_array('$incIds', ',')::uuid[])" | Out-Null
}
Sql "DELETE FROM incident_events WHERE event_id IN (SELECT e.id FROM events e JOIN agents a ON a.id = e.agent_id WHERE a.hostname LIKE 'verify-%')" | Out-Null
Sql "DELETE FROM alerts WHERE event_id IN (SELECT e.id FROM events e JOIN agents a ON a.id = e.agent_id WHERE a.hostname LIKE 'verify-%') OR agent_id IN (SELECT id FROM agents WHERE hostname LIKE 'verify-%')" | Out-Null
Sql "DELETE FROM events WHERE agent_id IN (SELECT id FROM agents WHERE hostname LIKE 'verify-%')" | Out-Null
Sql "DELETE FROM agents WHERE hostname LIKE 'verify-%'" | Out-Null
Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
Write-Host "Removed verify-* agents, their events and test incidents (audit log rows stay, by design)."

Write-Host ""
$color = if ($script:fail -eq 0) { "Green" } else { "Red" }
Write-Host "RESULT: $($script:pass) passed, $($script:fail) failed" -ForegroundColor $color
if ($script:fail -gt 0) { Write-Host "Failed checks:"; $script:failed | ForEach-Object { Write-Host " - $_" -ForegroundColor Red } }
exit $script:fail
