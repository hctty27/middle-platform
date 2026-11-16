import http from 'k6/http';
import { check } from 'k6';
import { baseUrl, requiredEnv, requireTestEnv, rampingOptions } from '../lib/config.js';
import { summaryOutputs } from '../lib/report.js';

export const options = rampingOptions(5, 200);

export default function () {
  const appId = requiredEnv('CORE_APP_ID');
  const env = requireTestEnv('CORE_ENV');
  const path = '/api/core/config/' + env + '/' + encodeURIComponent(appId);
  const response = http.get(baseUrl() + path, {
    headers: { 'User-Agent': 'MiddlePlatform-LoadTest/' + (__ENV.LOADTEST_RUN_ID || 'manual') },
    tags: { service: 'sdk-core', operation: 'config-read' },
    timeout: '10s',
  });
  check(response, {
    'sdk-core config returns HTTP 200': function (r) { return r.status === 200; },
    'sdk-core business code is zero': function (r) {
      try { return r.json().code === 0; } catch (_) { return false; }
    },
  });
}

export function handleSummary(data) {
  return summaryOutputs(data);
}
