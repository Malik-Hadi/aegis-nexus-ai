# AEGIS NEXUS AI

A defensive **Security Operations Center (SOC) prototype** for authorised lab use. It ingests telemetry from enrolled lab agents, applies five deterministic detection rules, raises alerts and incidents, and gives analysts a web console to triage them. Response actions are **simulated only** and need a separate admin approval. Every step is written to an audit log.

> **Status: working lab MVP / beta. Not production-ready and not independently penetration-tested.** Use only on machines you own or are explicitly authorised to monitor. See [Known limitations](#known-limitations).

## What it does

| Area | Details |
|---|---|
| Ingestion | Go API, JSON validation, bounded payloads, rate limits, idempotent event ingest, one-time agent enrolment tokens, revocable agent credentials |
| Detection | `DET-001` six failed logins in 5 min, `DET-002` unexpected privileged group, `DET-003` 10 failed connections to 3 or more ports from one source in 5 min, `DET-004` hash matches the lab IOC list, `DET-005` missing heartbeat. Each has a positive and a benign test fixture in `simulations/` |
| Incidents | Lifecycle with role checks, evidence links to source events, assignment, audit trail |
| AI analyst | Optional. Offline deterministic explanation by default. An optional HTTPS model adapter is validated: unknown event IDs are rejected and failures fall back offline |
| Approvals | Analyst requests a simulated action, a different admin approves or denies. Requesters cannot approve their own request. Nothing real is changed on any host |
| Console | Next.js dashboard: agents, event feed, incidents, triage, approvals, live updates over SSE |
| Multi-tenant | Tenant is taken from the session and re-checked on every read, write and stream |

## Architecture

```
Lab agent (Go, Windows/Linux) -> Go API (auth, RBAC, rules, SSE) -> PostgreSQL 16
                                      ^
Browser -> Next.js console (same-origin gateway) ---+
```

## Requirements

- **Docker Desktop** (Windows, macOS or Linux), with Docker Compose v2
- **Git**
- Nothing else is needed for the Docker route. For the local-process route and running tests you also need **Go 1.26+** and **Node.js 22+**.

## Quick start (one command, Docker)

```bash
git clone https://github.com/Malik-Hadi/Aegis_Nexus_Ai.git
cd Aegis_Nexus_Ai
cp .env.example .env          # Windows PowerShell: Copy-Item .env.example .env
```

Edit `.env` and change these three values (use **letters and digits only** in the database password, because it is placed inside a connection URL):

```
POSTGRES_PASSWORD=<your database password>
DATABASE_URL=postgres://aegis:<your database password>@localhost:5432/aegis?sslmode=disable
SESSION_SECRET=<random string, at least 32 characters>
```

Generate a secret with `openssl rand -hex 32`, or in PowerShell `-join ((48..57)+(97..122) | Get-Random -Count 40 | % {[char]$_})`.

Start everything:

```bash
docker compose -f deploy/docker-compose.yml --env-file .env --profile full up -d --build --wait
```

Create the four lab accounts. **The passwords are printed once. Save them privately.**

```bash
docker compose -f deploy/docker-compose.yml --env-file .env --profile full exec api /app/seed
```

Open **http://localhost:3000** and sign in. Tenant A is `11111111-1111-1111-1111-111111111111`, with users `admin-a` and `analyst-a`. Tenant B is `22222222-2222-2222-2222-222222222222`, with users `admin-b` and `analyst-b`.

Stop with `docker compose -f deploy/docker-compose.yml --env-file .env --profile full down`. Add `-v` only if you want to delete all data.

Check the stack automatically (this wipes the lab database): `powershell -ExecutionPolicy Bypass -File scripts\docker-full-test.ps1`

## Demonstrate the full flow (about 10 minutes)

1. Sign in as `admin-a`. On the **Agents** tab create a one-time enrolment token.
2. Enrol a lab agent and replay the synthetic failed-login sequence (the agent is inside the API container, so it reaches the API directly):

```bash
C="docker compose -f deploy/docker-compose.yml --env-file .env --profile full exec api"
$C /app/agent enrol -token <TOKEN> -api http://127.0.0.1:8080 -hostname demo-01 -state /tmp/demo.json
$C /app/agent run -once -state /tmp/demo.json -queue /tmp/demo.jsonl -replay /app/simulations/det001_positive.json
```

3. A **DET-001** incident appears in the console without refreshing. Open it and check the source events, rule ID and severity.
4. Sign in as `analyst-a` in a second browser profile. Request an AI explanation, then request a simulated isolation.
5. As `admin-a`, approve a request you did not create. Check the decision and the audit trail.
6. Revoke the agent on the Agents tab, then run the replay again. The submission is rejected.

The PowerShell version of the demo is in [docs/runbook.md](docs/runbook.md).

## Run without Docker for the app (local processes)

PostgreSQL still runs in Docker. See [docs/runbook.md](docs/runbook.md) for the full steps. On Windows, once `.env` is configured:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\start-lab.ps1
```

## Tests

```bash
go vet ./... && go test ./...                       # Go suite (needs the database up)
cd apps/dashboard && npm ci && npm test             # dashboard tests
powershell -ExecutionPolicy Bypass -File scripts\verify-all.ps1   # end-to-end lab checks (resets lab passwords)
```

CI runs the Go tests, dashboard tests and build, a Playwright production test, a dependency audit and a database-backed workflow check on every push (`.github/workflows/ci.yml`).

Acceptance results with evidence are in [docs/ACCEPTANCE.md](docs/ACCEPTANCE.md).

## Configuration (`.env`)

| Variable | Meaning |
|---|---|
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_PORT` | Database settings. If port 5432 is taken, change `POSTGRES_PORT` and the port in `DATABASE_URL` |
| `DATABASE_URL` | Used by the local-process route. The Docker stack builds its own from the values above |
| `SESSION_SECRET` | At least 32 characters. The API will not start without it |
| `API_ADDR` | Defaults to `127.0.0.1:8080` |
| `API_TLS_CERT`, `API_TLS_KEY` | Set both to serve the API over HTTPS. Plain HTTP is only allowed on loopback |
| `SOC_ANALYST_URL`, `SOC_ANALYST_TOKEN` | Optional HTTPS AI adapter. Leave blank for the offline explanation |

Never commit `.env`. It is in `.gitignore`.

## Security notes

- Server-side authorisation and tenant checks on every request. Parameterised SQL, request-size limits and rate limits.
- Agent credentials are stored hashed, are revocable, and enrolment tokens are one-time.
- The AI layer treats telemetry as untrusted, redacts it, and can never change incident state or permissions.
- Containment is simulated. There is no remote shell and no change to any real host.
- Rotate the lab passwords (`seed`) before any demo, and keep PostgreSQL private.
- For HTTPS in front of the console, see the TLS section of [docs/runbook.md](docs/runbook.md) and `cmd/tlsproxy`.

## Known limitations

- Lab MVP. No independent security assessment, no HA, no Kubernetes, no EDR-grade collectors.
- The API serves plain HTTP on loopback. The Docker stack publishes the console on `127.0.0.1:3000` only. For any network deployment put trusted HTTPS in front (see the runbook).
- Logging out clears the browser cookie only. Sessions are not tracked server-side and expire after 8 hours.
- Rate limits are in memory on a single API process.
- The 100 events per second load test (`tests/load/ingest.js`, needs k6 installed on the host) has not been measured yet. Results are only reported when measured.
- DET-003 and the other rules detect synthetic lab patterns. They are not proof of real-world intrusion.

## Repository layout

```
apps/dashboard/     Next.js SOC console
services/api/       Go API: auth, RBAC, ingestion, rules, incidents, SSE, AI adapter
agents/endpoint/    Go lab agent (Windows and Linux)
cmd/seed/           Creates lab accounts with random passwords
cmd/tlsproxy/       Lab-only HTTPS terminator
db/migrations/      PostgreSQL schema
simulations/        Positive and benign detection fixtures
deploy/             Docker Compose and Dockerfiles
scripts/            Verification and start-up scripts
tests/              API end-to-end and load tests
docs/               Runbook, OpenAPI, threat model, acceptance report, limitations
```

## Troubleshooting

| Problem | Fix |
|---|---|
| `SESSION_SECRET must be at least 32 characters` | Set a longer secret in `.env` |
| Port 5432 already in use | Set `POSTGRES_PORT` (for example 55435) in `.env`. Also change `DATABASE_URL` if you use local processes |
| Port 3000 already in use | Stop the other program using it |
| Login fails after reseeding | `seed` generates new passwords each time. Use the latest ones |
| Database password rejected after changing `.env` | The old password lives in the data volume. Run `down -v` to reset (this deletes data) |
| Console shows errors | `docker compose -f deploy/docker-compose.yml --env-file .env --profile full logs api dashboard` |

## Licence and use

Lab and educational use on authorised systems only.
