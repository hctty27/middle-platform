import http from 'k6/http';
import crypto from 'k6/crypto';
import exec from 'k6/execution';
import { check } from 'k6';
import { Rate, Counter } from 'k6/metrics';

const target = __ENV.LOAD_TARGET || 'nginx';
const base = target === 'direct' ? 'http://127.0.0.1:8211' : 'http://127.0.0.1:8280';
const app = 'load-app';
const gameSecret = 'runner-load-game-only';
// Multi-channel mode is restricted to high-frequency endpoints and synthetic fixtures.
const fixtureCount = Number(__ENV.TRAFFIC_APPS || 0);
if (![0, 16, 256].includes(fixtureCount)) throw new Error('invalid fixture app count');
function fixture(index) {
  if (!fixtureCount) return { app, gameSecret, zone: 'load-group' };
  const n = index % fixtureCount;
  const game = n % 16;
  return { app: 'load-app-' + n, gameSecret: 'runner-load-game-only-' + game, zone: 'load-group-' + game };
}
const platformSecret = 'runner-load-platform-only';
const subscriber = 'load-subscriber';
const subscriberSecret = 'runner-load-subscriber-only-0123456789';
const scenario = __ENV.LOAD_SCENARIO || 'mixed';
const rate = Number(__ENV.LOAD_RATE || 5);
const duration = __ENV.LOAD_DURATION || '10s';
const capacity = __ENV.LOAD_PROFILE === 'capacity';
const soak = ['soak', 'soak2h'].includes(__ENV.LOAD_PROFILE);
const dropBudget = soak ? Math.floor(rate * (duration === '2h' ? 7200 : 1800) * 0.001) : 0;
const failed409 = new Counter('failed_http_409');
const failed401 = new Counter('failed_http_401');
const failed503 = new Counter('failed_http_503');
const failedOther = new Counter('failed_http_other');
let loggedFailure = false;
const runID = __ENV.LOAD_RUN_ID || 'local';
const businessFailed = new Rate('business_failed');
const cases = ['config-sync', 'sdk-config', 'user-check', 'order', 'duplicate', 'payment'];
if (scenario !== 'mixed' && scenario !== 'event-report' && !cases.includes(scenario)) throw new Error('unknown scenario');
if (!['nginx', 'direct'].includes(target) || (target === 'direct' && (!capacity || scenario !== 'sdk-config'))) throw new Error('invalid local target');
if (!Number.isInteger(rate) || rate < 1 || rate > (scenario === 'sdk-config' ? (capacity ? 10000 : 5000) : 1000)) throw new Error('invalid local rate');

export const options = {
  scenarios: {
    load: {
      executor: 'constant-arrival-rate', rate, timeUnit: '1s', duration,
      preAllocatedVUs: capacity ? Math.min(500, Math.max(100, Math.ceil(rate / 20))) : soak ? 50 : Math.min(250, Math.max(5, Math.ceil(rate / 5))),
      maxVUs: capacity ? Math.min(750, Math.max(200, Math.ceil(rate / 10))) : soak ? 100 : Math.min(500, Math.max(20, Math.ceil(rate / 2))),
      gracefulStop: '10s',
    },
  },
  thresholds: {
    http_req_failed: [{ threshold: 'rate<0.01', abortOnFail: true, delayAbortEval: '5s' }],
    business_failed: [{ threshold: 'rate<0.01', abortOnFail: true, delayAbortEval: '5s' }],
    http_req_duration: ['p(95)<500', 'p(99)<1500'],
    dropped_iterations: ['count<=' + dropBudget],
  },
  summaryTrendStats: ['avg', 'min', 'med', 'max', 'p(95)', 'p(99)'],
  maxRedirects: 0,
};

function stamp() {
  return { ts: String(Math.floor(Date.now() / 1000)), nonce: Array.from(new Uint8Array(crypto.randomBytes(8)), b => b.toString(16).padStart(2, '0')).join('') };
}
function gameHeaders(path, body, selectedApp = app, selectedSecret = gameSecret) {
  const { ts, nonce } = stamp();
  return {
    'Content-Type': 'application/json', 'x-app-id': selectedApp, 'x-timestamp': ts, 'x-nonce': nonce,
    'x-signature': crypto.hmac('sha256', selectedSecret,
      ['HITALK-HMAC-SHA256', 'POST', path, selectedApp, ts, nonce, ''].join('\n') + body, 'hex'),
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
  if (!ok) {
    const counter = res.status === 409 ? failed409 : res.status === 401 ? failed401 : res.status === 503 ? failed503 : failedOther;
    counter.add(1);
    // First failed response per VU, including failures late in a stage.
    if (!loggedFailure) {
      console.error(name + ': status=' + res.status + ' body=' + String(res.body).slice(0, 160));
      loggedFailure = true;
    }
  }
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
  const selected = scenario === 'mixed' ? cases[exec.scenario.iterationInTest % cases.length] : scenario;
  runEndpoint(selected);
}

export function runEndpoint(selected) {
  const index = exec.scenario.iterationInTest;
  const id = runID + '-' + index;
  const selectedFixture = fixture(index);
  let path, body, res;
  switch (selected) {
    case 'config-sync':
      path = '/api/config/sync/revision';
      res = http.get(base + path, { headers: syncHeaders(path), tags: { name: selected }, timeout: '5s' });
      record(res, selected, jsonOK(res, j => typeof j.revision === 'number' && !!j.epoch));
      break;
    case 'sdk-config':
      path = '/api/core/config/test/' + selectedFixture.app;
      res = http.get(base + path, { tags: { name: selected }, timeout: '5s' });
      record(res, selected, jsonOK(res, j => j.code === 0 && j.data.snKey === selectedFixture.app && j.data.zoneKey === selectedFixture.zone));
      break;
    case 'user-check':
      path = '/api/service/user/check/test';
      body = JSON.stringify({ open_id: 'load-user-' + index });
      res = http.post(base + path, body, { headers: gameHeaders(path, body, selectedFixture.app, selectedFixture.gameSecret), tags: { name: selected }, timeout: '5s' });
      record(res, selected, jsonOK(res, j => j.code === 0 && j.data === true));
      break;
    case 'event-report':
      path = '/api/service/events/test';
      body = JSON.stringify({ event: 'login', ext: { roleId: 'load-role-' + index, open_id: 'load-user-' + index, server_id: selectedFixture.zone } });
      res = http.post(base + path, body, { headers: gameHeaders(path, body, selectedFixture.app, selectedFixture.gameSecret), tags: { name: selected }, timeout: '5s' });
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
