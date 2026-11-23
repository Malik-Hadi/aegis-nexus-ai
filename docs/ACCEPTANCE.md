# AEGIS NEXUS AI - Acceptance Report (lab MVP, not production-ready)
Host: MALIKS-DEVICE | go version go1.27.4 windows/amd64 | Run: 2026-11-22T23:34:25.3196515+05:00

| ID | Result | Evidence / note |
|----|--------|-----------------|
| AT-01 | PARTIAL | Compose runs the database only; API and dashboard started manually |
| AT-02 | PASS | 401/403 tests |
| AT-03 | PASS (backend tests) | Tenant isolation tests; manual Tenant B browser check not run |
| AT-04 | PASS | Windows and Linux agent enrol and submit |
| AT-05 | PASS | Events survive restart |
| AT-06 | PASS | Revoked credential rejected |
| AT-07 | PASS | 5 positive fixtures alert; 5 benign fixtures raise no alerts (DET-003 benign retested with a unique source IP) |
| AT-08 | PASS (automated API tests) | lifecycle_test.go, output in docs\evidence\at08-at10-lifecycle-20261122-2339.txt; not shown in the browser |
| AT-09 | PASS (automated tests) | Invalid evidence ID rejected and offline fallback verified; output in docs\evidence\at09-ai-20261123-0013.txt |
| AT-10 | PASS (automated API tests) | approval request, admin decision and requester-cannot-approve covered in lifecycle_test.go, output in docs\evidence\at08-at10-lifecycle-20261122-2339.txt; not shown in the browser |
| AT-11 | NOT MEASURED | Load tool could not reach the API from Docker |
| AT-12 | PASS | 38 events, 6 incidents, 1,273 audit rows matched after restore |
| AT-13 | PARTIAL | CI exists; no Playwright tests |

Go tests: PASS
Verify-all: RESULT: 76 passed, 0 failed

## Known limitations
- TLS is terminated by a lab reverse proxy (cmd/tlsproxy, self-signed cert, TLS 1.2+) at https://localhost:8443. The API itself listens on plain HTTP over loopback. Lab only: no CA-issued certificate and no end-to-end TLS. Compose covers the database only.
- AT-11 load test and AT-13 Playwright not completed.
- AT-08/AT-10 and live-stream behaviour not yet shown in the browser.
- Lab MVP only. Not enterprise ready and not independently penetration tested.


| TLS | LAB PASS | HTTPS via cmd/tlsproxy; negotiated protocol recorded in docs\evidence\tls-version-20261122-2346.txt; not production TLS |



| Dependency scan (govulncheck) | PASS | Local run: no vulnerabilities found. GitHub security workflow green (run security #2, commit a9b343c) after switching CI to latest stable Go. An earlier run (#1) failed before that fix. |

| AT-13 | PASS | GitHub Actions ci run #8, job dashboard: step 'node scripts/test-production.mjs --browser' green (Chromium login, live event feed, literal telemetry rendering) and 'npm audit --omit=dev' green; same test passed locally (docs/evidence/step4-browser-pass.txt). |

| AT-13 | PASS | GitHub Actions ci run #8, job dashboard: 'node scripts/test-production.mjs --browser' green (Chromium login, live event feed, literal telemetry rendering) and 'npm audit --omit=dev' in the same job; also passed locally (docs/evidence/step4-browser-pass.txt). |

| AT-01 | PASS | scripts\docker-full-test.ps1 ended RESULT: 0 failed on a running Docker stack (docs/evidence/at01-docker-full-stack.txt). Run scripts\build-binaries.ps1 first. |
