import http from 'k6/http';
import { check } from 'k6';
import { baseUrl, requiredEnv, requireTestEnv, constantRateOptions } from '../lib/config.js';
import { gameHeaders } from '../lib/signature.js';
import { summaryOutputs } from '../lib/report.js';

export const options = constantRateOptions(1, '5m');

export default function () {
  if (requiredEnv('ALLOW_ORDER_WRITES') !== 'true' ||
      requiredEnv('ORDER_NOTIFY_SINK_CONFIRMED') !== 'true') {
    throw new Error('Order writes require explicit opt-in and a verified test notification sink');
  }

  const appId = requiredEnv('CORE_APP_ID');
  const secret = requiredEnv('CORE_GAME_SECRET');
  const env = requireTestEnv('CORE_ENV');
  const prefix = requiredEnv('ORDER_ID_PREFIX').replace(/[^A-Za-z0-9_-]/g, '').slice(0, 20);
  if (!prefix) {
    throw new Error('ORDER_ID_PREFIX must contain letters, numbers, underscore, or hyphen');
  }

  const orderId = prefix + '-' + (__ENV.LOADTEST_RUN_ID || 'run') + '-' + __VU + '-' + __ITER;
  const amount = Number(__ENV.ORDER_AMOUNT || '1');
  if (!Number.isSafeInteger(amount) || amount <= 0) {
    throw new Error('ORDER_AMOUNT must be a positive integer');
  }

  const path = '/api/core/orders/' + env + '/' + encodeURIComponent(appId);
  const body = JSON.stringify({
    game_order_id: orderId,
    amount: amount,
    attach: 'middle-platform-load-test',
  });
  const headers = gameHeaders('POST', path, appId, secret, body);
  const response = http.post(baseUrl() + path, body, {
    headers: headers,
    tags: { service: 'sdk-core', operation: 'order-register-write' },
    timeout: '15s',
  });
  check(response, {
    'sdk-core order registration returns HTTP 200': function (r) { return r.status === 200; },
    'sdk-core order registration business code is zero': function (r) {
      try { return r.json().code === 0; } catch (_) { return false; }
    },
  });
}

export function handleSummary(data) {
  return summaryOutputs(data);
}
