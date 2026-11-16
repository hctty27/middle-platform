import http from 'k6/http';
import { check } from 'k6';
import { baseUrl, requiredEnv, requireTestEnv } from '../lib/config.js';
import { subscriberHeaders } from '../lib/signature.js';
import { summaryOutputs } from '../lib/report.js';

export const options = {
  vus: 1,
  iterations: 1,
  thresholds: {
    http_req_failed: ['rate==0'],
    checks: ['rate==1'],
  },
};

export default function () {
  const root = baseUrl();
  const code = requiredEnv('CONFIG_SUBSCRIBER_CODE');
  const secret = requiredEnv('CONFIG_SUBSCRIBER_SECRET');
  const appId = requiredEnv('CORE_APP_ID');
  const env = requireTestEnv('CORE_ENV');

  const configPath = '/api/config/sync/revision';
  const configHeaders = subscriberHeaders('GET', configPath, '', code, secret, '');
  const configResponse = http.get(root + configPath, {
    headers: configHeaders,
    tags: { service: 'config-center', operation: 'revision' },
    timeout: '10s',
  });
  check(configResponse, {
    'config-center revision returns HTTP 200': function (r) { return r.status === 200; },
  });

  const corePath = '/api/core/config/' + env + '/' + encodeURIComponent(appId);
  const coreResponse = http.get(root + corePath, {
    headers: { 'User-Agent': 'MiddlePlatform-LoadTest/' + (__ENV.LOADTEST_RUN_ID || 'manual') },
    tags: { service: 'sdk-core', operation: 'config-read' },
    timeout: '10s',
  });
  check(coreResponse, {
    'sdk-core config returns HTTP 200': function (r) { return r.status === 200; },
    'sdk-core config business code is zero': function (r) {
      try { return r.json().code === 0; } catch (_) { return false; }
    },
  });
}

export function handleSummary(data) {
  return summaryOutputs(data);
}
