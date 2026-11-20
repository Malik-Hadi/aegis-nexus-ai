# AEGIS NEXUS AI (lab MVP)
Defensive SOC prototype. Lab/synthetic data only. See docs/spec.md.

## Run
1. `cp .env.example .env` (already done by setup pack) and change the passwords.
2. Start Docker Desktop, then: `docker compose -f deploy/docker-compose.yml --env-file .env up -d`
3. API: `go run ./services/api` then open http://localhost:8080/healthz

Or use VS Code: Ctrl+Shift+P -> "Tasks: Run Task".
