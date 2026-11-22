import assert from 'node:assert/strict'

const base = process.argv[2] || 'http://localhost:3000'
const origin = new URL(base).origin
const request = (path, init) => fetch(origin + path, { signal: AbortSignal.timeout(10000), ...init })

const page = await request('/')
assert.equal(page.status, 200)
assert.match(await page.text(), /AEGIS NEXUS AI/i)
assert.match(page.headers.get('content-security-policy') ?? '', /nonce-/)
assert.equal(page.headers.get('x-frame-options'), 'DENY')
console.log('PASS Next.js page and security headers')

const anonymous = await request('/v1/me')
assert.equal(anonymous.status, 401)
assert.equal(anonymous.headers.get('cache-control'), 'no-store')
assert.equal((await request('/v1/stream')).status, 401)
console.log('PASS authentication required for profile and live stream')

assert.equal((await request('/v1/auth/login', {
  method: 'POST', headers: { origin: 'https://elsewhere.example' },
})).status, 403)
console.log('PASS foreign Origin rejected')

