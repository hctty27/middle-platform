// One measured HTTP request per k6 iteration. All fixture hosts are loopback-only.
import http from 'k6/http';
import crypto from 'k6/crypto';
import exec from 'k6/execution';
import { Rate } from 'k6/metrics';

const catalog = JSON.parse(open('tests/load/endpoint-qps-catalog.json'));
const spec = catalog.find(x => x.id === __ENV.QPS_CASE);
if (!spec) throw new Error('Unknown QPS_CASE: ' + __ENV.QPS_CASE);
const rate = Number(__ENV.QPS_RATE || '10');
const seconds = Number(__ENV.QPS_SECONDS || '5');
const allocated = Number(__ENV.QPS_VUS || '30');
if (!Number.isInteger(rate) || rate < 1 || rate > 20000 ||
    !Number.isInteger(seconds) || seconds < 2 || seconds > 30 ||
    !Number.isInteger(allocated) || allocated < 1 || allocated > 1000) {
  throw new Error('QPS rates, seconds or preallocated VUs outside bounded runner limits');
}
const local = 'http://127.0.0.1:8280';
const access = 'http://127.0.0.1:8081';
const adminSecret = 'runner-load-admin-only-0123456789abcdef';
const subscriberSecret = 'runner-load-subscriber-only-0123456789';
const subscriber = 'load-subscriber';
const gameSecret = 'runner-load-game-only';
const platformSecret = 'runner-load-platform-only';
const app = 'load-app';
const stage = __ENV.QPS_STAGE || 'stage';
const poolStart = Number(__ENV.QPS_BASE_ID || '0');
const localSubscriber = Number(__ENV.QPS_SUBSCRIBER_ID || '90000');
const roleServiceID = Number(__ENV.QPS_ROLE_SERVICE_ID || '1');
const order = __ENV.QPS_ORDER_ID || '';
const gameOrderID = __ENV.QPS_GAME_ORDER_ID || '';
const platformOrderNO = __ENV.QPS_PLATFORM_ORDER_NO || '';
const orderAmount = Number(__ENV.QPS_ORDER_AMOUNT || '100');
const currentEpoch = __ENV.QPS_EPOCH || '';
const currentRevision = Number(__ENV.QPS_REVISION || '0');
const bad = new Rate('endpoint_bad');
export const options = {
  scenarios: { endpoint: { executor: 'constant-arrival-rate',
    rate, timeUnit: '1s', duration: seconds + 's',
    preAllocatedVUs: allocated, gracefulStop: '5s' } },
  thresholds: { endpoint_bad: ['rate<0.01'] },
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
  noConnectionReuse: false, maxRedirects: 0,
};

function randomStamp() {
  const bytes = new Uint8Array(crypto.randomBytes(8));
  return { timestamp: String(Math.floor(Date.now() / 1000)),
    nonce: Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('') };
}
function signature(kind, method, path, body, extras) {
  const { timestamp, nonce } = randomStamp();
  const pure = path.split('?')[0];
  const query = (path.split('?')[1] || '').split('&').filter(Boolean).sort().join('&');
  const headers = { 'Content-Type': 'application/json', 'x-timestamp': timestamp, 'x-nonce': nonce };
  if (kind === 'admin' || kind === 'sync' || kind === 'access') {
    if (kind !== 'admin') headers['x-subscriber-code'] = subscriber;
    const prefix = 'HITALK-HMAC-SHA256\n' +
      (kind === 'admin' ? '' : 'SUBSCRIBER\n' + subscriber + '\n');
    headers['x-signature'] = crypto.hmac('sha256',
      kind === 'admin' ? adminSecret : subscriberSecret,
      prefix + [method, pure, query, timestamp, nonce, ''].join('\n') + body, 'hex');
  } else if (kind === 'game') {
    headers['x-app-id'] = app;
    headers['x-signature'] = crypto.hmac('sha256', gameSecret,
      ['HITALK-HMAC-SHA256', method, pure, app, timestamp, nonce, ''].join('\n') + body, 'hex');
  } else if (kind === 'platform') {
    headers['x-app-id'] = app;
    headers['x-signature'] = crypto.sha1(
      [platformSecret, app, timestamp, nonce].concat(extras || []).filter(x => x !== '' && x !== null)
      .map(String).sort().join('_'), 'hex');
  } else if (kind === 'payment') {
    headers['x-signature'] = crypto.sha1(
      [platformOrderNO, gameOrderID, platformSecret, nonce, timestamp].sort().join('_'), 'hex');
  }
  return headers;
}
function requestPlan(seq) {
  const unique = stage + '-' + seq;
  const group = String(poolStart + seq);
  const base = { origin: local, mode: '', path: '', body: null, extra: null };
  switch (spec.id) {
  case 'health-live': return { ...base, origin: 'http://127.0.0.1:8201', path:'/health/live' };
  case 'health-ready': return { ...base, origin: 'http://127.0.0.1:8201', path:'/health/ready' };
  case 'config-revision': return { ...base, mode:'sync', path:'/api/config/sync/revision' };
  case 'config-snapshot': return { ...base, mode:'sync', path:'/api/config/sync/snapshot' };
  case 'config-changes': return { ...base, mode:'sync', path:'/api/config/sync/changes?afterRevision=0&limit=20' };
  case 'config-node': return { ...base, mode:'sync', path:'/api/config/nodes/1' };
  case 'config-batch': return { ...base, mode:'sync', path:'/api/config/nodes/batch-get', body:{ids:[1,2,3]} };
  case 'config-schema': return { ...base, mode:'admin', path:'/api/config/admin/config-schema/game' };
  case 'config-list': return { ...base, mode:'admin', path:'/api/config/admin/nodes' };
  case 'config-admin-node': return { ...base, mode:'admin', path:'/api/config/admin/nodes/1' };
  case 'config-tree': return { ...base, mode:'admin', path:'/api/config/admin/nodes/tree?parentId=0' };
  case 'config-children': return { ...base, mode:'admin', path:'/api/config/admin/nodes/children?parentId=1&page=1&size=20' };
  case 'config-children-by-id': return { ...base, mode:'admin', path:'/api/config/admin/nodes/1/children' };
  case 'config-search': return { ...base, mode:'admin', path:'/api/config/admin/nodes/search?keyword=load' };
  case 'config-path': return { ...base, mode:'admin', path:'/api/config/admin/nodes/3/path' };
  case 'config-subscribers': return { ...base, mode:'admin', path:'/api/config/admin/subscribers' };
  case 'config-create-node': return { ...base, mode:'admin', path:'/api/config/admin/nodes',
    body:{parentId:0,nodeType:'game',code:'qps-game-'+unique,name:'QPS Game',status:1,config:'{}'} };
  case 'config-update-node': return { ...base, mode:'admin', path:'/api/config/admin/nodes/1',
    body:{name:'Load Game',status:1,config:'{}',remark:'qps-'+unique} };
  case 'config-delete-node': return { ...base, mode:'admin', path:'/api/config/admin/nodes/'+group };
  case 'config-create-subscriber': return { ...base, mode:'admin', path:'/api/config/admin/subscribers',
    body:{code:'qps-sub-'+unique,callbackUrl:'http://127.0.0.1:8290/config/webhook',
      secret:'runner-qps-secret-'+unique+'-abcdef0123456789'} };
  case 'config-update-subscriber': return { ...base, mode:'admin', path:'/api/config/admin/subscribers/'+localSubscriber,
    body:{callbackUrl:'http://127.0.0.1:8290/config/webhook',secret:''} };
  case 'config-switch-subscriber': return { ...base, mode:'admin',
    path:'/api/config/admin/subscribers/'+localSubscriber+'/enabled', body:{enabled:false} };
  case 'config-delete-subscriber': return { ...base, mode:'admin', path:'/api/config/admin/subscribers/'+group };
  case 'core-schema': return { ...base, mode:'admin', path:'/api/core/admin/config-schema/game' };
  case 'core-node-config': return { ...base, mode:'admin', path:'/api/core/admin/node-configs/1' };
  case 'core-node-config-update': return { ...base, mode:'admin', path:'/api/core/admin/node-configs/1',
    body:{config:'{"gameSecret":"runner-load-game-only"}'} };
  case 'core-channels': return { ...base, mode:'admin', path:'/api/core/admin/channel-apps?gameId=1' };
  case 'core-server-groups': return { ...base, mode:'admin',
    path:'/api/core/admin/channel-apps/server-groups?envType=test&gameId=1' };
  case 'core-server-bound': return { ...base, mode:'admin', path:'/api/core/admin/server-groups/3/channel-apps' };
  case 'core-create-channel': return { ...base, mode:'admin', path:'/api/core/admin/channel-apps',
    body:{gameId:1,code:'qps-app-'+unique,name:'QPS channel',status:1,
      config:'{"appSecret":"runner-qps-platform-only"}',testServerGroupId:3} };
  case 'core-update-channel': return { ...base, mode:'admin', path:'/api/core/admin/channel-apps/1',
    body:{name:'Load App',status:1,
      config:'{"appSecret":"runner-load-platform-only","tsKey":"load-ts"}',testServerGroupId:3} };
  case 'core-delete-channel': return { ...base, mode:'admin', path:'/api/core/admin/channel-apps/'+group };
  case 'core-services': return { ...base, mode:'admin', path:'/api/core/admin/services' };
  case 'core-switch-service': return { ...base, mode:'admin',
    path:'/api/core/admin/services/'+roleServiceID, body:{enabled:true} };
  case 'core-channel-services': return { ...base, mode:'admin',
    path:'/api/core/admin/channel-apps/1/services?env=test' };
  case 'core-switch-channel-service': return { ...base, mode:'admin',
    path:'/api/core/admin/channel-apps/1/services/'+roleServiceID+'?env=test',
    body:{enabled:true,config:{}} };
  case 'core-config': return { ...base, path:'/api/core/config/test/'+app };
  case 'service-user-check': return { ...base, mode:'game', path:'/api/service/user/check/test',
    body:{open_id:'load-user'} };
  case 'service-event': return { ...base, mode:'game', path:'/api/service/events/test',
    body:{event:'login',ext:{open_id:'load-user',roleId:'load-role'}} };
  case 'service-subscribe': return { ...base, mode:'game', path:'/api/service/subscribe/test',
    body:{open_id:'load-user',tpl_id:'runner-template'} };
  case 'service-role-query': return { ...base, mode:'platform', path:'/api/service/role/query/test',
    body:{open_id:'load-user'},extra:['load-user'] };
  case 'service-server-query': return { ...base, mode:'platform', path:'/api/service/server/query/test',
    body:{page:1,page_size:10,zonekey:'load-group'},extra:['1','10','load-group'] };
  case 'service-role-control': return { ...base, mode:'platform', path:'/api/service/role/control/test',
    body:{type:'lock',role_id:'load-role',duration:0},extra:['lock','load-role','0'] };
  case 'core-order-register': return { ...base, mode:'game', path:'/api/core/orders/test/'+app,
    body:{game_order_id:'qps-order-'+unique,amount:100,attach:'runner-qps'} };
  case 'core-payment-duplicate': return { ...base, mode:'payment', path:'/api/core/pay/notify/'+app,
    body:{order_no:platformOrderNO,game_order_id:gameOrderID,
      amount:orderAmount,attach:'runner-test',paid_at:new Date().toISOString()} };
  case 'core-orders': return { ...base, mode:'admin', path:'/api/core/admin/orders?gameId=1&pageSize=20' };
  case 'core-order-stats': return { ...base, mode:'admin', path:'/api/core/admin/orders/stats?gameId=1' };
  case 'core-order-detail': return { ...base, mode:'admin', path:'/api/core/admin/orders/'+order+'?gameId=1' };
  case 'core-order-retry-reject': return { ...base, mode:'admin', path:'/api/core/admin/orders/'+order+'/retry?gameId=1' };
  case 'access-webhook': return { ...base, origin:access, mode:'access', path:'/internal/config/callback',
    body:{epoch:currentEpoch,revision:currentRevision} };
  case 'access-revision': return { ...base, origin:access, mode:'access', path:'/api/access/revision' };
  case 'access-nodes': return { ...base, origin:access, mode:'access', path:'/api/access/nodes' };
  default: throw new Error('No actual call for QPS_CASE ' + spec.id);
  }
}
export default function () {
  const plan = requestPlan(exec.scenario.iterationInTest);
  const method = spec.route.split(' ')[0];
  const body = plan.body === null ? '' : JSON.stringify(plan.body);
  const headers = signature(plan.mode, method, plan.path, body, plan.extra);
  const response = http.request(method, plan.origin + plan.path, body || null, {
    headers, timeout:'5s', responseCallback:http.expectedStatuses(spec.expected),
    tags:{name:spec.route, endpoint_id:spec.id},
  });
  let ok = response.status === spec.expected;
  if (ok && response.status !== 204 && response.status !== 202) {
    if (spec.id === 'core-payment-duplicate') {
      ok = /^[0-9a-f]{16}$/.test(response.body);
    } else {
      let data = null;
      try { data = response.json(); } catch (_) { ok = false; }
      if (ok && (spec.id.startsWith('service-') ||
          spec.id === 'core-config' || spec.id === 'core-order-register' ||
          spec.id.startsWith('core-order'))) {
        ok = data && data.code === (spec.expected === 409 ? 409 : 0);
      }
      if (ok && (plan.mode === 'admin' || plan.mode === 'sync' || plan.mode === 'access')) {
        ok = data !== null && data !== undefined;
      }
    }
  }
  bad.add(!ok);
}
