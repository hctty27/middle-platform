const encoder = new TextEncoder()

function toHex(bytes: ArrayBuffer | Uint8Array): string {
  const data = bytes instanceof ArrayBuffer ? new Uint8Array(bytes) : bytes
  return Array.from(data, value => value.toString(16).padStart(2, '0')).join('')
}

async function signSorted(values: string[], filterEmpty: boolean): Promise<string> {
  if (!globalThis.crypto?.subtle) {
    throw new Error('当前页面不支持 Web Crypto，请使用 HTTPS 或 localhost 访问管理后台')
  }
  const items = filterEmpty ? values.filter(value => value !== '') : values
  items.sort()
  return toHex(await globalThis.crypto.subtle.digest('SHA-1', encoder.encode(items.join('_'))))
}

export function generateNonce(): string {
  if (!globalThis.crypto?.getRandomValues) {
    throw new Error('当前页面不支持安全随机数，请使用 HTTPS 或 localhost')
  }
  return toHex(globalThis.crypto.getRandomValues(new Uint8Array(8)))
}

export function signServiceRequest(
  appSecret: string,
  appId: string,
  timestamp: string,
  nonce: string,
  businessValues: string[]
): Promise<string> {
  if (!appSecret) throw new Error('当前渠道应用未配置 appSecret')
  return signSorted([appSecret, appId, timestamp, nonce, ...businessValues], true)
}

export function signPaymentCallback(
  appSecret: string,
  orderNo: string,
  gameOrderId: string,
  nonce: string,
  timestamp: string
): Promise<string> {
  if (!appSecret) throw new Error('当前渠道应用未配置 appSecret')
  return signSorted([orderNo, gameOrderId, appSecret, nonce, timestamp], false)
}
