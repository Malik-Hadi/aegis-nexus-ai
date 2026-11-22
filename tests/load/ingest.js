import http from 'k6/http';
import { Rate } from 'k6/metrics';
import exec from 'k6/execution';

const accepted = new Rate('durable_acceptance');
const origin = __ENV.SOC_LOAD_ORIGIN || 'http://127.0.0.1:8080';
const token = __ENV.SOC_LOAD_AGENT_TOKEN;
const run = __ENV.SOC_LOAD_RUN_ID;

if (!/^https?:\/\/(localhost|127\.0\.0\.1)(:[0-9]+)?$/.test(origin)) {
  throw new Error('The load test is restricted to a disposable loopback lab');
}
if (!token || !run || !/^[a-zA-Z0-9_-]{1,32}$/.test(run)) {
  throw new Error('Set SOC_LOAD_AGENT_TOKEN and a fresh SOC_LOAD_RUN_ID');
}

const batchSize = 10;

export const options = {
  batchPerHost: batchSize,
  discardResponseBodies: false,
  scenarios: {
    ingest: {
      executor: 'constant-arrival-rate',
      rate: 10,
      timeUnit: '1s',
      duration: '5m',
      preAllocatedVUs: 20,
      maxVUs: 80,
    },
  },
  thresholds: {
    durable_acceptance: ['rate>=0.99'],
    http_req_duration: ['p(95)<500'],
    dropped_iterations: ['count==0'],
  },
};

export default function () {
  const prefix = 'k6-' + run + '-' + exec.scenario.iterationInTest + '-';
  const now = new Date().toISOString();
  const requests = Array.from({ length: batchSize }, (_, index) => ({
    method: 'POST',
    url: origin + '/v1/events',
    body: JSON.stringify({
      kind: 'lab.benchmark',
      occurred_at: now,
      severity: 'info',
      payload: { synthetic: true },
      source_ref: 'k6-' + run,
      idempotency_key: prefix + index,
    }),
    params: {
      headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' },
      redirects: 0,
    },
  }));
  for (const response of http.batch(requests)) {
    accepted.add(response.status === 202 && response.json('status') === 'accepted');
  }
}