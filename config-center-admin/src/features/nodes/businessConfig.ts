/** 业务字段的表单模型。底层仍使用原有 config JSON 字符串。 */
export interface GameConfig {
  gameSecret?: string
}

export interface RegionConfig {
  payNotifyUrl?: string
}

export interface ServerGroupConfig {
  [key: string]: unknown
}

export interface ChannelAppConfig {
  tsKey?: string
  appSecret?: string
}

export type BusinessConfigType = 'game' | 'region' | 'server_group' | 'channel_app'

export interface BusinessConfigField {
  key: string
  label: string
  description: string
  placeholder: string
  secret?: boolean
}

export const businessConfigFields: Record<BusinessConfigType, readonly BusinessConfigField[]> = {
  game: [
    { key: 'gameSecret', label: '游戏主密钥', description: '用于游戏服与 SDK Core 的签名密钥派生，所有环境共享', placeholder: '请输入游戏主密钥', secret: true }
  ],
  region: [
    { key: 'payNotifyUrl', label: '支付发货通知地址', description: 'SDK Core 支付成功后通知游戏服的 HTTP/HTTPS 地址', placeholder: 'https://game.example.com/pay/notify' }
  ],
  server_group: [],
  channel_app: [
    { key: 'tsKey', label: 'TS Key', description: '可通过 SDK Core 响应映射向客户端返回', placeholder: '请输入 TS Key' },
    { key: 'appSecret', label: '平台 App Secret', description: '支付平台回调验签使用的服务端密钥，三个环境共享', placeholder: '请输入平台 App Secret', secret: true }
  ]
}

export function parseBusinessConfig(config: string): Record<string, unknown> {
  const value: unknown = JSON.parse(config)
  if (value === null || Array.isArray(value) || typeof value !== 'object') {
    throw new Error('配置必须是 JSON 对象')
  }
  return value as Record<string, unknown>
}

export function validateBusinessConfig(nodeType: BusinessConfigType, config: string): void {
  const value = parseBusinessConfig(config)
  for (const field of businessConfigFields[nodeType]) {
    if (field.key in value && typeof value[field.key] !== 'string') {
      throw new Error(`${field.label} 必须是字符串`)
    }
  }
}

export function getBusinessConfigValue(config: string, key: string): string {
  const value = parseBusinessConfig(config)[key]
  return typeof value === 'string' ? value : ''
}

/** 只更新指定业务字段，保留所有其他 JSON 属性，清空输入则移除字段。 */
export function setBusinessConfigValue(config: string, key: string, value: string): string {
  const object = parseBusinessConfig(config)
  if (value === '') delete object[key]
  else object[key] = value
  return JSON.stringify(object, null, 2)
}

/** 只读视图不展示已登记的服务端密钥。 */
export function redactBusinessConfig(nodeType: BusinessConfigType, config: string): string {
  try {
    const object = parseBusinessConfig(config)
    for (const field of businessConfigFields[nodeType]) {
      if (field.secret && field.key in object) object[field.key] = '******'
    }
    return JSON.stringify(object, null, 2)
  } catch {
    return '配置格式不正确'
  }
}
