import assert from 'node:assert/strict'
import { createHmac, webcrypto } from 'node:crypto'
import { test } from 'node:test'
import { canonicalQuery, signAdminRequest } from '../src/api/adminSignature.ts'

const originalCrypto = Object.getOwnPropertyDescriptor(globalThis, 'crypto')
const originalNow = Date.now
const nonce = '0102030405060708'

function setCrypto(value) {
  Object.defineProperty(globalThis, 'crypto', { configurable: true, value })
}

function expectedSignature(secret, method, path, query, body) {
  const prefix = `HITALK-HMAC-SHA256\n${method}\n${path}\n${query}\n1700000000\n${nonce}\n`
  return createHmac('sha256', secret).update(prefix, 'utf8').update(body, 'utf8').digest('hex')
}

test('admin signatures match with and without SubtleCrypto', async () => {
  Date.now = () => 1700000000000
  const random = array => { array.set([1, 2, 3, 4, 5, 6, 7, 8]); return array }
  try {
    for (const body of ['', JSON.stringify({ name: '测试', content: 'a'.repeat(200) })]) {
      const secret = 'secret-管理-key-0123456789abcdef0123456789'
      const path = '/api/config/admin/nodes/tree'
      const query = canonicalQuery(new URL('http://example.test/?z=测试&parentId=0'))
      const expected = expectedSignature(secret, 'POST', path, query, body)
      for (const subtle of [webcrypto.subtle, undefined]) {
        setCrypto({ getRandomValues: random, subtle })
        const result = await signAdminRequest(secret, 'post', path, query, body)
        assert.deepEqual(result, {
          'X-Timestamp': '1700000000', 'X-Nonce': nonce, 'X-Signature': expected
        })
      }
    }
  } finally {
    Date.now = originalNow
    if (originalCrypto) Object.defineProperty(globalThis, 'crypto', originalCrypto)
    else delete globalThis.crypto
  }
})

test('signing fails when secure random numbers are unavailable', async () => {
  try {
    setCrypto(undefined)
    await assert.rejects(signAdminRequest('secret', 'get', '/api/config/admin/test', '', ''), /安全随机数/)
  } finally {
    if (originalCrypto) Object.defineProperty(globalThis, 'crypto', originalCrypto)
    else delete globalThis.crypto
  }
})

test('signing rejects short management keys', async () => {
  try {
    setCrypto({ getRandomValues: array => array.fill(1) })
    await assert.rejects(signAdminRequest('short-secret', 'get', '/api/config/admin/test', '', ''), /32 字节/)
  } finally {
    if (originalCrypto) Object.defineProperty(globalThis, 'crypto', originalCrypto)
    else delete globalThis.crypto
  }
})
