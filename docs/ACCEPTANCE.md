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
| AT-07 | Positives PASS; benign 4/5 PASS; DET-003 benign: PASS (3 events stored, 0 alerts, unique source IP) | Earlier DET-003 alert came from reusing the positive fixture source IP within the 5-minute window (test isolation, not a rule flaw: rule needs 10 failures across 3 or more ports) |
| AT-08 | BUILT, routes verified | Not demonstrated end to end in the browser |
| AT-09 | Not re-verified in this session | Covered by Go test suite if present |
| AT-10 | BUILT, routes verified | Not demonstrated end to end in the browser |
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
