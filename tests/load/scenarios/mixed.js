import http from 'k6/http';
import { check } from 'k6';
import { baseUrl, optionalEnv, requiredEnv, requireTestEnv, rampingOptions } from '../lib/config.js';
import { gameHeaders, subscriberHeaders } from '../lib/signature.js';
import { summaryOutputs } from '../lib/report.js';

export const options = rampingOptions(5, 200);

function configRevision(root) {
  const code = requiredEnv('CONFIG_SUBSCRIBER_CODE');
  const secret = requiredEnv('CONFIG_SUBSCRIBER_SECRET');
  const path = '/api/config/sync/revision';
  const headers = subscriberHeaders('GET', path, '', code, secret, '');
  const response = http.get(root + path, {
    headers: headers,
    tags: { service: 'config-center', operation: 'revision' },
    timeout: '10s',
  });
  check(response, { 'config-center revision returns HTTP 200': function (r) { return r.status === 200; } });
}

function coreConfig(root) {
  const appId = requiredEnv('CORE_APP_ID');
  const env = requireTestEnv('CORE_ENV');
  const path = '/api/core/config/' + env + '/' + encodeURIComponent(appId);
  const response = http.get(root + path, {
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

function serviceUserCheck(root) {
  if (requiredEnv('SDK_SERVICE_SKIP_USER_CHECK_CONFIRMED') !== 'true') {
    throw new Error('Service mixed traffic requires confirmed test-channel user-check bypass');
  }
  const appId = requiredEnv('SDK_SERVICE_APP_ID');
  const secret = requiredEnv('SDK_SERVICE_GAME_SECRET');
  const env = requireTestEnv('SDK_SERVICE_ENV');
  const openId = requiredEnv('OPEN_ID_PREFIX') + '-' +
    (__ENV.LOADTEST_RUN_ID || 'run') + '-' + __VU + '-' + __ITER;
  if (openId.length > 256) {
    throw new Error('OPEN_ID_PREFIX is too long');
  }
  const path = '/api/service/user/check/' + env;
  const body = JSON.stringify({ open_id: openId });
  const headers = gameHeaders('POST', path, appId, secret, body);
  const response = http.post(root + path, body, {
    headers: headers,
    tags: { service: 'sdk-service', operation: 'user-check-test' },
    timeout: '15s',
  });
  check(response, {
    'sdk-service user check returns HTTP 200': function (r) { return r.status === 200; },
    'sdk-service business code is zero': function (r) {
      try { return r.json().code === 0; } catch (_) { return false; }
    },
  });
}

export default function () {
  const root = baseUrl();
  const includeService = optionalEnv('MIX_INCLUDE_SERVICE', 'false') === 'true';
  const choice = Math.random();
  if (includeService && choice >= 0.9) {
    serviceUserCheck(root);
  } else if (choice < 0.4) {
    configRevision(root);
  } else {
    coreConfig(root);
  }
}

export function handleSummary(data) {
  return summaryOutputs(data);
}
