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
| AT-09 | Not re-verified in this session | Covered by Go test suite if present |
| AT-10 | PASS (automated API tests) | approval request, admin decision and requester-cannot-approve covered in lifecycle_test.go, output in docs\evidence\at08-at10-lifecycle-20261122-2339.txt; not shown in the browser |
| AT-11 | NOT MEASURED | Load tool could not reach the API from Docker |
| AT-12 | PASS | 38 events, 6 incidents, 1,273 audit rows matched after restore |
| AT-13 | PARTIAL | CI exists; no Playwright tests |

Go tests: PASS
Verify-all: RESULT: 76 passed, 0 failed

## Known limitations
- Local HTTP only (no TLS). Compose covers the database only.
- AT-11 load test and AT-13 Playwright not completed.
- AT-08/AT-10 and live-stream behaviour not yet shown in the browser.
- Lab MVP only. Not enterprise ready and not independently penetration tested.

