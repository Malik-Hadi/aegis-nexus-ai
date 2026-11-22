# Project progress

Updated 22 November 2026.

## Implemented

- Go lab API with seeded roles, tenant-scoped reads, agent enrolment and revocation, heartbeat, event ingestion, deterministic rules, incident lifecycle, approvals and SSE.
- Next.js TypeScript/Tailwind dashboard with one App Router entry point, same-origin gateway, live event feed and simulated response workflow.
- Incident explanations with deterministic offline mode, optional HTTPS adapter and evidence-ID validation.
- API loopback HTTP default, optional TLS, secure cookies under TLS, and bounded login and ingestion rate limits.
- Lab startup and verification scripts, API reference, runbook, threat model, browser smoke test and k6 load fixture.

## Evidence

Go unit tests and vet, both agent builds, the Windows agent replay, Next.js production build, HTTP streaming and browser smoke passed locally. A disposable PostgreSQL 16 lab passed core SOC integration checks, all five detector checks, restart persistence and physical restore. The five-minute batched load run sustained 100.03 events/s with 22.20 ms p95 latency and 30,010 persisted events. npm reported zero production dependency vulnerabilities at the time of the run.

## Release gates

Verify clean Docker Compose startup and Linux agent execution before MVP sign-off. Hosted CI, OWASP ZAP, logical dump/restore and a release tag are still pending. Incident analysis uses persistent PostgreSQL jobs and an in-process Go worker.
Release follow-up: startup now applies migration 006 on existing lab volumes. The full HTTPS dashboard path passed through a trusted loopback proxy. The optional adapter protocol passed against an HTTPS fixture. The intended staging deployment is still unverified.
