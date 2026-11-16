import http from 'k6/http';
import { check } from 'k6';
import { baseUrl, requiredEnv, requireTestEnv, constantRateOptions } from '../lib/config.js';
import { gameHeaders } from '../lib/signature.js';
import { summaryOutputs } from '../lib/report.js';

export const options = constantRateOptions(1, '5m');

export default function () {
  if (requiredEnv('SDK_SERVICE_SKIP_USER_CHECK_CONFIRMED') !== 'true') {
    throw new Error('Confirm the test channel has user-check bypass enabled before running');
  }

  const appId = requiredEnv('SDK_SERVICE_APP_ID');
  const secret = requiredEnv('SDK_SERVICE_GAME_SECRET');
  const env = requireTestEnv('SDK_SERVICE_ENV');
  const prefix = requiredEnv('OPEN_ID_PREFIX');
  const openId = prefix + '-' + (__ENV.LOADTEST_RUN_ID || 'run') + '-' + __VU + '-' + __ITER;
  if (openId.length > 256) {
    throw new Error('OPEN_ID_PREFIX is too long');
  }

  const path = '/api/service/user/check/' + env;
  const body = JSON.stringify({ open_id: openId });
  const headers = gameHeaders('POST', path, appId, secret, body);
  const response = http.post(baseUrl() + path, body, {
    headers: headers,
    tags: { service: 'sdk-service', operation: 'user-check-test' },
    timeout: '15s',
  });
  check(response, {
    'sdk-service user check returns HTTP 200': function (r) { return r.status === 200; },
    'sdk-service user check business code is zero': function (r) {
      try { return r.json().code === 0; } catch (_) { return false; }
    },
  });
}

export function handleSummary(data) {
  return summaryOutputs(data);
}
