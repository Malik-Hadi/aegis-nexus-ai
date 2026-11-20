# 10-minute smoke test (tick each)
- [ ] go version / node -v / git --version / docker compose version all print versions
- [ ] Docker Desktop shows "running"
- [ ] Task "DB: start" -> Task "DB: status" shows db "healthy"
- [ ] Task "API: run" -> browser http://localhost:8080/healthz shows {"status":"ok"}
- [ ] SQLTools connects (localhost:5432, user/password from .env) and `SELECT name FROM tenants;` returns 2 rows
- [ ] Task "Go: test all" runs without crashing
- [ ] Task "Build agent: Linux" and "Build agent: Windows" create files in bin/
- [ ] Your AI assistant replies in VS Code
- [ ] git init, git add ., git commit -m "setup complete"
