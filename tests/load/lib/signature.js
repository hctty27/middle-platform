import crypto from 'k6/crypto';

function verifySignatureVectors() {
  const gameBody = '{"event":"登录","empty":"","absent":null,"ext":{"name":"角色","numbers":[1,false]}}';
  const gameMessage = 'HITALK-HMAC-SHA256\n' +
    'POST\n' +
    '/api/service/events/test\n' +
    '110006\n' +
    '1793700000\n' +
    '0123456789abcdef\n' +
    gameBody;
  const gameExpected = '26800b27ae8f852c0e423aaa8b3f5a78d20b98b25c23ad5a1f9d30cefc78b47c';
  if (crypto.hmac('sha256', 'secret', gameMessage, 'hex') !== gameExpected) {
    throw new Error('k6 game HMAC does not match the Go signature test vector');
  }

  const subscriberMessage = 'HITALK-HMAC-SHA256\n' +
    'SUBSCRIBER\n' +
    'access-a\n' +
    'GET\n' +
    '/api/config/sync/revision\n' +
    '\n' +
    '1793865600\n' +
    '0123456789abcdef\n';
  const subscriberExpected = '31dcd9424c2a44dcf8683dde1c3feb3b09e914c9b5bfe6ca8e00d041720d7816';
  const subscriberSecret = 'subscriber-secret-0123456789abcdef0123456789';
  if (crypto.hmac('sha256', subscriberSecret, subscriberMessage, 'hex') !== subscriberExpected) {
    throw new Error('k6 subscriber HMAC does not match the Go signature test vector');
  }
}

verifySignatureVectors();

export function newNonce() {
  const bytes = new Uint8Array(crypto.randomBytes(8));
  return Array.from(bytes, function (value) {
    return value.toString(16).padStart(2, '0');
  }).join('');
}

function timestamp() {
  return String(Math.floor(Date.now() / 1000));
}

function userAgent() {
  return 'MiddlePlatform-LoadTest/' + (__ENV.LOADTEST_RUN_ID || 'manual');
}

export function gameHeaders(method, path, appId, secret, body) {
  const ts = timestamp();
  const nonce = newNonce();
  const message = 'HITALK-HMAC-SHA256\n' +
    method.toUpperCase() + '\n' +
    path + '\n' +
    appId + '\n' +
    ts + '\n' +
    nonce + '\n' +
    body;
  return {
    'Content-Type': 'application/json',
    'User-Agent': userAgent(),
    'x-app-id': appId,
    'x-timestamp': ts,
    'x-nonce': nonce,
    'x-signature': crypto.hmac('sha256', secret, message, 'hex'),
  };
}

export function subscriberHeaders(method, path, query, code, secret, body) {
  const ts = timestamp();
  const nonce = newNonce();
  const message = 'HITALK-HMAC-SHA256\n' +
    'SUBSCRIBER\n' +
    code + '\n' +
    method.toUpperCase() + '\n' +
    path + '\n' +
    query + '\n' +
    ts + '\n' +
    nonce + '\n' +
    body;
  return {
    'User-Agent': userAgent(),
    'X-Subscriber-Code': code,
    'X-Timestamp': ts,
    'X-Nonce': nonce,
    'X-Signature': crypto.hmac('sha256', secret, message, 'hex'),
  };
}
