// Focused controls: unsigned local HTTP generator, signed admin nonce cache,
// signed subscriber nonce cache. No production traffic, one request per iteration.
import http from 'k6/http';
import crypto from 'k6/crypto';
import { Counter } from 'k6/metrics';

const kind = __ENV.DIAG_CASE || '';
const targets = {
  mock: { origin: 'http://127.0.0.1:8290', path: '/load/bench/ping', mode: '', status: 204 },
  health: { origin: 'http://127.0.0.1:8201', path: '/health/live', mode: '', status: 200 },
  'admin-core': { origin: 'http://127.0.0.1:8211', path: '/api/core/admin/config-schema/game', mode: 'admin', status: 200 },
  'admin-config': { origin: 'http://127.0.0.1:8201', path: '/api/config/admin/config-schema/game', mode: 'admin', status: 200 },
  'subscriber-access': { origin: 'http://127.0.0.1:8081', path: '/api/access/revision', mode: 'subscriber', status: 200 },
  'subscriber-sync': { origin: 'http://127.0.0.1:8201', path: '/api/config/sync/revision', mode: 'subscriber', status: 200 },
};
const target = targets[kind];
if (!target) throw new Error('Invalid DIAG_CASE');
const rate = Number(__ENV.DIAG_RATE || 3000);
const seconds = Number(__ENV.DIAG_SECONDS || 5);
const vus = Number(__ENV.DIAG_VUS || 300);
if (!Number.isInteger(rate) || rate < 100 || rate > 20000 ||
    !Number.isInteger(seconds) || seconds < 2 || seconds > 10 ||
    !Number.isInteger(vus) || vus < 10 || vus > 800) {
  throw new Error('Diagnostics workload is outside bounded runner limits');
}
const ok = new Counter('diag_success_2xx');
const conflict = new Counter('diag_http_409');
const other = new Counter('diag_unexpected_http');
export const options = {
  scenarios: { probe: { executor: 'constant-arrival-rate', rate,
    timeUnit: '1s', duration: seconds + 's', preAllocatedVUs: vus, gracefulStop: '5s' } },
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
  // In diagnostics we must count the expected replay-cache 409, not hide it.
  noConnectionReuse: false,
};
export default function () {
  const headers = {};
  if (target.mode) {
    const bytes = new Uint8Array(crypto.randomBytes(8));
    const nonce = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
    const ts = String(Math.floor(Date.now() / 1000));
    headers['x-nonce'] = nonce;
    headers['x-timestamp'] = ts;
    const sub = target.mode === 'subscriber';
    if (sub) headers['x-subscriber-code'] = 'load-subscriber';
    const prefix = 'HITALK-HMAC-SHA256\n' + (sub ? 'SUBSCRIBER\nload-subscriber\n' : '');
    headers['x-signature'] = crypto.hmac('sha256',
      sub ? 'runner-load-subscriber-only-0123456789' : 'runner-load-admin-only-0123456789abcdef',
      prefix + ['GET', target.path, '', ts, nonce, ''].join('\n'), 'hex');
  }
  const res = http.get(target.origin + target.path, {
    headers, timeout: '5s',
    responseCallback: http.expectedStatuses(target.status, 409),
    tags: { name: 'diag-' + kind },
  });
  if (res.status === target.status) ok.add(1);
  else if (res.status === 409) conflict.add(1);
  else other.add(1);
}
