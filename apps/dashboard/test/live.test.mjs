import assert from 'node:assert/strict'
import { test } from 'node:test'
import { coalesce, liveResources } from '../lib/live.mjs'

test('updates start immediately and burst arrivals keep a trailing refresh', async () => {
  let calls = 0
  let release
  const pending = new Promise(resolve => { release = resolve })
  const load = coalesce(async () => {
    calls++
    if (calls === 1) await pending
  })
  const first = load()
  assert.equal(calls, 1)
  await Promise.all([load(), load(), load()])
  assert.equal(calls, 1)
  release()
  await first
  assert.equal(calls, 2)
  await load()
  assert.equal(calls, 3)
})

test('known messages invalidate the appropriate views', () => {
  for (const [event, resources] of [
    ['event.new', ['events']],
    ['incident.created', ['incidents']],
    ['incident.updated', ['incidents', 'detail']],
    ['agent.heartbeat', ['agents']],
    ['agent.revoked', ['agents']],
    ['approval.requested', ['detail']],
    ['approval.decided', ['detail']],
    ['unknown', []],
  ]) assert.deepEqual(liveResources(event), resources, event)
})

