// Explicitly invoked from an independent load generator; never used by CI.
// Only test-environment fixtures; no order, payment, writes or admin APIs.
import http from 'k6/http';
import crypto from 'k6/crypto';
import exec from 'k6/execution';
import { SharedArray } from 'k6/data';
import { Rate } from 'k6/metrics';

const base = __ENV.LOAD_BASE_URL || '';
if (__ENV.LOAD_CONFIRM !== 'AUTHORIZED-ISOLATED-TEST') throw new Error('Explicit isolated-target confirmation required');
if (!/^https:\/\/[A-Za-z0-9.-]+(?::[0-9]{1,5})?$/.test(base)) throw new Error('Specify an HTTPS origin without path, credentials, query or fragment');
const qps = Number(__ENV.LOAD_TOTAL_QPS || 400), seconds = Number(__ENV.LOAD_SECONDS || 20);
if (!Number.isInteger(qps) || qps < 4 || qps > 10000 || qps % 4 || !Number.isInteger(seconds) || seconds < 10 || seconds > 60) throw new Error('Invalid bounded rate or duration');
if (!__ENV.LOAD_FIXTURES) throw new Error('Specify an external test fixture JSON file');
const apps = new SharedArray('isolated test channels', () => {
  const rows = JSON.parse(open(__ENV.LOAD_FIXTURES));
  if (!Array.isArray(rows) || !rows.length || rows.length > 4096) throw new Error('Require 1-4096 test channels');
  const ids = new Set();
  for (const row of rows) {
    if (!row || typeof row.appId !== 'string' || !/^[A-Za-z0-9_-]{1,128}$/.test(row.appId) ||
        typeof row.gameSecret !== 'string' || !row.gameSecret || typeof row.zoneKey !== 'string' || !row.zoneKey ||
        ids.has(row.appId)) throw new Error('Invalid or duplicate test fixture');
    ids.add(row.appId);
  }
  return rows;
});
const businessFailed = new Rate('business_failed');
const scenarios = {}, thresholds = {};
for (const [name, rate, fn, p95, p99] of [
  ['sdk_config', qps/4, 'sdkConfig', 20, 50],
  ['user_check', qps/4, 'userCheck', 100, 250],
  ['event_report', qps/2, 'eventReport', 500, 1500],
]) {
  const vus = Math.max(20, Math.ceil(rate * .1));
  scenarios[name] = { executor: 'constant-arrival-rate', exec: fn, rate, timeUnit: '1s', duration: seconds + 's', preAllocatedVUs: vus, maxVUs: Math.min(2000, vus*4), gracefulStop: '10s' };
  const tag = '{scenario:' + name + '}';
  thresholds['http_req_failed' + tag] = [{ threshold: 'rate<0.01', abortOnFail: true, delayAbortEval: '5s' }];
  thresholds['business_failed' + tag] = [{ threshold: 'rate<0.01', abortOnFail: true, delayAbortEval: '5s' }];
  thresholds['http_req_duration' + tag] = ['p(95)<' + p95, 'p(99)<' + p99];
  thresholds['dropped_iterations' + tag] = ['count<=' + Math.floor(rate*seconds*.001)];
  thresholds['http_reqs' + tag] = ['count>=' + Math.floor(rate*seconds*.999)];
}
export const options = { scenarios, thresholds, maxRedirects: 0, insecureSkipTLSVerify: false, summaryTrendStats: ['avg','med','max','p(95)','p(99)'] };

function current() { return apps[exec.scenario.iterationInTest % apps.length]; }
function signed(path, row, body) {
  const ts = String(Math.floor(Date.now()/1000));
  const nonce = Array.from(new Uint8Array(crypto.randomBytes(8)), b => b.toString(16).padStart(2,'0')).join('');
  return {
    'Content-Type':'application/json', 'x-app-id':row.appId, 'x-timestamp':ts, 'x-nonce':nonce,
    'x-signature':crypto.hmac('sha256', row.gameSecret, ['HITALK-HMAC-SHA256','POST',path,row.appId,ts,nonce,''].join('\n') + body, 'hex'),
  };
}
function result(res, predicate) {
  let success = false;
  try { success = res.status === 200 && predicate(res.json()); } catch (_) {}
  // Do not log request bodies, response bodies, credentials or signatures.
  businessFailed.add(!success);
}
export function sdkConfig() {
  const row = current(), path = '/api/core/config/test/' + row.appId;
  const res = http.get(base + path, { tags:{name:'sdk-config'}, timeout:'5s' });
  result(res, j => j.code === 0 && j.data.snKey === row.appId && j.data.zoneKey === row.zoneKey);
}
export function userCheck() {
  const row = current(), path = '/api/service/user/check/test';
  const body = JSON.stringify({open_id:'load-user-' + exec.scenario.iterationInTest});
  const res = http.post(base + path, body, { headers:signed(path,row,body), tags:{name:'user-check'}, timeout:'5s' });
  result(res, j => j.code === 0 && j.data === true);
}
export function eventReport() {
  const row = current(), path = '/api/service/events/test', i = exec.scenario.iterationInTest;
  const body = JSON.stringify({event:'login',ext:{roleId:'load-role-'+i,open_id:'load-user-'+i,server_id:row.zoneKey}});
  const res = http.post(base + path, body, { headers:signed(path,row,body), tags:{name:'event-report'}, timeout:'5s' });
  result(res, j => j.code === 0 && j.data === true);
}
