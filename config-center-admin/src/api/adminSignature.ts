// 与 shared/signature/admin.go 一致的管理请求签名规则。
const encoder = new TextEncoder()

function toHex(data: ArrayBuffer | Uint8Array): string {
  return Array.from(data instanceof ArrayBuffer ? new Uint8Array(data) : data,
    byte => byte.toString(16).padStart(2, '0')).join('')
}

async function digest(algorithm: 'SHA-1' | 'SHA-256', text: string): Promise<string> {
  return toHex(await crypto.subtle.digest(algorithm, encoder.encode(text)))
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

export async function signAdminRequest(
  secret: string, method: string, path: string, query: string, body: string
): Promise<Record<string, string>> {
  if (!crypto?.subtle || !crypto?.getRandomValues) {
    throw new Error('当前浏览器无法生成签名，请使用 HTTPS 或 localhost 访问')
  }
  const timestamp = String(Math.floor(Date.now() / 1000))
  const nonce = toHex(crypto.getRandomValues(new Uint8Array(8)))
  const bodyHash = await digest('SHA-256', body)
  const values = [
    secret,
    'method:' + method.toUpperCase(),
    'path:' + path,
    'query:' + query,
    'body:' + bodyHash,
    'timestamp:' + timestamp,
    'nonce:' + nonce
  ].filter(Boolean).sort(byteOrder)
  const signature = await digest('SHA-1', values.join('_'))
  return {
    'X-Timestamp': timestamp,
    'X-Nonce': nonce,
    'X-Signature': signature
  }
}
