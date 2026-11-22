# Client handover checklist

## 1. What the client receives
- Access to the GitHub repository (Settings > Collaborators > Add people) and release tag `v0.1.0-mvp`.
- `docs/ACCEPTANCE.md` (measured test results), `docs/limitations.md`, `docs/threat-model.md`, `docs/openapi.yaml`, `docs/runbook.md`.
- Lab account passwords, sent **privately** (never by chat history or commit). Generate fresh ones on the client's machine with the `seed` command.

## 2. Client setup (about 15 minutes)
1. Install Docker Desktop and Git.
2. `git clone` the repository and `git checkout v0.1.0-mvp`.
3. Copy `.env.example` to `.env`. Set their own `POSTGRES_PASSWORD`, matching `DATABASE_URL`, and a new `SESSION_SECRET` (32+ characters).
4. Run the one-command start from the README, then the `seed` command.
5. Open http://localhost:3000 and sign in.
6. Follow "Demonstrate the full flow" in the README.

## 3. Transferring control
- **Repository:** the client forks it, or you transfer it (Settings > Danger Zone > Transfer ownership). Remove yourself as collaborator afterwards.
- **Secrets:** the client creates their own `.env`. Nothing secret is in Git. Rotate all lab passwords on their machine.
- **Admin accounts:** after seeding, the client signs in as `admin-a` and owns user and agent management. Change the seeded subjects only through a new migration.
- **Agents:** the client enrols their own authorised test machines with one-time tokens and revokes any they no longer want.
- **Backups:** `pg_dump -Fc` and `pg_restore --exit-on-error`, as described in the runbook.
- **Support boundary:** write down what you support after handover and for how long.

## 4. Before you hand over (your checklist)
- [ ] `scripts\docker-full-test.ps1` ends with `RESULT: 0 failed` on your machine
- [ ] `scripts\verify-all.ps1` ends with `0 failed`
- [ ] A fresh clone in a new folder starts with the README commands only
- [ ] `git ls-files .env` prints nothing, and no secrets are in history
- [ ] `docs/ACCEPTANCE.md` and `docs/limitations.md` match what you actually measured (AT-01, AT-11 and AT-13 especially)
- [ ] Release tag points at the final commit and is pushed
