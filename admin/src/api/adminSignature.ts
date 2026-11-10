// 与 shared/signature/admin.go 一致的 HMAC-SHA256 管理请求签名规则。
import { hmac } from '@noble/hashes/hmac.js'
import { sha256 } from '@noble/hashes/sha2.js'

const encoder = new TextEncoder()

function toHex(data: ArrayBuffer | Uint8Array): string {
  return Array.from(data instanceof ArrayBuffer ? new Uint8Array(data) : data,
    byte => byte.toString(16).padStart(2, '0')).join('')
}

function byteOrder(a: string, b: string): number {
  const left = encoder.encode(a)
  const right = encoder.encode(b)
  for (let i = 0; i < Math.min(left.length, right.length); i++) {
    if (left[i] !== right[i]) return left[i] - right[i]
  }
  return left.length - right.length
}

// 与 Go url.Values.Encode 保持一致：排序键名，同键保留原有值顺序。
function escapeQuery(value: string): string {
  return encodeURIComponent(value)
    .replace(/[!'()*]/g, char => '%' + char.charCodeAt(0).toString(16).toUpperCase())
    .replace(/%20/g, '+')
}

export function canonicalQuery(url: URL): string {
  const values = new Map<string, string[]>()
  url.searchParams.forEach((value, key) => {
    const existing = values.get(key) ?? []
    existing.push(value)
    values.set(key, existing)
  })
  return Array.from(values.keys()).sort(byteOrder)
    .flatMap(key => values.get(key)!.map(value => escapeQuery(key) + '=' + escapeQuery(value)))
    .join('&')
}

function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const size = parts.reduce((total, part) => total + part.length, 0)
  const result = new Uint8Array(size)
  let offset = 0
  for (const part of parts) {
    result.set(part, offset)
    offset += part.length
  }
  return result
}

async function signHmac(secret: string, message: Uint8Array): Promise<string> {
  const keyBytes = encoder.encode(secret)
  if (globalThis.crypto?.subtle) {
    const key = await globalThis.crypto.subtle.importKey(
      'raw', keyBytes, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']
    )
    return toHex(await globalThis.crypto.subtle.sign('HMAC', key, new Uint8Array(message)))
  }
  return toHex(hmac(sha256, keyBytes, message))
}

export async function signAdminRequest(
  secret: string, method: string, path: string, query: string, body: string
): Promise<Record<string, string>> {
  if (!globalThis.crypto?.getRandomValues) {
    throw new Error('当前浏览器不支持安全随机数，无法生成签名')
  }
  if (new TextEncoder().encode(secret).length < 32) {
    throw new Error('管理密钥至少需要 32 字节')
  }
  const timestamp = String(Math.floor(Date.now() / 1000))
  const nonce = toHex(globalThis.crypto.getRandomValues(new Uint8Array(8)))
  const prefix = encoder.encode(
    'HITALK-HMAC-SHA256\n' + method.toUpperCase() + '\n' +
    path + '\n' + query + '\n' + timestamp + '\n' + nonce + '\n'
  )
  const signature = await signHmac(secret, concatBytes(prefix, encoder.encode(body)))
  return {
    'X-Timestamp': timestamp,
    'X-Nonce': nonce,
    'X-Signature': signature
  }
}
