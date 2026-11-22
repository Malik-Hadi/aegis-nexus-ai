# Lab threat model

| Boundary | Risk | Control and remaining work |
| --- | --- | --- |
| Endpoint to Go API | Unenrolled source, credential abuse or replay | Limited enrolment tokens, hashed agent credentials, revocation, event validation and scoped keys. Verify controls against the database and run the negative checks. |
| Browser to dashboard and Go API | Cross-site writes, script injection, cookie theft | Same-origin gateway, CSP nonce, React text rendering, HTTP-only cookies and backend checks. Keep the public origin exact and use trusted TLS outside loopback. |
| Tenant and role boundaries | An operator reads another tenant or decides an unauthorised action | Scope queries and the event hub by server-authenticated tenant. Require an eligible admin distinct from the requester. Verify cross-tenant read, write and stream cases on a clean lab. |
| Event storage and live feed | Loss on restart, duplicate inserts or excess traffic | Persist events in PostgreSQL, reject duplicate keys and throttle ingest. Run restart and restore tests and measure sustainable capacity. |
| Incident analysis | Leaked payloads, injected instructions or fabricated evidence | Exclude raw telemetry from the optional HTTPS adapter input, validate typed output and evidence IDs, and fall back offline. Analysis has no response or lifecycle permissions. |
| Response workflow | Real endpoint changes or unreviewed decision | Only record a simulated outcome after an eligible approval. The endpoint agent has no remote shell or response executor. |

Assumptions include lab-owned endpoints, dedicated test identities, private PostgreSQL access, trusted local operators and synthetic telemetry. Known gaps are tracked in limitations.md and the acceptance report.