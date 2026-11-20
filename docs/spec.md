# AEGIS NEXUS AI spec (from the PDF)
Goal: lab SOC MVP. Agent -> TLS -> Go ingestion/auth API -> PostgreSQL -> rule evaluator -> alerts/incidents -> Next.js dashboard. Optional AI analyst (structured, evidence-grounded, offline fallback). Human-approved SIMULATED containment. Everything audited.

Roles: Admin, Analyst. Tenant id on every domain record.

## Tables
tenants, users, agents, events, rules, alerts, incidents, incident_events, approvals, audit_logs (see db/migrations/001_core_schema.sql).

## Endpoints (/v1)
- POST /agents/enrol : one-time lab token -> scoped agent credential
- POST /events : agent-authenticated, idempotency key, 202 on accept
- GET /events : tenant-scoped, paginated, never cross-tenant
- GET /agents : tenant-scoped list + heartbeat status
- POST /agents/{id}/revoke : Admin only; later submissions rejected
- GET /incidents ; GET /incidents/{id} ; PATCH /incidents/{id} (role-checked)
- POST /incidents/{id}/analyse : queue AI explanation, no write privileges
- POST /incidents/{id}/approvals : request, then SEPARATE approval of simulation
- GET /stream : auth-protected SSE
- GET /healthz, /readyz

## Security controls
TLS, no secrets in Git, deny by default, re-check tenant+role on every read/write, hashed revocable agent credentials, size limits + rate limits, parameterised SQL, strict CORS, secure cookies, replay protection, audit everything, containment simulated only.

## AI boundaries
Untrusted logs; redact secrets; typed JSON output (summary, confidence, referenced_event_ids, proposed_next_steps); reject IDs not in the incident; AI cannot change status/permissions; deterministic fallback.

## Acceptance tests
P0: AT-01 compose start + /readyz; AT-02 401/403; AT-03 tenant isolation; AT-04 Windows+Linux agent builds + sends event; AT-05 data survives restart; AT-06 revoked agent denied; AT-07 5 positive alert, 5 benign don't; AT-08 lifecycle audited; AT-10 containment needs approval, denial changes nothing.
P1: AT-09 AI rejects bad IDs + fallback; AT-11 100 ev/s 5 min, >=99% accepted, p95 < 500ms; AT-12 backup/restore; AT-13 Playwright + dependency scan in CI.
