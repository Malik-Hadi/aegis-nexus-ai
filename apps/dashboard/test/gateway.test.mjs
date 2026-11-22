import assert from 'node:assert/strict'
import { once } from 'node:events'
import { createServer } from 'node:http'
import { test } from 'node:test'
import { forward } from '../lib/gateway.mjs'

const publicOrigin = 'http://localhost:3000'
const request = (path, options) => new Request(publicOrigin + '/v1/' + path, options)

test('dashboard gateway preserves the Go contract and streams a frame before upstream closes', async t => {
  let requests = 0
  const server = createServer(async (req, res) => {
    requests++
    if (req.url === '/v1/auth/login') {
      assert.equal(req.headers.origin, 'http://' + req.headers.host)
      const body = []
      for await (const chunk of req) body.push(chunk)
      assert.deepEqual(JSON.parse(Buffer.concat(body)), { subject: 'fixture' })
      res.setHeader('Content-Type', 'application/json')
      res.setHeader('Set-Cookie', ['session=fixture; HttpOnly; Path=/', 'second=fixture; Path=/'])
      res.end('{"status":"logged in"}')
    } else if (req.url === '/v1/stream') {
      assert.equal(req.headers.cookie, 'session=fixture')
      assert.equal(req.headers['last-event-id'], '17')
      res.writeHead(200, { 'Content-Type': 'text/event-stream' })
      res.write('data: {"type":"event.new"}\n\n')
    } else {
      assert.equal(req.url, '/v1/incidents?limit=100')
      res.writeHead(401, { 'Content-Type': 'application/json' })
      res.end('{"error":"unauthenticated"}')
    }
  }).listen(0, '127.0.0.1')
  t.after(() => { server.closeAllConnections(); server.close() })
  await once(server, 'listening')
  const env = { SOC_API_ORIGIN: 'http://127.0.0.1:' + server.address().port, SOC_PUBLIC_ORIGIN: publicOrigin }
  const send = (path, options) => forward(request(path, options), path.split('?')[0].split('/'), env)

  assert.equal((await send('auth/login', { method: 'POST', headers: { origin: 'https://elsewhere.example' } })).status, 403)
  assert.equal((await send('agents/enrol', { method: 'POST' })).status, 404)
  assert.equal(requests, 0)

  const login = await send('auth/login', {
    method: 'POST', headers: { origin: publicOrigin, 'content-type': 'application/json' },
    body: JSON.stringify({ subject: 'fixture' }),
  })
  assert.deepEqual(await login.json(), { status: 'logged in' })
  assert.equal(login.headers.getSetCookie().length, 2)
  assert.match(login.headers.getSetCookie()[0], /HttpOnly/i)
  assert.equal(login.headers.get('cache-control'), 'no-store')
  const unauthenticated = await send('incidents?limit=100')
  assert.equal(unauthenticated.status, 401)
  assert.equal((await unauthenticated.json()).error, 'unauthenticated')

  const stream = await send('stream', {
    signal: AbortSignal.timeout(2000),
    headers: { cookie: 'session=fixture', 'last-event-id': '17' },
  })
  assert.equal(stream.headers.get('content-type'), 'text/event-stream')
  assert.equal(stream.headers.get('x-accel-buffering'), 'no')
  const reader = stream.body.getReader()
  const chunk = await reader.read()
  assert.match(new TextDecoder().decode(chunk.value), /event\.new/)
  await reader.cancel()
})

