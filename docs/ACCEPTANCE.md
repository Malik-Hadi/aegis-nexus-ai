# AEGIS NEXUS AI - Acceptance report (lab MVP, not production-ready)
Final results, 2026-11-23. Host: MALIKS-DEVICE (Windows, Intel i5-1245U, 16 GB RAM, Go 1.27.4). Evidence is in docs/evidence/.

| ID | Priority | Result | Evidence and notes |
|----|----------|--------|--------------------|
| AT-01 One-command Docker start | P0 | PASS | scripts/docker-full-test.ps1 ended "RESULT: 0 failed": compose up healthy, /readyz ready, console on :3000, seed, API recovered after restart (docs/evidence/at01-docker-full-stack.txt). Run scripts/build-binaries.ps1 first; it compiles the Linux programs in a Go container. |
| AT-02 401 anonymous, 403 low privilege | P0 | PASS | go test ./... and scripts/verify-all.ps1 (docs/evidence/final-*-20261123-0536.txt) |
| AT-03 Tenant isolation incl. stream | P0 | PASS | Go tests for cross-tenant read, change and stream isolation |
| AT-04 Windows and Linux agent | P0 | PASS | Windows agent enrolled and replayed fixtures live. Linux agent built and exercised against the API in CI job "Verify persisted SOC workflow and Linux endpoint agent". |
| AT-05 Persistence across restart | P0 | PASS | Verified in earlier full verify-all runs (76 checks passed). The final check ran with -SkipRestart. |
| AT-06 Revoked credential denied | P0 | PASS | verify-all revocation checks |
| AT-07 Five positive fixtures alert, benign stay silent | P0 | PASS | Fixtures DET-001 to DET-005. All five rules also observed firing live (DET-005 opened by itself about 4 minutes after the agent stopped). DET-003 benign retested with a unique source IP: 3 events, 0 alerts. |
| AT-08 Lifecycle transitions recorded | P0 | PASS | Go lifecycle tests and manual demo |
| AT-09 AI rejects bad evidence IDs, clean fallback | P1 | PASS | docs/evidence/at09-ai-20261123-0547.txt |
| AT-10 Containment needs approval | P0 | PASS | Go approval-flow tests and manual demo (requester cannot approve own request) |
| AT-11 Load, 100 events/s for 5 min | P1 | PASS (measured) | 30000 of 30000 accepted (100%), p95 50.59 ms, 0 dropped, 30000 rows stored. An earlier run reached only 58.81% because the API process was gone for part of the run (cause not proven). See docs/evidence/at11-load-test-summary.txt |
| AT-12 Backup restore | P1 | PASS | Restored counts matched the source: 38 events, 6 incidents, 1273 audit rows |
| AT-13 Playwright and dependency scan in CI | P1 | PASS | GitHub Actions ci run #8, job dashboard: browser test and npm audit green; same test passed locally |

Additional checks: inbound request inspection (7 automated tests plus a live check, blocks recorded in audit_logs), govulncheck and ZAP workflow in CI (security), dashboard typecheck and unit tests, go vet.

Final claim: a functional, lab-tested defensive SOC MVP with documented limitations. It is not production-ready and has had no independent security assessment.