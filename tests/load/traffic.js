// Three independently scheduled one-request scenarios. All targets are loopback.
import { runEndpoint } from './scenarios.js';

const configRate = Number(__ENV.TRAFFIC_CONFIG || 5);
const userRate = Number(__ENV.TRAFFIC_USER || 5);
const eventRate = Number(__ENV.TRAFFIC_EVENT || 10);
const seconds = Number(__ENV.TRAFFIC_SECONDS || 20);
const expectedFailure = __ENV.TRAFFIC_EVENT_FAIL === '1';
for (const [rate, max] of [[configRate, 5000], [userRate, 5000], [eventRate, 10000]]) {
  if (!Number.isInteger(rate) || rate < 0 || rate > max) throw new Error('invalid bounded runner rate');
}
if (!Number.isInteger(seconds) || seconds < 5 || seconds > 60 || configRate + userRate + eventRate === 0) throw new Error('invalid traffic duration or rate');
const delay = Number(__ENV.TRAFFIC_EVENT_DELAY || 0);
if (![0, 1, 10, 50, 100].includes(delay)) throw new Error('invalid fixture delay');
const scenarios = {}, thresholds = {};
for (const [name, rate, fn, p95, p99] of [
  ['sdk_config', configRate, 'sdkConfig', 20, 50],
  ['user_check', userRate, 'userCheck', 100, 250],
  ['event_report', eventRate, 'eventReport', 500, 1500],
]) {
  if (!rate) continue;
  const initial = Math.min(500, Math.max(30, Math.ceil(rate * (name === 'event_report' ? (delay + 30) / 1000 : .04))));
  scenarios[name] = {
    executor: 'constant-arrival-rate', exec: fn, rate, timeUnit: '1s', duration: seconds + 's',
    preAllocatedVUs: initial, maxVUs: Math.min(750, Math.max(100, initial * 2)), gracefulStop: '10s',
  };
  const tag = '{scenario:' + name + '}';
  const expected = expectedFailure && name === 'event_report';
  // Planned delivery is independently enforced per endpoint (0.1% budget).
  thresholds['dropped_iterations' + tag] = ['count<=' + Math.floor(rate * seconds * .001)];
  thresholds['http_reqs' + tag] = ['count>=' + Math.floor(rate * seconds * .999)];
  thresholds['http_req_failed' + tag] = [expected ? 'rate>0.95' : 'rate<0.01'];
  thresholds['business_failed' + tag] = [expected ? 'rate>0.95' : 'rate<0.01'];
  if (!expected) thresholds['http_req_duration' + tag] = ['p(95)<' + p95, 'p(99)<' + p99];
  else thresholds['http_req_duration' + tag] = ['p(99)<1500'];
}
export const options = {scenarios, thresholds, summaryTrendStats: ['avg','med','max','p(95)','p(99)'], maxRedirects: 0};
export function sdkConfig() { runEndpoint('sdk-config'); }
export function userCheck() { runEndpoint('user-check'); }
export function eventReport() { runEndpoint('event-report'); }
