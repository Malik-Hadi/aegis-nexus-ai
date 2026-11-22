# SOC dashboard

This TypeScript and Tailwind Next.js console runs as a separate service. Its same-origin gateway forwards requests and Server-Sent Events to the Go API.

## Local development

1. Start the database and Go API using the project runbook.
2. Keep the default origins in .env.example or copy it to .env.local and set SOC_API_ORIGIN and SOC_PUBLIC_ORIGIN.
3. Run npm.cmd ci, then npm.cmd run dev.
4. Open http://localhost:3000 and sign in with your lab account.

## Verification

Run npm.cmd test, npm.cmd run build, then node scripts/test-production.mjs. Append --browser to execute the Chromium login and live feed smoke test after installing the Playwright browser. SOC_TEST_CHROMIUM can point to a compatible installed Chromium executable.

To use the production build, run npm.cmd start. With Go and PostgreSQL running, use npm.cmd run smoke to check the live deployment. Full lab verification and the client demo are described in [the project runbook](../../docs/runbook.md).