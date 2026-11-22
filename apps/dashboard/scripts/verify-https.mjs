import assert from 'node:assert/strict'

const base = new URL(process.argv[2] || 'https://localhost')
assert.equal(base.protocol, 'https:', 'Provide an authorised dashboard HTTPS origin')
assert.equal(base.href, base.origin + '/', 'Use the dashboard origin without a path or credentials')
assert.notEqual(process.env.NODE_TLS_REJECT_UNAUTHORIZED, '0', 'Certificate validation must be enabled')
const request = (path, options = {}) => fetch(new URL(path, base), {
  ...options, redirect: 'error', signal: AbortSignal.timeout(10000),
})

const home = await request('/')
assert.equal(home.status, 200, 'Dashboard must respond over trusted HTTPS')
assert.equal(home.headers.get('x-content-type-options'), 'nosniff')
assert.match(home.headers.get('content-security-policy') || '', /frame-ancestors 'none'/)
await home.body?.cancel()
assert.equal((await request('/v1/me')).status, 401)
assert.equal((await request('/v1/stream')).status, 401)
console.log('PASS trusted HTTPS, dashboard headers, and anonymous profile and stream rejection')

const { SOC_LAB_SUBJECT, SOC_LAB_PASSWORD, SOC_LAB_TENANT } = process.env
if (![SOC_LAB_SUBJECT, SOC_LAB_PASSWORD, SOC_LAB_TENANT].every(Boolean)) {
  console.log('Authenticated checks skipped. Set SOC_LAB_SUBJECT, SOC_LAB_PASSWORD and SOC_LAB_TENANT for an authorised test account.')
  process.exitCode = 2
} else {
  const login = await request('/v1/auth/login', {
    method: 'POST',
    headers: { Origin: base.origin, 'Content-Type': 'application/json' },
    body: JSON.stringify({ subject: SOC_LAB_SUBJECT, tenant_id: SOC_LAB_TENANT, password: SOC_LAB_PASSWORD }),
  })
  assert.equal(login.status, 200, 'Lab account sign-in failed')
  const session = login.headers.getSetCookie().find(cookie => cookie.startsWith('aegis_session='))
  assert.ok(session, 'Session cookie is missing')
  assert.match(session, /;\s*Secure(?:;|$)/i)
  assert.match(session, /;\s*HttpOnly(?:;|$)/i)
  const headers = { cookie: session.split(';', 1)[0] }
  try {
    const profile = await request('/v1/me', { headers })
    assert.equal(profile.status, 200)
    assert.equal((await profile.json()).tenant_id, SOC_LAB_TENANT)
    const stream = await request('/v1/stream', { headers })
    assert.equal(stream.status, 200)
    assert.match(stream.headers.get('content-type') || '', /text\/event-stream/i)
    const reader = stream.body.getReader()
    try {
      const frame = await reader.read()
      assert.ok(!frame.done && frame.value?.length, 'SSE response did not deliver an initial frame')
    } finally { await reader.cancel() }
    console.log('PASS Secure and HttpOnly cookie, tenant session and live frame over HTTPS')
  } finally {
    await request('/v1/auth/logout', { method: 'POST', headers: { ...headers, Origin: base.origin } })
  }
}