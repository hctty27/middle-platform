// 订单登记模拟签名：严格匹配 SDK Core 当前的游戏登记协议。
const encoder = new TextEncoder()
const appIDPattern = '[A-Za-z0-9][A-Za-z0-9._:-]{0,127}'
const registerPathPattern = new RegExp(`(/api/core/orders/(test|prod|review)/(${appIDPattern}))$`)

export interface Endpoint {
  url: string
  appid: string
  env?: string
}

function parseUrl(raw: string): URL {
  let url: URL
  try { url = new URL(raw.trim()) }
  catch { throw new Error('请输入完整的 HTTP/HTTPS 请求 URL') }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
    throw new Error('URL 仅支持 HTTP/HTTPS，不允许账户信息、查询参数或片段')
  }
  return url
}

export function parseRegisterEndpoint(raw: string): Endpoint {
  const url = parseUrl(raw)
  const match = url.pathname.match(registerPathPattern)
  if (!match) throw new Error('登记 URL 应以 /api/core/orders/{env}/{appid} 结尾')
  return { url: url.toString(), env: match[2], appid: match[3] }
}

function hex(bytes: ArrayBuffer | Uint8Array): string {
  const data = bytes instanceof ArrayBuffer ? new Uint8Array(bytes) : bytes
  return Array.from(data, value => value.toString(16).padStart(2, '0')).join('')
}

async function digest(algorithm: 'SHA-1' | 'SHA-256', text: string): Promise<string> {
  return hex(await crypto.subtle.digest(algorithm, encoder.encode(text)))
}

// 游戏侧签名与平台一致：仅签密钥、AppID、时间戳、Nonce 和全部 Body 非空值。
export async function signRegistration(
  gameSecret: string, rawBody: string, appid: string, timestamp: string, nonce: string
): Promise<string> {
  if (!crypto?.subtle) throw new Error('当前页面不支持 Web Crypto，请使用 HTTPS 或 localhost 访问管理后台')
  if (!gameSecret.trim()) throw new Error('gameSecret 不能为空')
  const body: Record<string, unknown> = JSON.parse(rawBody)
  if (!body || typeof body !== 'object' || Array.isArray(body)) {
    throw new Error('请求体必须为 JSON 对象')
  }
  const values: string[] = [gameSecret, appid, timestamp, nonce]
  for (const value of Object.values(body)) {
    if (value === null || value === undefined || value === '') continue
    values.push(typeof value === 'object' ? JSON.stringify(value) : String(value))
  }
  return digest('SHA-1', values.filter(value => value !== '').sort().join('_'))
}

export function generateNonce(): string {
  if (!crypto?.getRandomValues) throw new Error('当前页面不支持安全随机数，请使用 HTTPS 或 localhost')
  return hex(crypto.getRandomValues(new Uint8Array(8)))
}
