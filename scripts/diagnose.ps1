# AEGIS NEXUS AI - project diagnosis. READ-ONLY: it changes nothing.
# Run from the project root:  powershell -ExecutionPolicy Bypass -File scripts\diagnose.ps1
# It writes diagnostic-report.txt (passwords, secrets and tokens are masked). Send me that file.
param([switch]$SkipTests)
$ErrorActionPreference = "Continue"
$out = New-Object System.Collections.Generic.List[string]
function Say($t) { $out.Add([string]$t); Write-Host $t }
function Head($t) { Say ""; Say "=== $t ===" }
function Run($label, [scriptblock]$sb, [int]$max = 60) {
  Say "> $label"
  try {
    $lines = @(& $sb 2>&1 | Out-String -Stream)
    $lines | Select-Object -First $max | ForEach-Object { Say ("   " + $_) }
    if ($lines.Count -gt $max) { Say "   ... ($($lines.Count - $max) more lines)" }
  } catch { Say "   ERROR: $($_.Exception.Message)" }
}
function Has($c) { $null -ne (Get-Command $c -ErrorAction SilentlyContinue) }
if (-not (Test-Path go.mod)) { Write-Host "Run this from the project root (go.mod must be here)." -ForegroundColor Red; exit 2 }
$compose = @("compose", "-f", "deploy/docker-compose.yml", "--env-file", ".env")

Head "1. MACHINE AND TOOLS"
Say "time: $(Get-Date -Format s)   folder: $(Get-Location)"
Say "windows: $([Environment]::OSVersion.VersionString)   powershell: $($PSVersionTable.PSVersion)"
foreach ($c in @("go version", "node -v", "npm -v", "git --version", "docker --version")) {
  $exe = $c.Split(" ")[0]
  if (Has $exe) { Run $c ([scriptblock]::Create($c)) 3 } else { Say "> $c`n   MISSING" }
}

Head "2. DOCKER AND THE DATABASE CONTAINER"
if (Has docker) {
  Run "docker engine" { docker info --format "engine={{.ServerVersion}} os={{.OperatingSystem}} cpus={{.NCPU}} memGB={{.MemTotal}}" } 5
  Run "containers (all)" { docker ps -a --format "{{.Names}} | {{.Status}} | {{.Ports}}" } 20
  Run "volumes" { docker volume ls --format "{{.Name}}" } 20
  Say "> compose file deploy/docker-compose.yml"
  if (Test-Path deploy/docker-compose.yml) { Get-Content deploy/docker-compose.yml | Select-Object -First 70 | ForEach-Object { Say ("   " + $_) } } else { Say "   MISSING" }
  Run "compose status" { docker @compose ps -a } 15
  Run "database logs (last 40 lines)" { docker @compose logs --tail 40 db } 45
} else { Say "docker command not found" }

Head "3. PORTS IN USE (5432/5433 database, 8080 API, 3000 dev dashboard)"
try {
  Get-NetTCPConnection -LocalPort 5432, 5433, 8080, 3000 -State Listen -ErrorAction Stop | ForEach-Object {
    $p = Get-Process -Id $_.OwningProcess -ErrorAction SilentlyContinue
    Say ("   port {0} <- {1} (pid {2})" -f $_.LocalPort, $(if ($p) { $p.ProcessName } else { "?" }), $_.OwningProcess)
  }
} catch { Say "   nothing is listening on those ports" }

Head "4. .env (names and shapes only, secrets masked)"
$envs = @{}
if (Test-Path .env) {
  Get-Content .env | ForEach-Object {
    if ($_ -match '^\s*([^#=\s]+)\s*=(.*)$') {
      $k = $matches[1]; $v = $matches[2].Trim(); $envs[$k] = $v
      if ($k -eq "DATABASE_URL") { $shown = $v -replace '(://[^:]+:)[^@]+(@)', '$1***$2' }
      elseif ($k -match 'PASSWORD|SECRET|KEY|TOKEN') { $shown = "***(length $($v.Length))" }
      else { $shown = $v }
      Say "   $k = $shown"
    }
  }
  if ($envs["DATABASE_URL"] -match '^postgres(ql)?://([^:]+):[^@]*@([^:/]+):(\d+)/([^?]+)') {
    $u, $h, $pt, $d = $matches[2], $matches[3], $matches[4], $matches[5]
    Say "   url says: user=$u host=$h port=$pt db=$d"
    if ($envs["POSTGRES_USER"] -and $u -ne $envs["POSTGRES_USER"]) { Say "   MISMATCH: DATABASE_URL user '$u' differs from POSTGRES_USER '$($envs['POSTGRES_USER'])'" }
    if ($envs["POSTGRES_DB"] -and $d -ne $envs["POSTGRES_DB"]) { Say "   MISMATCH: DATABASE_URL database '$d' differs from POSTGRES_DB '$($envs['POSTGRES_DB'])'" }
    if ($envs["POSTGRES_PORT"] -and $pt -ne $envs["POSTGRES_PORT"]) { Say "   MISMATCH: DATABASE_URL port '$pt' differs from POSTGRES_PORT '$($envs['POSTGRES_PORT'])'" }
    $pwInUrl = ($envs["DATABASE_URL"] -split '://')[1].Split('@')[0].Split(':', 2)[1]
    if ($envs["POSTGRES_PASSWORD"] -and $pwInUrl -ne $envs["POSTGRES_PASSWORD"]) { Say "   MISMATCH: the password inside DATABASE_URL differs from POSTGRES_PASSWORD" }
  } else { Say "   DATABASE_URL is missing or not in the form postgres://user:password@host:port/db" }
  if (-not $envs["SESSION_SECRET"] -or $envs["SESSION_SECRET"].Length -lt 32) { Say "   PROBLEM: SESSION_SECRET is missing or shorter than 32 characters (the API will refuse to start)" }
} else { Say "   .env is MISSING" }

Head "5. DATABASE CONTENT"
$dbu = $envs["POSTGRES_USER"]; $dbn = $envs["POSTGRES_DB"]
function Sql($q) { $o = & docker @compose exec -T db psql -U $dbu -d $dbn -t -A -c $q 2>&1 | Out-String; return $o.Trim() }
if (Has docker) {
  Run "pg_isready" { docker @compose exec -T db pg_isready -U $dbu -d $dbn } 3
  $v = Sql "SELECT current_user || ' @ ' || current_database()"
  Say "   connected as: $v"
  if ($v -match ' @ ') {
    $tables = Sql "SELECT string_agg(table_name, ', ' ORDER BY table_name) FROM information_schema.tables WHERE table_schema = 'public'"
    Say "   tables: $tables"
    foreach ($t in @("tenants","users","agents","events","rules","alerts","incidents","incident_events","approvals","audit_logs","enrolment_tokens")) {
      if ($tables -notmatch "\b$t\b") { Say "   MISSING TABLE: $t" }
    }
    foreach ($c in @("incidents.rule_id", "alerts.incident_id", "alerts.agent_id", "approvals.executed_at", "approvals.created_at")) {
      $tn, $cn = $c.Split(".")
      if ((Sql "SELECT count(*) FROM information_schema.columns WHERE table_name = '$tn' AND column_name = '$cn'") -ne "1") { Say "   MISSING COLUMN: $c  (apply db/migrations/004 and 005)" }
    }
    foreach ($t in @("tenants","users","agents","events","incidents","alerts","approvals","audit_logs")) {
      if ($tables -match "\b$t\b") { Say ("   rows in {0,-10} = {1}" -f $t, (Sql "SELECT count(*) FROM $t")) }
    }
  }
}

Head "6. API"
foreach ($u in @("/healthz", "/readyz", "/v1/me")) {
  try { $r = Invoke-WebRequest "http://localhost:8080$u" -UseBasicParsing -TimeoutSec 4; Say "   GET $u -> $($r.StatusCode) $($r.Content.Substring(0, [Math]::Min(80, $r.Content.Length)))" }
  catch { $code = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { "no answer" }; Say "   GET $u -> $code" }
}
try {
  $r = Invoke-WebRequest "http://localhost:8080/" -UseBasicParsing -TimeoutSec 4
  Say "   GET / -> $($r.StatusCode); Next.js build served: $($r.Content -match '/_next/static/')"
} catch { Say "   GET / -> no answer" }
Say "   dashboard build present (apps\dashboard\out\index.html): $(Test-Path apps\dashboard\out\index.html)"

Head "7. GIT (what changed since the last commit)"
if (Has git) {
  Run "last commits" { git log --oneline -15 } 15
  Run "uncommitted changes (status)" { git status --short } 60
  Run "size of the changes" { git diff --stat HEAD } 40
  Run "stashes" { git stash list } 5
}

Head "8. FILES MODIFIED IN THE LAST 72 HOURS (outside node_modules/.git)"
Get-ChildItem -Recurse -File -ErrorAction SilentlyContinue | Where-Object {
  $_.FullName -notmatch '\\(node_modules|\.git|\.next|out|bin)\\' -and $_.LastWriteTime -gt (Get-Date).AddHours(-72)
} | Sort-Object LastWriteTime -Descending | Select-Object -First 70 | ForEach-Object {
  Say ("   {0}  {1}" -f $_.LastWriteTime.ToString("MM-dd HH:mm"), (Resolve-Path -Relative $_.FullName))
}

Head "9. YOUR FILES COMPARED WITH THE FILES I DELIVERED"
$expected = [ordered]@{
  ".github/workflows/ci.yml" = "288d1107f6e553cd739f14d2483cae5fa87932a62724c55900124f5afd78f86d"
  "agents/endpoint/agent_test.go" = "a5805bb04323ad4580e593ea61f25688a0ace21f98b0db618e6061e1d6039583"
  "agents/endpoint/client.go" = "959438674a74fd845ae649cb57993347983f45ea56da04fb733e0aa7910fccdb"
  "agents/endpoint/fixture.go" = "7e615a645fbbba168c2bde7b7fe964e99991ba1e4e5bf3266f33003d03ced860"
  "agents/endpoint/main.go" = "5a30f32947a0d31592992949af2447804935a27cfcfdef22dfb65047b9dd6d1d"
  "apps/dashboard/.gitignore" = "f9a3c7990c86a8d254039ea690f2c7af73ccadeb9acfea2f64b25d02918f78d9"
  "apps/dashboard/.gitkeep" = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  "apps/dashboard/components/AgentsView.tsx" = "fe8ac885103b203f9e363af1fdde251a0ea6364071093916740273e841f2edc5"
  "apps/dashboard/components/Badge.tsx" = "1aaa5bf5b81f57c87185e5ea1d1e46d260f512fcf3c482c8f28397ad71a69893"
  "apps/dashboard/components/EventsView.tsx" = "f59236401fbf27a8c75ea0334380d5b5f7abc3be801267fbf5a1be88ec7b47e1"
  "apps/dashboard/components/Header.tsx" = "c88bba86ac53b59506a0a81b044939f5ec6e5e3fe0d24016b3bb44ff286ab5b1"
  "apps/dashboard/components/IncidentDetailView.tsx" = "143acbf2481d636b294fe25c4db299335641aae7cfd830be648315bf67a84371"
  "apps/dashboard/components/IncidentsView.tsx" = "44e975db4122c22d69fce71ff38528254e8179faf3150dc3f8dbab7c26a12c8b"
  "apps/dashboard/components/LoginForm.tsx" = "c72cb03411fe14dae58410f220fd08a3d402ef7c35ffd16ae8d3f692637a26a1"
  "apps/dashboard/components/Toasts.tsx" = "92d123cfece88c80b68a226fdc713c32cdb1ffe73258a46331f5f121efa3ee1e"
  "apps/dashboard/lib/api.ts" = "b35b956b8a950600078743ec196c6d18e858abe689a7621f34831c3fa38f40cf"
  "apps/dashboard/lib/format.ts" = "7d932570af85e3a16caf6c4132d0c99b8f8e325a230a8da6d9427a86512bb52c"
  "apps/dashboard/lib/types.ts" = "6d419df78cf98ca9fa34e0fef9934e48cc6eadd95d3e5d99745b0e6e45a69a17"
  "apps/dashboard/lib/useLive.ts" = "3c4c7d758117a3e61c3e57b265ae34c7f2cc50e9397ed87c1d77f2b7a5258927"
  "apps/dashboard/next-env.d.ts" = "9269d492817e359123ac64c8205e5d05dab63d71a3a7a229e68b5d9a0e8150bf"
  "apps/dashboard/next.config.js" = "1e3704294ac06f7ad8524e53b7cb8b614af45bd46c61a1396700d8f18421c00a"
  "apps/dashboard/package.json" = "2ee767cb2f791c396bf0232639882701f02c9cbd9abec8adfc3851d1da94afde"
  "apps/dashboard/pages/_app.tsx" = "8de0326c07b14ea9a00024edcaf8ea767c8b443ab62d8edcd4e16be057a3fee3"
  "apps/dashboard/pages/_document.tsx" = "7a1c264423bd3f5cadb3b1d831af81a989a54d9bc6b659e87dc646cadd98017e"
  "apps/dashboard/pages/index.tsx" = "23357cdf316456ca4e6f008629c5a31441bd4d509481d7e168bb7d0d1867ae73"
  "apps/dashboard/postcss.config.js" = "fe0d5cf3cb8c1922f71cca2da3796f3e63366623ad6164336655766efe330e6a"
  "apps/dashboard/styles/globals.css" = "6456f2eb3a5265757efac7e996229a15080fed560460a8727aa03cd0420a6586"
  "apps/dashboard/tailwind.config.js" = "e98dfb31eb3f47536ec65566e0b342db06acee1607e9e276f4f6db2d4d169bb9"
  "apps/dashboard/tsconfig.json" = "fc3ce4299bda5caaf20d7ae8c678ff4fc0f1a451eb0bc2781e7da918b71aa556"
  "cmd/seed/main.go" = "f99255f599b08b199576c437efa12e54af30a62f93fac97b9dc51445c7e0c08e"
  "db/migrations/001_core_schema.sql" = "2e937598b282090a12e838b93e3e656c85986db71012f27ce1d2153d637ceee3"
  "db/migrations/002_seed_lab.sql" = "79a1297dcc762b26eecc5518eb18cd312a29239c395584420945b5a7369fb536"
  "db/migrations/003_enrolment.sql" = "65a2adf522edb7d6ae03a10220bfc4c3197854e511aadf34211ca960d7d85a7c"
  "db/migrations/004_detection.sql" = "151939d8b8ccfa0bdd96139e42d81e7d1fe2570cdc505b1cbcf4cbf820f9dd76"
  "db/migrations/005_lifecycle.sql" = "5ee2dca2625baa72eeb8c6d26cc3f04259c4c9c45349859ac4c8934b4609a4e2"
  "deploy/docker-compose.yml" = "6625cc00e6e40a7f6606004f68281eeec5c87cdb6a481a5879f87a7fb4e75550"
  "scripts/build-dashboard.ps1" = "6eafac3737832eb967715258afde86c86493a32990da0468e0144d94112af546"
  "scripts/new-enrol-token.ps1" = "dd565661305e98059eb5b70b07a57d592efbbc4da72e679ae64db7be02ec2b25"
  "scripts/verify-all.ps1" = "bf8674680471caeb65b000ec207b2a288b834bb9100cb1e9c6cfe15f8d596a0e"
  "scripts/verify-backend.ps1" = "02cea722732e338171a54b2dfa24291ec0081db0b408b82680d9dfcf36ec08f8"
  "scripts/verify-features.ps1" = "19487640aeb8eb71ea9b8e8142a2502ed4a2fd2aca52a35e19c170dfc052dd40"
  "scripts/watch-stream.ps1" = "a644fad480850b583cdc8a34e6f4dd175d8ee9d6e9938f7a6989503a26b83d23"
  "services/ai-analyst/.gitkeep" = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  "services/api/agents.go" = "06eb3bd94302972fbba4df094e7c671b459c5771c6e274ac529504728cfe77e1"
  "services/api/auth.go" = "56337069c31587e3e2f7c3f89e6141e4a9e73d72667f035f371d4a7a3a6d3519"
  "services/api/auth_test.go" = "ab858034eae73a054732860cbe4e0800fea81bc0db8235d9ce01cea7dace79c0"
  "services/api/block2_test.go" = "affb1078d61316f6d19a81fe5360491b3973b54eeded14292b4170beea64d0cd"
  "services/api/events.go" = "b65040d817d5b2257de7af60c52c225006728ce15937e3f86466a79940537b34"
  "services/api/hub.go" = "8846be1da98d1f40d1b2c15f40c8c8aa40f4e2a3647e6547d06eb8c254f1bf45"
  "services/api/incidents.go" = "a44ed1744f92bb96763965e4f627ec3b15226a78007051c0f94a54a93c64a74b"
  "services/api/lifecycle.go" = "a70e6e1d7085d7f60190c8db4117d205afc9db2127f66111a72e96e415fcc46f"
  "services/api/lifecycle_test.go" = "42f435cca7c92e7facc91fac461a0402494eee104e1beac1c72d4a74b90d1b45"
  "services/api/main.go" = "c1548b86a30c6d9fb4e2f2f710be4cff85b7216e1f3ee80601b7df01eface024"
  "services/api/realtime_test.go" = "f6958c3a850148086ae27973df513e490aa11b19734e645ffedf2d9973aa17a2"
  "services/api/rules.go" = "6b0b3f36f91108a00a08d398e3ea5c5bea0ba68575e4f4cc0ca93188d18e0fd4"
  "services/api/rules_test.go" = "151df2977621013b3be8dd61fafbe867a031a24ae955a534bfdc769cbc13ac68"
  "services/api/server.go" = "a5c4312c34d5dc064af783a50c419071723b511532f8f41ab20b33076ef0409d"
  "services/api/web.go" = "bcb693c961dbc11ac4dcafe373dae5e133bcc8a731b77cf99eff4646bfcea308"
  "services/api/web/app.js" = "dec2c73b85d506153eb28fdfe7cd6e9513a60c31e45072793b36bc2ce66fd95c"
  "services/api/web/index.html" = "f5c7a9b42c60ef363c149af25e6f4519b45aad68748d6b85407406fe66a2b408"
  "services/api/web/style.css" = "40050c07bef3292d0d62327cbc177cea0ae728628eab82626231b1908e378508"
  "services/api/web_test.go" = "c4a441c5364f2569a5db42fad8efd400def200d51007dac602b15773e815e4ce"
  "services/detector/.gitkeep" = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  "simulations/det001_benign_five.json" = "61a3a241735cd028026f6671a5eeca1f8a8233081daea88368761fcf93b502d2"
  "simulations/det001_benign_spread.json" = "2c63b93fe60604dd6e998fce2c59a825356af5c3ee774a789add5d029b17779d"
  "simulations/det001_positive.json" = "32eecc85a43482abb0b5aaa33729f68a54cfc0a3232774fbb1a656dd5bf02289"
  "simulations/det002_benign.json" = "8e94e38bf03dd98dabcfb6ce03c61a0d57326ee8895c05d5ddea5057699f98f0"
  "simulations/det002_positive.json" = "1744a5c500fc9a4653fa1f58a9e970ccc1b38580877630b30c91decd2958e80e"
  "simulations/det003_benign.json" = "f6063c583127a6f332efb5d6f2d013d0f95c66465d9f27d00fc7cc75c14a5f92"
  "simulations/det003_positive.json" = "5523a99c1ee2c7a60991a2bc6a5bd166ab6a13f547c4d507255cd5eb428c0cd0"
  "simulations/det004_benign.json" = "7c9b3730644520324b4e98551f8e0df48128501169de7b00564f0d2e3e2640ac"
  "simulations/det004_positive.json" = "d915b8480786f8eaf5552ea024f20274f60e4d3348de60cb9a63736337fcb446"
  "simulations/ioc_hashes.txt" = "f06000d77dc035cd884d37a1182cf06d533fb83c4229c6c84911aa5962884cf5"
}

function NormHash($path) {
  $b = [IO.File]::ReadAllBytes($path); $ms = New-Object IO.MemoryStream
  foreach ($x in $b) { if ($x -ne 13) { $ms.WriteByte($x) } }
  $sha = [Security.Cryptography.SHA256]::Create()
  return ([BitConverter]::ToString($sha.ComputeHash($ms.ToArray())) -replace '-', '').ToLower()
}
$root = (Get-Location).Path
$same = 0; $modified = @(); $missing = @()
foreach ($rel in $expected.Keys) {
  $p = Join-Path $root ($rel -replace '/', '\')
  if (-not (Test-Path $p)) { $missing += $rel; continue }
  if ((NormHash $p) -eq $expected[$rel]) { $same++ } else { $modified += $rel }
}
Say "   identical to what I delivered: $same of $($expected.Count)"
Say "   MODIFIED ($($modified.Count)):"; $modified | ForEach-Object { Say "      $_" }
Say "   MISSING ($($missing.Count)):"; $missing | ForEach-Object { Say "      $_" }
Say "   (scripts\verify-*.ps1 may differ on purpose: you changed a test count. .gitkeep files do not matter.)"
$extra = @()
foreach ($d in @("services", "cmd", "agents", "db", "apps\dashboard", "simulations", "deploy", ".github")) {
  if (Test-Path $d) {
    Get-ChildItem $d -Recurse -File -ErrorAction SilentlyContinue | Where-Object { $_.FullName -notmatch '\\(node_modules|\.next|out)\\' -and $_.Name -notin @("package-lock.json", "tsconfig.tsbuildinfo") } | ForEach-Object {
      $rel = Resolve-Path -Relative $_.FullName
      if ($rel.StartsWith(".\")) { $rel = $rel.Substring(2) }
      $rel = $rel -replace '\\', '/'
      if (-not $expected.Contains($rel)) { $extra += $rel }
    }
  }
}
Say "   EXTRA files I never delivered ($($extra.Count)) - these are the ones to look at:"; $extra | ForEach-Object { Say "      $_" }

Head "10. STACK CHECK AGAINST THE PDF (Go + PostgreSQL + Next.js/TypeScript/Tailwind)"
Say "> go.mod"; Get-Content go.mod | Select-Object -First 30 | ForEach-Object { Say "   $_" }
$bad = @("gin-gonic", "gorm.io", "gofiber", "labstack/echo", "mattn/go-sqlite", "mongo-driver", "go-redis", "sqlite", "firebase", "supabase")
$hits = Get-ChildItem -Recurse -File -Include *.go, go.mod -ErrorAction SilentlyContinue | Where-Object { $_.FullName -notmatch '\\(node_modules|\.git)\\' } | Select-String -Pattern ($bad -join '|') -SimpleMatch:$false
if ($hits) { Say "   STACK DRIFT in Go files:"; $hits | Select-Object -First 15 | ForEach-Object { Say "      $($_.Path):$($_.LineNumber): $($_.Line.Trim())" } } else { Say "   Go: no drift found (no gin/gorm/fiber/echo/sqlite/mongo/redis)" }
if (Test-Path apps\dashboard\package.json) {
  $pj = Get-Content apps\dashboard\package.json -Raw | ConvertFrom-Json
  Say "   dashboard dependencies: $((@($pj.dependencies.PSObject.Properties.Name) + @($pj.devDependencies.PSObject.Properties.Name)) -join ', ')"
} else { Say "   apps\dashboard\package.json MISSING (the PDF requires a Next.js dashboard)" }
Say "   other top-level folders: $((Get-ChildItem -Directory | Where-Object { $_.Name -notin @('.git','.vscode','node_modules','agents','apps','cmd','db','deploy','docs','scripts','services','simulations','tests','bin','rules','.github') } | ForEach-Object { $_.Name }) -join ', ')"

if (-not $SkipTests -and (Has go)) {
  Head "11. BUILD AND TESTS"
  Run "go build ./..." { go build ./... } 25
  Run "go vet ./..." { go vet ./... } 15
  Say "> go test ./... (about 30 seconds; needs the database for the integration tests)"
  $t = & go test ./... -count=1 2>&1 | Out-String -Stream
  $t | Where-Object { $_ -match '^(ok|FAIL|---\s+FAIL|panic|\?)|SKIP' } | Select-Object -First 40 | ForEach-Object { Say "   $_" }
  $skips = @($t | Where-Object { $_ -match '^\s*---\s+SKIP' }).Count
  Say "   skipped tests: $skips (skips mean the database was not reachable)"
}

Head "DONE"
Say "Passwords and tokens in this report are masked. Send me the file diagnostic-report.txt"
$out | Set-Content -Path diagnostic-report.txt -Encoding UTF8
Write-Host "`nSaved: $(Join-Path (Get-Location) 'diagnostic-report.txt')" -ForegroundColor Green
