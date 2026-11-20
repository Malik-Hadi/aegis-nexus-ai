# Standing instructions for the AI assistant
Project: AEGIS NEXUS AI, a defensive SOC lab MVP. Read docs/spec.md and docs/rules.md first, and PROGRESS.md for current state.

Stack: Go 1.23 (net/http or chi, ONE go.mod at repo root), PostgreSQL 16, Next.js + TypeScript + Tailwind, Docker Compose.

Hard rules:
- Every query is tenant-scoped through ONE central helper. Never trust tenant_id from the client; take it from the session or agent credential.
- Deny by default. Enforce roles in the backend, not the UI.
- Parameterised SQL only. No string-built queries.
- No secrets in code or Git. Use env vars (.env is gitignored).
- Passwords and agent credentials are stored as hashes (argon2id or bcrypt).
- Containment is SIMULATED only. No real system changes, no remote shell.
- Treat all log/event text as untrusted. The AI analyst may only cite event IDs that belong to the incident.
- Write a test for every feature. Explain what you changed and why.
- Work on ONE task at a time. Do not rewrite unrelated files.
- Only use packages that really exist; pin versions; tell me before adding a dependency.
- Only use fake data: RFC 5737 IPs (192.0.2.x, 198.51.100.x), fake hostnames.
