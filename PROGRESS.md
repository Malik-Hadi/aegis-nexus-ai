# Progress (paste this at the start of every new AI chat)

## Done

- Setup, Block 1 (login, roles), Block 2 (enrolment, events, tenant isolation, revocation).
- 15 Go tests pass. scripts/verify-backend.ps1 passes all checks.

## Next

- Block 3: Go agent (heartbeat, synthetic events, retry/backoff), Windows and Linux builds.

## Known limitations

- Logout only clears the cookie (sessions are not tracked server-side).
- No rate limiting yet (hardening phase).
