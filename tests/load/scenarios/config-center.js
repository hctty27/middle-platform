import http from 'k6/http';
import { check } from 'k6';
import { baseUrl, optionalEnv, requiredEnv, rampingOptions } from '../lib/config.js';
import { subscriberHeaders } from '../lib/signature.js';
import { summaryOutputs } from '../lib/report.js';

export const options = rampingOptions(5, 200);

export default function () {
  const root = baseUrl();
  const code = requiredEnv('CONFIG_SUBSCRIBER_CODE');
  const secret = requiredEnv('CONFIG_SUBSCRIBER_SECRET');
  const operation = optionalEnv('CONFIG_SYNC_OPERATION', 'revision').toLowerCase();

  let path = '/api/config/sync/revision';
  let query = '';
  if (operation === 'snapshot') {
    path = '/api/config/sync/snapshot';
  } else if (operation === 'changes') {
    const afterRevision = optionalEnv('CONFIG_AFTER_REVISION', '0');
    const limit = optionalEnv('CONFIG_CHANGE_LIMIT', '50');
    if (!/^\d+$/.test(afterRevision) || !/^\d+$/.test(limit) ||
        Number(limit) < 1 || Number(limit) > 500) {
      throw new Error('CONFIG_AFTER_REVISION and CONFIG_CHANGE_LIMIT are invalid');
    }
    path = '/api/config/sync/changes';
    query = 'afterRevision=' + afterRevision + '&limit=' + limit;
  } else if (operation !== 'revision') {
    throw new Error('CONFIG_SYNC_OPERATION must be revision, changes, or snapshot');
  }

  const headers = subscriberHeaders('GET', path, query, code, secret, '');
  const url = root + path + (query ? '?' + query : '');
  const response = http.get(url, {
    headers: headers,
    tags: { service: 'config-center', operation: operation },
    timeout: '15s',
  });
  check(response, {
    'config-center sync returns HTTP 200': function (r) { return r.status === 200; },
  });
}

export function handleSummary(data) {
  return summaryOutputs(data);
}
