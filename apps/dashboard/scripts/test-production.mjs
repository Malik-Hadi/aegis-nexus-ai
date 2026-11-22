import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { createServer } from 'node:http'
import { setTimeout as delay } from 'node:timers/promises'

const profile = { subject: 'fixture', role: 'analyst', tenant_id: 'fixture' }
let stream
let eventRows = []
const fixture = createServer((req, res) => {
  if (req.url === '/v1/auth/login' && req.headers.origin === 'http://' + req.headers.host) {
    req.resume()
    res.writeHead(200, { 'Set-Cookie': 'session=fixture; HttpOnly; Path=/; SameSite=Lax', 'Content-Type': 'application/json' })
    res.end('{}')
    return
  }
  if (req.headers.cookie !== 'session=fixture') {
    res.writeHead(401, { 'Content-Type': 'application/json' })
    res.end('{}')
    return
  }
  if (req.url === '/v1/stream') {
    stream = res
    res.writeHead(200, { 'Content-Type': 'text/event-stream' })
    res.write(': connected\n\n')
    return
  }
  res.writeHead(200, { 'Content-Type': 'application/json' })
  const payload = {
    '/v1/agents': { agents: [] },
    '/v1/incidents': { incidents: [] },
    '/v1/events': { events: eventRows },
  }[req.url.split('?')[0]] ?? profile
  res.end(JSON.stringify(payload))
}).listen(0, '127.0.0.1')
await once(fixture, 'listening')
const portProbe = createServer().listen(0, '127.0.0.1')
await once(portProbe, 'listening')
const port = portProbe.address().port
await new Promise(resolve => portProbe.close(resolve))
const origin = 'http://127.0.0.1:' + port
const next = spawn(process.execPath, ['node_modules/next/dist/bin/next', 'start', '-p', String(port), '-H', '127.0.0.1'], {
  env: {
    ...process.env, NEXT_TELEMETRY_DISABLED: '1', SOC_PUBLIC_ORIGIN: origin,
    SOC_API_ORIGIN: 'http://127.0.0.1:' + fixture.address().port,
  },
  stdio: 'inherit',
  windowsHide: true,
})
const request = (path, init) => fetch(origin + path, { signal: AbortSignal.timeout(4000), ...init })

try {
  let page
  for (let attempt = 0; attempt < 50 && !page; attempt++) {
    page = await request('/').catch(() => null)
    if (!page) await delay(100)
  }
  assert.equal(page?.status, 200)
  const html = await page.text()
  assert.match(html, /AEGIS NEXUS AI/)
  const nonce = page.headers.get('content-security-policy')?.match(/'nonce-([^']+)'/)?.[1]
  assert.ok(nonce)
  const scripts = [...html.matchAll(/<script\b([^>]*)>/g)]
  assert.ok(scripts.some(([, attributes]) => !attributes.includes('src=')))
  for (const [, attributes] of scripts.filter(([, value]) => !value.includes('src='))) {
    assert.ok(attributes.includes('nonce="' + nonce + '"'), 'Inline Next.js script must match page nonce')
  }
  assert.equal((await request('/v1/me')).status, 401)
  assert.equal((await request('/v1/stream')).status, 401)
  assert.equal((await request('/v1/auth/login', { method: 'POST', headers: { origin: 'https://elsewhere.example' } })).status, 403)
  console.log('PASS production page, CSP nonce, unauthorized requests and origin check')

  const login = await request('/v1/auth/login', { method: 'POST', headers: { origin } })
  assert.equal(login.status, 200)
  const session = login.headers.getSetCookie()[0]
  assert.match(session, /HttpOnly/)
  const headers = { cookie: session.split(';')[0] }
  assert.deepEqual(await (await request('/v1/me', { headers })).json(), profile)
  console.log('PASS login and session forwarding through Next.js')

  const response = await request('/v1/stream', { headers })
  assert.equal(response.headers.get('x-accel-buffering'), 'no')
  assert.match(response.headers.get('content-type'), /text\/event-stream/)
  const reader = response.body.getReader()
  const decode = new TextDecoder()
  let frames = ''
  async function readUntil(pattern) {
    while (!pattern.test(frames)) {
      const part = await reader.read()
      assert.equal(part.done, false)
      frames += decode.decode(part.value, { stream: true })
    }
  }
  await readUntil(/: connected\n\n/)
  const emittedAt = performance.now()
  stream.write('data: {"type":"event.new","data":{"id":"synthetic"}}\n\n')
  await readUntil(/event\.new[^\n]*\n\n/)
  console.log('PASS live SSE frame through Next.js while stream stayed open (' + Math.round(performance.now() - emittedAt) + ' ms locally)')
  await reader.cancel()
  if (process.argv.includes('--browser')) {
    const { chromium } = await import('playwright')
    const browser = await chromium.launch({ channel: 'chromium', executablePath: process.env.SOC_TEST_CHROMIUM })
    try {
      const page = await browser.newPage()
      await page.goto(origin)
      await page.getByLabel('User', { exact: true }).fill('fixture')
      await page.getByLabel('Password', { exact: true }).fill('fixture')
      await page.getByRole('button', { name: 'Sign in', exact: true }).click()
      await page.getByText('Live', { exact: true }).waitFor({ timeout: 10000 })
      await page.getByRole('button', { name: 'Event feed', exact: true }).click()
      const payload = '<b>literal test telemetry</b>'
      eventRows = [{
        id: 'synthetic', agent_id: 'fixture-agent', kind: 'lab.fixture', severity: 'info',
        occurred_at: new Date().toISOString(), payload,
      }]
      stream.write('data: {"type":"event.new","data":{"id":"synthetic"}}\n\n')
      await page.getByText('lab.fixture', { exact: true }).waitFor({ timeout: 10000 })
      await page.getByText(payload, { exact: true }).waitFor({ timeout: 10000 })
      assert.equal(await page.locator('b').filter({ hasText: 'literal test telemetry' }).count(), 0)
      console.log('PASS Chromium login, live event feed and literal telemetry rendering')
    } finally {
      await browser.close()
    }
  }
} finally {
  next.kill()
  fixture.closeAllConnections()
  fixture.close()
}

