// 充值联调签名：严格匹配 SDK Core 当前的游戏登记协议和平台 V1 协议。
const encoder = new TextEncoder()
const appIDPattern = '[A-Za-z0-9][A-Za-z0-9._:-]{0,127}'
const registerPathPattern = new RegExp(`(/api/core/(test|prod|review)/(${appIDPattern})/orders)$`)
const callbackPathPattern = new RegExp(`/api/core/(${appIDPattern})/pay/notify$`)

export interface Endpoint {
  url: string
  appid: string
  signingPath?: string
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
  if (!match) throw new Error('登记 URL 应以 /api/core/{env}/{appid}/orders 结尾')
  return { url: url.toString(), signingPath: match[1], env: match[2], appid: match[3] }
}

export function parseCallbackEndpoint(raw: string): Endpoint {
  const url = parseUrl(raw)
  const match = url.pathname.match(callbackPathPattern)
  if (!match) throw new Error('回调 URL 应以 /api/core/{appid}/pay/notify 结尾')
  return { url: url.toString(), appid: match[1] }
}

export function resolveSigningPath(inferred: string, override: string): string {
  const path = override.trim() || inferred
  if (!path.startsWith('/') || path.includes('?') || path.includes('#') || path.includes('://')) {
    throw new Error('签名路径必须是 SDK Core 实际收到的绝对路径，不含域名和查询参数')
  }
  return path
}

function hex(bytes: ArrayBuffer | Uint8Array): string {
  const data = bytes instanceof ArrayBuffer ? new Uint8Array(bytes) : bytes
  return Array.from(data, value => value.toString(16).padStart(2, '0')).join('')
}

async function digest(algorithm: 'SHA-1' | 'SHA-256', text: string): Promise<string> {
  return hex(await crypto.subtle.digest(algorithm, encoder.encode(text)))
}

export async function signRegistration(gameSecret: string, rawBody: string, signingPath: string, timestamp: string): Promise<string> {
  if (!crypto?.subtle) throw new Error('当前页面不支持 Web Crypto，请使用 HTTPS 或 localhost 访问管理后台')
  const encoded = gameSecret.trim()
  if (!/^[A-Za-z0-9+/]{43}=$/.test(encoded)) throw new Error('gameSecret 必须为 32 字节密钥的标准 Base64 编码')
  const secret = Uint8Array.from(atob(encoded), char => char.charCodeAt(0))
  if (secret.byteLength !== 32 || btoa(String.fromCharCode(...secret)) !== encoded) {
    throw new Error('gameSecret 必须是 32 字节密钥的严格标准 Base64 编码')
  }
  const master = await crypto.subtle.importKey('raw', secret, 'HKDF', false, ['deriveKey'])
  const key = await crypto.subtle.deriveKey({
    name: 'HKDF', hash: 'SHA-256', salt: new Uint8Array(0),
    info: encoder.encode('middle-platform:sdk-core:order-register:v1')
  }, master, { name: 'HMAC', hash: 'SHA-256', length: 256 }, false, ['sign'])
  const bodyHash = await digest('SHA-256', rawBody)
  const canonical = ['POST', signingPath, timestamp, bodyHash].join('\n')
  return hex(await crypto.subtle.sign('HMAC', key, encoder.encode(canonical)))
}

export function generateNonce(): string {
  if (!crypto?.getRandomValues) throw new Error('当前页面不支持安全随机数，请使用 HTTPS 或 localhost')
  return hex(crypto.getRandomValues(new Uint8Array(8)))
}

export async function signPlatformV1(appSecret: string, orderNo: string, gameOrderID: string, nonce: string, timestamp: string): Promise<string> {
  if (!crypto?.subtle) throw new Error('当前页面不支持 Web Crypto，请使用 HTTPS 或 localhost')
  if (!appSecret) throw new Error('请填写平台密钥 appSecret')
  const values = [orderNo, gameOrderID, appSecret, nonce, timestamp].sort()
  return digest('SHA-1', values.join('_'))
}
