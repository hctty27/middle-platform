import http from 'k6/http';
import crypto from 'k6/crypto';
import exec from 'k6/execution';
import { check } from 'k6';
import { Rate } from 'k6/metrics';

const base = 'http://127.0.0.1:8280';
const app = 'load-app';
const gameSecret = 'runner-load-game-only';
const platformSecret = 'runner-load-platform-only';
const subscriber = 'load-subscriber';
const subscriberSecret = 'runner-load-subscriber-only-0123456789';
const scenario = __ENV.LOAD_SCENARIO || 'mixed';
const rate = Number(__ENV.LOAD_RATE || 5);
const duration = __ENV.LOAD_DURATION || '10s';
const runID = __ENV.LOAD_RUN_ID || 'local';
const businessFailed = new Rate('business_failed');
const cases = ['config-sync', 'sdk-config', 'user-check', 'order', 'duplicate', 'payment'];
if (scenario !== 'mixed' && !cases.includes(scenario)) throw new Error('unknown scenario');
if (!Number.isInteger(rate) || rate < 1 || rate > 1000) throw new Error('invalid local rate');

export const options = {
  scenarios: {
    load: {
      executor: 'constant-arrival-rate', rate, timeUnit: '1s', duration,
      preAllocatedVUs: Math.min(200, Math.max(5, Math.ceil(rate / 5))),
      maxVUs: Math.min(400, Math.max(20, Math.ceil(rate / 2))),
      gracefulStop: '10s',
    },
  },
  thresholds: {
    http_req_failed: [{ threshold: 'rate<0.01', abortOnFail: true, delayAbortEval: '5s' }],
    business_failed: [{ threshold: 'rate<0.01', abortOnFail: true, delayAbortEval: '5s' }],
    http_req_duration: ['p(95)<500', 'p(99)<1500'],
    dropped_iterations: ['count==0'],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(95)', 'p(99)'],
  maxRedirects: 0,
};

function stamp() {
  return { ts: String(Math.floor(Date.now() / 1000)), nonce: Array.from(new Uint8Array(crypto.randomBytes(8)), b => b.toString(16).padStart(2, '0')).join('') };
}
function gameHeaders(path, body) {
  const { ts, nonce } = stamp();
  return {
    'Content-Type': 'application/json', 'x-app-id': app, 'x-timestamp': ts, 'x-nonce': nonce,
    'x-signature': crypto.hmac('sha256', gameSecret,
      ['HITALK-HMAC-SHA256', 'POST', path, app, ts, nonce, ''].join('\n') + body, 'hex'),
  };
}
function syncHeaders(path) {
  const { ts, nonce } = stamp();
  return {
    'x-subscriber-code': subscriber, 'x-timestamp': ts, 'x-nonce': nonce,
    'x-signature': crypto.hmac('sha256', subscriberSecret,
      ['HITALK-HMAC-SHA256', 'SUBSCRIBER', subscriber, 'GET', path, '', ts, nonce, ''].join('\n'), 'hex'),
  };
}
function jsonOK(res, predicate) {
  try { return res.status === 200 && predicate(res.json()); } catch (_) { return false; }
}
function record(res, name, ok) {
  check(res, { [name + ' success']: () => ok });
  businessFailed.add(!ok, { endpoint: name });
  if (!ok && exec.scenario.iterationInTest < 3) console.error(name + ': status=' + res.status + ' body=' + String(res.body).slice(0, 300));
  return ok;
}
function register(id) {
  const path = '/api/core/orders/test/' + app;
  const body = JSON.stringify({ game_order_id: id, amount: 100, attach: 'load' });
  const res = http.post(base + path, body, { headers: gameHeaders(path, body), tags: { name: 'order' }, timeout: '5s' });
  return record(res, 'order', jsonOK(res, j => j.code === 0 && j.data.game_order_id === id));
}
function pay(id) {
  const path = '/api/core/pay/notify/' + app;
  const { ts, nonce } = stamp();
  const orderNo = 'p-' + id;
  const body = JSON.stringify({ order_no: orderNo, game_order_id: id, amount: 100, attach: 'load', paid_at: new Date().toISOString() });
  const sig = crypto.sha1([orderNo, id, platformSecret, nonce, ts].sort().join('_'), 'hex');
  const res = http.post(base + path, body, { headers: { 'Content-Type': 'application/json', 'x-timestamp': ts, 'x-nonce': nonce, 'x-signature': sig }, tags: { name: 'payment' }, timeout: '5s' });
  record(res, 'payment', res.status === 200 && res.body === nonce);
}
export default function () {
  const index = exec.scenario.iterationInTest;
  const selected = scenario === 'mixed' ? cases[index % cases.length] : scenario;
  const id = runID + '-' + index;
  let path, body, res;
  switch (selected) {
    case 'config-sync':
      path = '/api/config/sync/revision';
      res = http.get(base + path, { headers: syncHeaders(path), tags: { name: selected }, timeout: '5s' });
      record(res, selected, jsonOK(res, j => typeof j.revision === 'number' && !!j.epoch));
      break;
    case 'sdk-config':
      path = '/api/core/config/test/' + app;
      res = http.get(base + path, { tags: { name: selected }, timeout: '5s' });
      record(res, selected, jsonOK(res, j => j.code === 0 && j.data.snKey === app && j.data.zoneKey === 'load-group'));
      break;
    case 'user-check':
      path = '/api/service/user/check/test';
      body = JSON.stringify({ open_id: 'load-user-' + index });
      res = http.post(base + path, body, { headers: gameHeaders(path, body), tags: { name: selected }, timeout: '5s' });
      record(res, selected, jsonOK(res, j => j.code === 0 && j.data === true));
      break;
    case 'order':
      register(id);
      break;
    case 'duplicate':
      if (register(id)) register(id);
      break;
    case 'payment':
      if (register(id)) pay(id);
      break;
  }
}
