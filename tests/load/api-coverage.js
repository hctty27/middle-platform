// One-pass, full HTTP-route contract coverage using ONLY disposable GitHub-hosted
// runner services and synthetic fixtures. This is NOT the high-QPS write workload.
import http from 'k6/http';
import crypto from 'k6/crypto';
import { Counter } from 'k6/metrics';
import { sleep } from 'k6';

const edge = 'http://127.0.0.1:8280';
const access = 'http://127.0.0.1:8081';
const adminSecret = 'runner-load-admin-only-0123456789abcdef';
const subscriber = 'load-subscriber';
const subscriberSecret = 'runner-load-subscriber-only-0123456789';
const gameSecret = 'runner-load-game-only';
const platformSecret = 'runner-load-platform-only';
const app = 'load-app';
const failed = new Counter('full_api_failed');
const covered = new Counter('full_api_covered');
const errors = [];
const expectedResponses = {
  200: http.expectedStatuses(200),
  201: http.expectedStatuses(201),
  202: http.expectedStatuses(202),
  204: http.expectedStatuses(204),
  409: http.expectedStatuses(409),
};

export const options = {
  scenarios: { full_api: { executor: 'shared-iterations', vus: 1, iterations: 1, maxDuration: '90s' } },
  thresholds: { full_api_failed: ['count==0'], full_api_covered: ['count>=59'] },
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
  maxRedirects: 0,
};

function stamp() {
  return {
    ts: String(Math.floor(Date.now() / 1000)),
    nonce: Array.from(new Uint8Array(crypto.randomBytes(8)), b => b.toString(16).padStart(2, '0')).join(''),
  };
}
function headers(kind, method, path, body, extra) {
  const x = stamp();
  const out = { 'Content-Type': 'application/json', 'x-timestamp': x.ts, 'x-nonce': x.nonce };
  const purePath = path.split('?')[0];
  const query = (path.split('?')[1] || '').split('&').filter(Boolean).sort().join('&');
  if (kind === 'admin' || kind === 'sync' || kind === 'access') {
    if (kind !== 'admin') out['x-subscriber-code'] = subscriber;
    const prefix = 'HITALK-HMAC-SHA256\n' +
      (kind === 'admin' ? '' : 'SUBSCRIBER\n' + subscriber + '\n');
    out['x-signature'] = crypto.hmac('sha256', kind === 'admin' ? adminSecret : subscriberSecret,
      prefix + [method, purePath, query, x.ts, x.nonce, ''].join('\n') + body, 'hex');
  } else if (kind === 'game') {
    out['x-app-id'] = app;
    out['x-signature'] = crypto.hmac('sha256', gameSecret,
      ['HITALK-HMAC-SHA256', method, purePath, app, x.ts, x.nonce, ''].join('\n') + body, 'hex');
  } else if (kind === 'platform') {
    out['x-app-id'] = app;
    out['x-signature'] = crypto.sha1([platformSecret, app, x.ts, x.nonce].concat(extra || [])
      .filter(v => v !== '' && v !== null && v !== undefined).map(String).sort().join('_'), 'hex');
  } else if (kind === 'payment') {
    out['x-signature'] = crypto.sha1([extra.orderNo, extra.orderID, platformSecret, x.nonce, x.ts].sort().join('_'), 'hex');
  }
  return out;
}
function parsed(response) {
  try { return response.json(); } catch (_) { return null; }
}
function test(route, actual, body = null, kind = '', expected = 200, predicate = null, extra = null, origin = edge) {
  const method = route.split(' ')[0];
  const json = body === null ? '' : JSON.stringify(body);
  const hdr = headers(kind, method, actual, json, extra);
  const res = http.request(method, origin + actual, json || null, {
    headers: hdr, timeout: '8s', tags: { name: route },
    responseCallback: expectedResponses[expected],
  });
  const data = parsed(res);
  const ok = res.status === expected && (predicate ? predicate(data, res) : true);
  covered.add(1, { route });
  if (!ok) {
    failed.add(1, { route });
    const problem = route + ' expected=' + expected + ' actual=' + res.status;
    errors.push(problem);
    console.error(problem);
  } else {
    console.log('PASS ' + route + ' status=' + res.status);
  }
  return { data, res, ok };
}
function idOf(result, name) {
  if (!result.ok || !result.data || !(Number(result.data.id) > 0)) {
    throw new Error('Cannot continue: ' + name + ' fixture was not created');
  }
  return Number(result.data.id);
}
function good(data) { return data !== null && data !== undefined; }
function success(data) { return data && data.code === 0; }
function list(data) { return Array.isArray(data); }
function get(route, path, mode = '', predicate = good, origin = edge) {
  return test(route, path, null, mode, 200, predicate, null, origin);
}
function write(route, path, body, mode = 'admin', status = 204, predicate = null) {
  return test(route, path, body, mode, status, predicate);
}

export default function () {
  // Health endpoints, including the optional config-center-access replica.
  for (const port of [8201, 8211, 8221, 8231, 8081]) {
    const origin = 'http://127.0.0.1:' + port;
    test('GET /health/live', '/health/live', null, '', 200, good, null, origin);
    test('GET /health/ready', '/health/ready', null, '', 200, good, null, origin);
  }
  // Configuration synchronization, all HMAC-protected with subscriber identity.
  const original = get('GET /api/config/sync/revision', '/api/config/sync/revision', 'sync',
    d => d && typeof d.revision === 'number' && !!d.epoch).data;
  get('GET /api/config/sync/snapshot', '/api/config/sync/snapshot', 'sync',
    d => d && Array.isArray(d.nodes) && d.epoch === original.epoch);
  get('GET /api/config/sync/changes', '/api/config/sync/changes?afterRevision=0&limit=20', 'sync', list);
  get('GET /api/config/nodes/{id}', '/api/config/nodes/1', 'sync', d => d && d.id === 1);
  test('POST /api/config/nodes/batch-get', '/api/config/nodes/batch-get', { ids: [1, 2, 3] },
    'sync', 200, d => Array.isArray(d) && d.length === 3);

  // Read-only config-center management routes.
  get('GET /api/config/admin/config-schema/{nodeType}', '/api/config/admin/config-schema/game', 'admin',
    d => d && d.nodeType === 'game');
  get('GET /api/config/admin/nodes', '/api/config/admin/nodes', 'admin', list);
  get('GET /api/config/admin/nodes/{id}', '/api/config/admin/nodes/1', 'admin', d => d && d.id === 1);
  get('GET /api/config/admin/nodes/tree', '/api/config/admin/nodes/tree?parentId=0', 'admin', list);
  get('GET /api/config/admin/nodes/children',
    '/api/config/admin/nodes/children?parentId=1&page=1&size=20', 'admin',
    d => d && Array.isArray(d.items));
  get('GET /api/config/admin/nodes/{id}/children', '/api/config/admin/nodes/1/children', 'admin',
    d => d && Array.isArray(d.items));
  get('GET /api/config/admin/nodes/search', '/api/config/admin/nodes/search?keyword=load', 'admin', list);
  get('GET /api/config/admin/nodes/{id}/path', '/api/config/admin/nodes/3/path', 'admin', good);
  get('GET /api/config/admin/subscribers', '/api/config/admin/subscribers', 'admin', list);

  // Config-node lifecycle: create game -> region -> group, then reverse-delete.
  const suffix = String(Date.now());
  const game = idOf(write('POST /api/config/admin/nodes', '/api/config/admin/nodes', {
    parentId: 0, nodeType: 'game', marketType: 'domestic', code: 'cov-game-' + suffix,
    name: 'Coverage game', status: 1, config: '{}',
  }, 'admin', 201, good), 'game');
  write('PUT /api/config/admin/nodes/{id}', '/api/config/admin/nodes/' + game,
    { name: 'Coverage game updated', status: 1, config: '{}', remark: 'runner test' },
    'admin', 200, d => d && d.id === game);
  const region = idOf(write('POST /api/config/admin/nodes', '/api/config/admin/nodes', {
    parentId: game, nodeType: 'region', envType: 'test', code: 'cov-region-' + suffix,
    name: 'Coverage region', status: 1, config: '{}',
  }, 'admin', 201, good), 'region');
  const group = idOf(write('POST /api/config/admin/nodes', '/api/config/admin/nodes', {
    parentId: region, nodeType: 'server_group', code: 'cov-group-' + suffix,
    name: 'Coverage group', status: 1, config: '{}',
  }, 'admin', 201, good), 'group');

  // Subscriber lifecycle, never exposing or using any real secret.
  const subCode = 'cov-sub-' + suffix;
  write('POST /api/config/admin/subscribers', '/api/config/admin/subscribers', {
    code: subCode, callbackUrl: 'http://127.0.0.1:8290/config/webhook',
    secret: 'runner-full-api-only-' + suffix + '-123456789',
  }, 'admin', 201);
  const subs = get('GET /api/config/admin/subscribers', '/api/config/admin/subscribers', 'admin', list).data || [];
  const row = subs.find(x => x.code === subCode);
  if (!row) throw new Error('subscriber fixture missing');
  write('PUT /api/config/admin/subscribers/{id}', '/api/config/admin/subscribers/' + row.id,
    { callbackUrl: 'http://127.0.0.1:8290/config/webhook', secret: '' });
  write('PATCH /api/config/admin/subscribers/{id}/enabled',
    '/api/config/admin/subscribers/' + row.id + '/enabled', { enabled: false });
  write('DELETE /api/config/admin/subscribers/{id}', '/api/config/admin/subscribers/' + row.id, null);

  // SDK Admin management: schema, node config, channel CRUD, bindings and service switches.
  get('GET /api/sdk/admin/config-schema/{nodeType}', '/api/sdk/admin/config-schema/game', 'admin',
    d => d && d.nodeType === 'game');
  get('GET /api/sdk/admin/node-configs/{id}', '/api/sdk/admin/node-configs/' + game, 'admin', good);
  write('PUT /api/sdk/admin/node-configs/{id}', '/api/sdk/admin/node-configs/' + game,
    { config: '{"gameSecret":"runner-full-api-game-only"}' }, 'admin', 200, good);
  get('GET /api/sdk/admin/channel-apps', '/api/sdk/admin/channel-apps?gameId=1', 'admin', list);
  get('GET /api/sdk/admin/channel-apps/server-groups',
    '/api/sdk/admin/channel-apps/server-groups?envType=test&gameId=1', 'admin', list);
  get('GET /api/sdk/admin/server-groups/{id}/channel-apps',
    '/api/sdk/admin/server-groups/3/channel-apps', 'admin', list);
  const channel = idOf(write('POST /api/sdk/admin/channel-apps', '/api/sdk/admin/channel-apps', {
    gameId: game, code: 'cov-app-' + suffix, name: 'Coverage channel', status: 1,
    config: '{"appSecret":"runner-full-api-app-only"}', testServerGroupId: group,
  }, 'admin', 201, good), 'channel');
  write('PUT /api/sdk/admin/channel-apps/{id}', '/api/sdk/admin/channel-apps/' + channel, {
    name: 'Coverage channel updated', status: 1, config: '{"appSecret":"runner-full-api-app-only"}',
    testServerGroupId: group,
  }, 'admin', 200, good);
  const services = get('GET /api/sdk/admin/services', '/api/sdk/admin/services', 'admin', list).data || [];
  const role = services.find(x => x.code === 'role_query');
  if (!role) throw new Error('role_query catalog fixture missing');
  write('PUT /api/sdk/admin/services/{id}', '/api/sdk/admin/services/' + role.id, { enabled: true });
  get('GET /api/sdk/admin/channel-apps/{id}/services',
    '/api/sdk/admin/channel-apps/' + channel + '/services?env=test', 'admin', list);
  write('PUT /api/sdk/admin/channel-apps/{id}/services/{serviceId}',
    '/api/sdk/admin/channel-apps/' + channel + '/services/' + role.id + '?env=test',
    { enabled: true, config: {} });
  // Detach before deleting the channel, group, region and game.
  write('PUT /api/sdk/admin/channel-apps/{id}', '/api/sdk/admin/channel-apps/' + channel,
    { name: 'Coverage channel updated', status: 1,
      config: '{"appSecret":"runner-full-api-app-only"}', testServerGroupId: null },
    'admin', 200, good);
  write('DELETE /api/sdk/admin/channel-apps/{id}', '/api/sdk/admin/channel-apps/' + channel, null);
  write('DELETE /api/config/admin/nodes/{id}', '/api/config/admin/nodes/' + group, null);
  write('DELETE /api/config/admin/nodes/{id}', '/api/config/admin/nodes/' + region, null);
  write('DELETE /api/config/admin/nodes/{id}', '/api/config/admin/nodes/' + game, null);

  // SDK public business routes; simulated external endpoints remain on loopback.
  get('GET /api/core/config/{env}/{appid}', '/api/core/config/test/' + app, '',
    d => success(d) && d.data.snKey === app);
  test('POST /api/service/user/check/{env}', '/api/service/user/check/test',
    { open_id: 'load-user' }, 'game', 200, d => success(d) && d.data === true);
  test('POST /api/service/events/{env}', '/api/service/events/test',
    { event: 'login', ext: { open_id: 'load-user', roleId: 'load-role' } },
    'game', 200, d => success(d) && d.data === true);
  test('POST /api/service/subscribe/{env}', '/api/service/subscribe/test',
    { open_id: 'load-user', tpl_id: 'runner-template' }, 'game', 200,
    d => success(d) && d.data === true);
  test('POST /api/service/role/query/{env}', '/api/service/role/query/test',
    { open_id: 'load-user' }, 'platform', 200, d => success(d) && Array.isArray(d.data),
    ['load-user']);
  test('POST /api/service/server/query/{env}', '/api/service/server/query/test',
    { page: 1, page_size: 10, zonekey: 'load-group' }, 'platform', 200,
    d => success(d) && d.data && Array.isArray(d.data.list),
    ['1', '10', 'load-group']);
  test('POST /api/service/role/control/{env}', '/api/service/role/control/test',
    { type: 'lock', role_id: 'load-role', duration: 0 }, 'platform', 200, success,
    ['lock', 'load-role', '0']);

  // Orders, payment callback and all order-admin endpoints.
  const orderID = 'cov-' + suffix;
  test('POST /api/core/orders/{env}/{appid}', '/api/core/orders/test/' + app,
    { game_order_id: orderID, amount: 100, attach: 'runner-test' }, 'game', 200,
    d => success(d) && d.data.game_order_id === orderID);
  const orderNo = 'p-' + orderID;
  const payment = test('POST /api/core/pay/notify/{appid}', '/api/core/pay/notify/' + app,
    { order_no: orderNo, game_order_id: orderID, amount: 100,
      attach: 'runner-test', paid_at: new Date().toISOString() },
    'payment', 200, (_d, r) => /^[0-9a-f]{16}$/.test(r.body),
    { orderNo, orderID });
  if (!payment.ok) throw new Error('payment fixture missing');
  const orders = get('GET /api/sdk/admin/orders',
    '/api/sdk/admin/orders?gameId=1&gameOrderId=' + orderID, 'admin',
    d => success(d) && Array.isArray(d.data.items)).data;
  if (!orders || !orders.data.items.length) throw new Error('order missing in admin list');
  const storedID = orders.data.items[0].id;
  get('GET /api/sdk/admin/orders/stats', '/api/sdk/admin/orders/stats?gameId=1', 'admin',
    d => success(d) && typeof d.data.registered === 'number');
  get('GET /api/sdk/admin/orders/{id}', '/api/sdk/admin/orders/' + storedID + '?gameId=1',
    'admin', d => success(d) && d.data.gameOrderId === orderID);
  // On a fully delivered order, retry correctly rejects with 409; the happy
  // path is separately verified in dedicated order-worker integration tests.
  test('POST /api/sdk/admin/orders/{id}/retry',
    '/api/sdk/admin/orders/' + storedID + '/retry?gameId=1',
    null, 'admin', 409, d => d && d.code === 409);

  // Confirm changes, then verify the optional replica read and signed webhook.
  get('GET /api/config/sync/changes', '/api/config/sync/changes?afterRevision=0&limit=200',
    'sync', list);
  const revised = get('GET /api/config/sync/revision', '/api/config/sync/revision', 'sync',
    d => d && d.revision > original.revision).data;
  test('POST /internal/config/callback', '/internal/config/callback',
    { epoch: revised.epoch, revision: revised.revision }, 'access', 202,
    null, null, access);
  let synchronized = false;
  for (let attempt = 0; attempt < 15; attempt++) {
    const rev = get('GET /api/access/revision', '/api/access/revision', 'access',
      d => d && typeof d.revision === 'number', access);
    if (rev.data && rev.data.revision === revised.revision) {
      synchronized = true;
      break;
    }
    sleep(0.5);
  }
  if (!synchronized) {
    errors.push('config-center-access did not converge to latest revision');
    failed.add(1);
  }
  get('GET /api/access/nodes', '/api/access/nodes', 'access',
    d => Array.isArray(d) && d.some(n => n.id === 1), access);

  if (errors.length) throw new Error(errors.length + ' full-api checks failed: ' + errors.join(' | '));
  console.log('Full API route coverage completed with synthetic runner-only fixtures.');
}
