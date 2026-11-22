# Known limitations (keep updated)
- Lab prototype, not production ready. Not independently pen-tested.
- Containment is simulated only.
- Fill in as you build.

- Incident explanation defaults to a deterministic offline template. The optional HTTPS adapter accepts only incident-scoped rule and event IDs. Analysis jobs persist in PostgreSQL. The worker runs in the Go process, and the dashboard polls only while waiting for a requested analysis.
- Rate limits apply per connection IP to login and event ingest, with in-memory counters on a single API process. A reverse proxy shares a login budget across its clients.
- A loopback HTTP mode exists for local development. Validate TLS and certificate trust on the target lab environment before a client demo.
- Record database-backed acceptance, browser automation, load, backup and restore results for each release.

## Test status at release (2026-11-22 22:01, lab host, Windows)

| Test | Result |
| --- | --- |
| AT-01 Clean start with Compose | Partial. Compose runs the PostgreSQL database only (healthy on 127.0.0.1:5432). The Go API and the Next.js dashboard run as local processes (see docs/runbook.md). |
| AT-02 to AT-06 | Pass. Covered by scripts/verify-all.ps1 (0 failed) and the Go test suite (56 tests, none skipped, run against the real database). |
| AT-07, AT-08, AT-09, AT-10 | Covered by the verify scripts and Go tests. Output retained as evidence. |
| AT-11 Load test (100 events/s for 5 min) | Pending. k6 was run through Docker using tests/load/ingest.js. See the result recorded below, or treat as not measured if none is listed. |
| AT-12 Backup and restore: pass | pg_dump custom-format backup restored into a fresh database with pg_restore --exit-on-error (RESTORE_OK). Record-count comparison is recorded below if present. |
| AT-13 Playwright and CI | Partial. A CI workflow exists (.github/workflows/ci.yml). Playwright browser tests are not implemented; tests/e2e contains an API script only. |

## Known limitations
- Logging out clears the browser cookie only. Sessions are not tracked on the server, so a stolen cookie stays valid until it expires (8 hours).
- Containment is simulated only. No real change is made to any machine.
- The AI analyst is optional and uses a deterministic template summary when no model key is set.
- Rate limiting and hardening are lab-level, not penetration-tested.
- This is a lab beta MVP, not production ready. No independent security assessment has been done.

- AT-11 load test: NOT MEASURED. k6 was run through Docker, but the container could not reach the API on the Windows host (connection refused on all 30010 requests), so the run is invalid. The script tests/load/ingest.js is provided. Rerun with k6 installed natively on the host to obtain real figures.

- AT-12 backup/restore: PASS. Restored dump record counts matched the source database (events, incidents, audit_logs).

- AT-12 backup/restore: PASS. pg_dump custom-format backup restored into a fresh database with pg_restore --exit-on-error; record counts (events,incidents,audit_logs) matched: 38,6,1273.

- DET-003 benign fixture alerted once when reusing the positive fixture's source IP within the 5-minute window (test isolation, not a rule flaw). Retest with a unique IP: PASS (3 events stored, 0 alerts, unique source IP)
