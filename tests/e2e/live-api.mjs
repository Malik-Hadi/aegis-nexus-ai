import assert from 'node:assert/strict'
import { mkdtemp, readFile, unlink, rmdir } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { execFileSync } from 'node:child_process'
import { setTimeout as delay } from 'node:timers/promises'

const base = process.env.SOC_LAB_API
const tenantA = '11111111-1111-1111-1111-111111111111'
const tenantB = '22222222-2222-2222-2222-222222222222'
assert.equal(new URL(base).hostname, '127.0.0.1')
assert.equal(new URL(base).protocol, 'http:')

async function call(method, path, expected, body, headers = {}) {
  const res = await fetch(base + path, {
    method, headers: { ...headers, ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(10000),
  })
  assert.equal(res.status, expected, method + ' ' + path)
  const data = await res.json()
  return { data, cookie: res.headers.getSetCookie()[0]?.split(';')[0] }
}

async function login(subject, password, tenant_id) {
  assert.ok(password, 'seed password unavailable for ' + subject)
  return { cookie: (await call('POST', '/v1/auth/login', 200, { subject, tenant_id, password })).cookie }
}

await call('GET', '/readyz', 200)
await call('GET', '/v1/me', 401)
await call('GET', '/v1/stream', 401)
const admin = await login('admin-a', process.env.LAB_ADMIN_A, tenantA)
const analyst = await login('analyst-a', process.env.LAB_ANALYST_A, tenantA)
const other = await login('admin-b', process.env.LAB_ADMIN_B, tenantB)
await call('POST', '/v1/admin/enrolment-tokens', 403, undefined, analyst)
console.log('PASS readiness, anonymous 401 and analyst 403')

async function enrol(hostname) {
  const { data: token } = await call('POST', '/v1/admin/enrolment-tokens', 201, undefined, admin)
  const { data } = await call('POST', '/v1/agents/enrol', 201, { token: token.enrolment_token, hostname, os: 'windows' })
  return data
}

const agent = await enrol('acceptance-windows')
const agentHeaders = { Authorization: 'Bearer ' + agent.credential }
await call('POST', '/v1/agents/heartbeat', 200, {}, agentHeaders)
async function fixtureFrom(name) {
  const fixture = JSON.parse(await readFile('simulations/' + name + '.json', 'utf8'))
  const latest = Math.max(...fixture.events.map(event => Date.parse(event.occurred_at)))
  const shift = Date.now() - latest
  for (const event of fixture.events) event.occurred_at = new Date(Date.parse(event.occurred_at) + shift).toISOString()
  return fixture
}

const fixture = await fixtureFrom('det001_positive')
const sample = fixture.events[0]
const streamResponse = await fetch(base + '/v1/stream', { headers: analyst, signal: AbortSignal.timeout(15000) })
assert.equal(streamResponse.status, 200)
const stream = streamResponse.body.getReader()
for (const event of fixture.events) await call('POST', '/v1/events', 202, event, agentHeaders)
const decoder = new TextDecoder()
let seen = ''
while (!seen.includes('incident.created')) {
  const part = await stream.read()
  assert.equal(part.done, false)
  seen += decoder.decode(part.value, { stream: true })
}
await stream.cancel()
await call('POST', '/v1/events', 202, sample, agentHeaders)
const { data: listed } = await call('GET', '/v1/incidents', 200, undefined, analyst)
const incident = listed.incidents.find(item => item.rule_id === 'DET-001')
assert.ok(incident)
const path = '/v1/incidents/' + incident.id
const { data: detail } = await call('GET', path, 200, undefined, analyst)
assert.ok(detail.events.length >= 6)
await call('GET', path, 404, undefined, other)
await call('PATCH', path, 404, { status: 'investigating' }, other)
await call('POST', path + '/analyse', 404, undefined, other)
const { data: events } = await call('GET', '/v1/events', 200, undefined, other)
assert.equal(events.events.length, 0)
console.log('PASS enrolment, heartbeat, synthetic detection and cross-tenant data denial')

const { data: job } = await call('POST', path + '/analyse', 202, undefined, analyst)
const jobPath = path + '/analysis/' + job.job_id
await call('GET', jobPath, 404, undefined, other)
let analysis
for (let i = 0; i < 50 && !analysis; i++) {
  const { data: result } = await call('GET', jobPath, 200, undefined, analyst)
  assert.notEqual(result.status, 'failed')
  if (result.status === 'completed') analysis = result.result
  else await delay(200)
}
assert.ok(analysis, 'queued analysis timed out')
assert.equal(analysis.mode, 'offline')
const validIds = new Set(detail.events.map(event => event.id))
assert.ok(analysis.referenced_event_ids.length)
assert.ok(analysis.referenced_event_ids.every(id => validIds.has(id)))
await call('PATCH', path, 200, { status: 'investigating', owner: 'me' }, analyst)
const { data: pending } = await call('POST', path + '/approvals', 201, { action: 'isolate_host' }, analyst)
assert.equal(pending.decision, 'pending')
const decision = '/v1/approvals/' + pending.approval_id + '/decision'
await call('POST', decision, 403, { decision: 'approved' }, analyst)
await call('POST', decision, 200, { decision: 'approved' }, admin)
const { data: decided } = await call('GET', path, 200, undefined, analyst)
assert.ok(decided.approvals[0].simulation_note)
assert.ok(decided.audit_trail.some(item => item.actor === 'analyst-a'))
const { data: deniedRequest } = await call('POST', path + '/approvals', 201, { action: 'block_ip' }, analyst)
await call('POST', '/v1/approvals/' + deniedRequest.approval_id + '/decision', 200, { decision: 'denied' }, admin)
const { data: deniedDetail } = await call('GET', path, 200, undefined, analyst)
const denied = deniedDetail.approvals.find(item => item.id === deniedRequest.approval_id)
assert.equal(denied.decision, 'denied')
assert.ok(!denied.simulation_note)
await call('POST', '/v1/agents/' + agent.agent_id + '/revoke', 200, undefined, admin)
await call('POST', '/v1/events', 401, sample, agentHeaders)
console.log('PASS grounded offline analysis, triage, distinct admin decision, audit and revoked-agent rejection')

const fresh = await enrol('acceptance-rule-fixtures')
const freshHeaders = { Authorization: 'Bearer ' + fresh.credential }
async function replay(file) {
  const input = await fixtureFrom(file)
  for (const event of input.events) await call('POST', '/v1/events', 202, event, freshHeaders)
}
for (const name of ['det001_benign_five', 'det002_benign', 'det003_benign', 'det004_benign']) await replay(name)
const { data: afterBenign } = await call('GET', '/v1/incidents', 200, undefined, analyst)
assert.equal(afterBenign.incidents.length, 1)
for (const name of ['det002_positive', 'det003_positive', 'det004_positive']) await replay(name)
const { data: afterPositive } = await call('GET', '/v1/incidents', 200, undefined, analyst)
assert.deepEqual(new Set(afterPositive.incidents.map(i => i.rule_id)), new Set(['DET-001', 'DET-002', 'DET-003', 'DET-004']))
console.log('PASS four positive and four benign detector fixtures against PostgreSQL')

if (process.env.SOC_LAB_AGENT_BIN) {
  const { data: token } = await call('POST', '/v1/admin/enrolment-tokens', 201, undefined, admin)
  const temp = await mkdtemp(join(tmpdir(), 'aegis-agent-'))
  const state = join(temp, 'state.json')
  const queue = join(temp, 'queue.jsonl')
  const host = 'acceptance-' + process.platform
  try {
    execFileSync(process.env.SOC_LAB_AGENT_BIN,
      ['enrol', '-api', base, '-token', token.enrolment_token, '-hostname', host, '-state', state],
      { windowsHide: true })
    execFileSync(process.env.SOC_LAB_AGENT_BIN,
      ['run', '-once', '-state', state, '-queue', queue, '-replay', 'simulations/det002_benign.json'],
      { windowsHide: true })
    const { data: agents } = await call('GET', '/v1/agents', 200, undefined, admin)
    assert.ok(agents.agents.some(agent => agent.hostname === host && agent.last_seen_at))
    console.log('PASS agent binary enrolled and replayed a benign fixture on ' + process.platform)
  } finally {
    for (const file of [state, queue]) await unlink(file).catch(err => { if (err.code !== 'ENOENT') throw err })
    await rmdir(temp)
  }
}