# Step 2: inbound request inspection. Safe to run: backs up main.go, tests, and rolls back on failure.
# Run from the repo root (D:\Projects\aegis-nexus-ai) with the step2 folder contents unzipped over it.
$ErrorActionPreference = "Continue"
Set-Location (Split-Path $PSScriptRoot -Parent)
New-Item -ItemType Directory -Force docs\evidence | Out-Null
$log = "docs\evidence\step2-inspection-$(Get-Date -Format 'yyyyMMdd-HHmm').txt"
function Log($m) { $m | Tee-Object -FilePath $log -Append }
"Host: $env:COMPUTERNAME | $(go version) | Run: $(Get-Date -Format o)" | Out-File $log -Encoding utf8

foreach ($f in "services\api\inspect.go","services\api\inspect_test.go","services\api\main.go") { if (-not (Test-Path $f)) { Log "MISSING $f - unzip step2 over the repo first"; return } }

Log "== 1. BASELINE (before wiring): existing API tests"
$base = (go test ./services/api -count=1 2>&1 | Out-String)
Log $base
if ($base -match '(?m)^(FAIL|---\s*FAIL)' -and $base -notmatch 'inspect') {
  Log "BASELINE ALREADY FAILING. Nothing was changed in main.go. Paste the output above to Claude."
  return
}

Log "== 2. format and vet new files"
$fmt = (gofmt -l services\api\inspect.go services\api\inspect_test.go 2>&1 | Out-String)
if ($fmt.Trim()) { Log "gofmt reformatting: $fmt"; gofmt -w services\api\inspect.go services\api\inspect_test.go }
$vet = (go vet ./services/api 2>&1 | Out-String); Log $vet
if ($LASTEXITCODE -ne 0) { Log "VET FAILED - paste this to Claude. main.go untouched."; return }

Log "== 3. new inspection tests (no database needed)"
$t1 = (go test ./services/api -count=1 -run "TestInspect" -v 2>&1 | Out-String); Log $t1
if ($t1 -match '--- FAIL' -or $t1 -notmatch '--- PASS: TestInspectBlocksKnownBadPatterns') { Log "INSPECTION TESTS FAILED - paste the FAIL lines to Claude. main.go untouched."; return }

Log "== 4. wire into main.go (backup first)"
Copy-Item services\api\main.go services\api\main.go.bak -Force
$src = Get-Content services\api\main.go -Raw
$old = 'Handler:           limitRequests(srv.routes()),'
$new = 'Handler:           srv.inspectRequests(os.Getenv("INSPECT_MODE"), limitRequests(srv.routes())),'
if ($src.Contains($new)) { Log "main.go already wired." }
elseif ($src.Contains($old)) { [IO.File]::WriteAllText((Resolve-Path services\api\main.go), $src.Replace($old,$new), (New-Object Text.UTF8Encoding $false)); Log "main.go wired." }
else { Log "Could not find the Handler line in main.go. Send me lines 95-115 of services/api/main.go."; Remove-Item services\api\main.go.bak; return }

Log "== 5. build and FULL API test suite after wiring"
go build -o NUL ./services/api 2>&1 | Out-Null
$build = $LASTEXITCODE
$full = (go test ./... -count=1 2>&1 | Out-String); Log $full
if ($build -ne 0 -or $full -match '(?m)^(FAIL|---\s*FAIL)') {
  Copy-Item services\api\main.go.bak services\api\main.go -Force
  Log "FAILED AFTER WIRING - main.go restored from backup. Paste the FAIL lines to Claude."
  return
}
Remove-Item services\api\main.go.bak -ErrorAction SilentlyContinue

if (-not (Select-String -Path .env.example -Pattern '^INSPECT_MODE=' -Quiet)) {
  Add-Content .env.example "`n# Inbound request inspection: block (default), monitor (log only) or off`nINSPECT_MODE=block"
}
Log "VERDICT STEP 2: PASS (inspection tests + full suite green). Restart the API, then run the live check."
