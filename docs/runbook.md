# AEGIS NEXUS AI lab runbook

## Start the lab

After configuring the root .env, a Windows loopback lab can use `powershell -File scripts/start-lab.ps1`. The command starts PostgreSQL, applies the additive analysis migration, builds the dashboard if needed and keeps the Go and Next.js services running until stopped. The manual steps below also support troubleshooting. If Windows refuses host port 5432, set POSTGRES_PORT and the host port in DATABASE_URL to the same available port, such as 55435. The container still uses port 5432.

Use Go, Node.js 22 or newer and Docker Desktop. In the project root create a local .env from .env.example when needed. Set the database password and session secret. Keep these values outside Git. Rotate lab account passwords before a client demo and share them privately with the operators.

1. Start the PostgreSQL service with `docker compose -f deploy/docker-compose.yml --env-file .env up -d`.
2. Check its status with `docker compose -f deploy/docker-compose.yml --env-file .env ps`. The mounted SQL migrations run on initialization of an empty database volume. For an existing Block 6 volume, apply migration 006 using the command below before starting the new API.
3. In a new terminal run `go run ./services/api`. Check `http://localhost:8080/healthz` and `http://localhost:8080/readyz`. Readiness requires PostgreSQL.
4. Use existing lab passwords. In a disposable lab, `go run ./cmd/seed` creates fresh passwords for all four seeded accounts.
5. From the root run `powershell -File scripts/build-dashboard.ps1`, then run `npm.cmd start` from `apps/dashboard`. The SOC console is at `http://localhost:3000`. Copy the dashboard's .env.example to .env.local when configuring custom origins.
6. With both services running, run `npm.cmd run smoke` from `apps/dashboard`.

For an existing Block 6 database that already has migrations 001 through 005, apply the additive analysis migration. Substitute your configured database name and user if they differ from the defaults:

```powershell
docker compose -f deploy/docker-compose.yml --env-file .env exec -T db psql -X -v ON_ERROR_STOP=1 -U aegis -d aegis -f /docker-entrypoint-initdb.d/006_analysis_jobs.sql
```

Plain HTTP is limited to the loopback lab. For an authorised network deployment, set API_TLS_CERT and API_TLS_KEY to a trusted certificate pair and configure the API origin to use HTTPS. Go uses secure cookies when serving TLS. Serve the dashboard over trusted HTTPS, configure its browser-facing SOC_PUBLIC_ORIGIN, and disable proxy buffering for the stream. Configure trust in the agent and Node runtimes through their certificate stores. Keep the database private. Run node apps/dashboard/scripts/verify-https.mjs with the authorised HTTPS origin and set SOC_LAB_SUBJECT, SOC_LAB_TENANT and SOC_LAB_PASSWORD privately for the authenticated checks. It verifies certificate trust, response headers, cookie flags, tenant sign-in and the first SSE frame.

## Verify and demonstrate

Run `go vet ./...` and `go test ./...` from the root. The dashboard build script runs Node tests, a production build and an HTTP fixture that verifies login-cookie forwarding, the security headers and unbuffered live delivery.

On a disposable integration lab, launch the production dashboard and run `powershell -File scripts/verify-all.ps1 -SkipRestart` from the root. This suite resets all four lab passwords and creates and removes verification records. Run the restart case separately to collect persistence evidence. Record the host, versions, date, pass/fail counts and remaining failures for handover.

For the demo, use separate browser profiles for Analyst and Admin. Admin enrols a disposable lab agent, then the agent replays the repository's synthetic failed-login fixture. Analyst opens the incident, checks the cited source events, reviews the offline explanation and requests simulated isolation. Admin approves a request they did not create. Show the recorded decision and audit trail, then revoke the agent and verify that another submission is rejected.

From the project root, with the demo lab running and a token created in the Agents view:

```powershell
$token = Read-Host 'Paste the one-time enrolment token'
$id = [Guid]::NewGuid().ToString('N').Substring(0, 8)
$state = Join-Path $env:TEMP "aegis-demo-$id.json"
$queue = Join-Path $env:TEMP "aegis-demo-$id.jsonl"
go run ./agents/endpoint enrol -token $token -hostname "demo-$id" -state $state
Remove-Variable token
go run ./agents/endpoint run -once -state $state -queue $queue -replay ./simulations/det001_positive.json
```

The replay agent rebases fixture time for the current lab run. The six synthetic failures should produce one DET-001 incident with linked events. Repeat a submission after revocation to demonstrate credential rejection using that disposable agent.
## Backup and rollback

Before schema changes, create a restricted PostgreSQL custom-format dump with `pg_dump -Fc -f aegis.dump` using the lab database connection settings. Keep the dump outside the repo. Restore into a new disposable database using `pg_restore --exit-on-error`; compare the event and incident counts and sample hashes before using the restored data. Record the versions and exact restore result. To roll back application code, stop the Go and Next.js processes and redeploy the prior tested revision against a compatible schema or a verified restore. Keep the original database volume available until the restore is validated.
## Load benchmark

Use a disposable loopback lab and a dedicated enrolled agent. Set SOC_LOAD_AGENT_TOKEN to its agent credential and SOC_LOAD_RUN_ID to a fresh alphanumeric run ID. Run `k6 run tests/load/ingest.js`. The fixture emits ten batches of ten benign events per second for five minutes, expects at least 99% API acceptance following a fresh insert, p95 under 500 ms and zero dropped iterations. Record the machine configuration and cross-check the database row count by the fixture's source_ref, k6- followed by the run ID. Keep the credential private and revoke it after the benchmark.

## Optional model adapter

Leave SOC_ANALYST_URL blank for deterministic offline analysis. For a lab-approved adapter, set SOC_ANALYST_URL to its trusted HTTPS POST endpoint and optionally set SOC_ANALYST_TOKEN. The request contains rule_id and event_ids only. The adapter must return a JSON object containing summary, confidence (low, medium or high), referenced_event_ids and proposed_next_steps. The service rejects unknown IDs, invalid output and redirects, and falls back offline on timeout or adapter failure. Use only an approved environment and collect synthetic evidence for the demo.
