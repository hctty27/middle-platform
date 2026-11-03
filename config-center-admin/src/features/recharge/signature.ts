import { md5Upper } from './md5'
// 充值联调签名：严格匹配 SDK Core 当前的游戏登记协议和平台 V1 协议。
const encoder = new TextEncoder()
const appIDPattern = '[A-Za-z0-9][A-Za-z0-9._:-]{0,127}'
const registerPathPattern = new RegExp(`(/api/core/(test|prod|review)/(${appIDPattern})/orders)$`)
const callbackPathPattern = new RegExp(`/api/core/(${appIDPattern})/pay/notify$`)

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
  if (!match) throw new Error('登记 URL 应以 /api/core/{env}/{appid}/orders 结尾')
  return { url: url.toString(), env: match[2], appid: match[3] }
}

export function parseCallbackEndpoint(raw: string): Endpoint {
  const url = parseUrl(raw)
  const match = url.pathname.match(callbackPathPattern)
  if (!match) throw new Error('回调 URL 应以 /api/core/{appid}/pay/notify 结尾')
  return { url: url.toString(), appid: match[1] }
}

function hex(bytes: ArrayBuffer | Uint8Array): string {
  const data = bytes instanceof ArrayBuffer ? new Uint8Array(bytes) : bytes
  return Array.from(data, value => value.toString(16).padStart(2, '0')).join('')
}

async function digest(algorithm: 'SHA-1' | 'SHA-256', text: string): Promise<string> {
  return hex(await crypto.subtle.digest(algorithm, encoder.encode(text)))
}

// 与 Java 游戏服 sortJoinSign(data) + gameSecret + MD5 大写完全一致。
export async function signRegistration(gameSecret: string, rawBody: string, env: string, appid: string,
  timestamp: string, nonce: string): Promise<string> {
  if (!gameSecret.trim()) throw new Error('gameSecret 不能为空')
  const body = JSON.parse(rawBody) as Record<string, unknown>
  if (!body || Array.isArray(body) || typeof body !== 'object') throw new Error('请求体必须是 JSON 对象')
  const data: Record<string, unknown> = {
    action: 'order.register', appid, env, timestamp, nonce
  }
  for (const [key, value] of Object.entries(body)) {
    if (key === 'sign' || key in data) throw new Error('请求体包含签名保留字段')
    data[key] = value
  }
  const javaTrim = (text: string) => text.replace(/^[\\u0000-\\u0020]+|[\\u0000-\\u0020]+$/g, '')
  const parts: string[] = []
  for (const key of Object.keys(data).sort()) {
    const value = data[key]
    if (key === 'sign' || value === null || value === undefined || value === '') continue
    if (typeof value === 'object') throw new Error('签名不支持嵌套 JSON，请先序列化为字符串')
    parts.push(`${key}=${javaTrim(String(value))}`)
  }
  return md5Upper(parts.join('&') + gameSecret)
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
