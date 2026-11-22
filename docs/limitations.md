# Known limitations (keep updated)
- Lab prototype, not production ready. Not independently pen-tested.
- Containment is simulated only.
- Fill in as you build.

- Incident explanation defaults to a deterministic offline template. The optional HTTPS adapter accepts only incident-scoped rule and event IDs. Analysis jobs persist in PostgreSQL. The worker runs in the Go process, and the dashboard polls only while waiting for a requested analysis.
- Rate limits apply per connection IP to login and event ingest, with in-memory counters on a single API process. A reverse proxy shares a login budget across its clients.
- A loopback HTTP mode exists for local development. Validate TLS and certificate trust on the target lab environment before a client demo.
- Record database-backed acceptance, browser automation, load, backup and restore results for each release.
