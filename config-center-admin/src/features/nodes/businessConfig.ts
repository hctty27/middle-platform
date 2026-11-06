/** 业务字段的表单模型。底层仍使用原有 config JSON 字符串。 */
export interface GameConfig {
  gameSecret?: string
  serviceAdapters?: { user_check?: string; role_query?: string; server_query?: string; role_control?: string; [key: string]: unknown }
}

export interface RegionConfig {
  payNotifyUrl?: string
  userCheckUrl?: string
  roleQueryUrl?: string
  serverQueryUrl?: string
  roleControlUrl?: string
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
  defaultValue?: string
}

export const businessConfigFields: Record<BusinessConfigType, readonly BusinessConfigField[]> = {
  game: [
    { key: 'gameSecret', label: '游戏主密钥', description: '游戏服与 SDK Core/Service 共用的服务端签名密钥，所有环境共享', placeholder: '请输入游戏主密钥', secret: true },
    { key: 'serviceAdapters.user_check', label: '用户核验适配器', description: '可选。留空自动使用标准适配器，只有特殊游戏才需要填写', placeholder: '留空默认 standard', defaultValue: 'standard' },
    { key: 'serviceAdapters.role_query', label: '角色查询适配器', description: '可选。留空自动使用标准适配器，特殊游戏可以配置已注册的实现', placeholder: '留空默认 standard', defaultValue: 'standard' },
    { key: 'serviceAdapters.server_query', label: '区服查询适配器', description: '可选。留空自动使用标准适配器，特殊游戏可选择已注册实现', placeholder: '留空默认 standard', defaultValue: 'standard' },
    { key: 'serviceAdapters.role_control', label: '封解禁适配器', description: '可选。留空自动使用标准适配器，特殊游戏可指定已注册实现', placeholder: '留空默认 standard', defaultValue: 'standard' }
  ],
  region: [
    { key: 'payNotifyUrl', label: '支付发货通知地址', description: 'SDK Core 支付成功后通知游戏服的 HTTP/HTTPS 地址', placeholder: 'https://game.example.com/pay/notify' },
    { key: 'userCheckUrl', label: '用户核验地址', description: 'SDK Service 核验玩家时请求的完整游戏服 HTTP/HTTPS 地址', placeholder: 'https://game.example.com/internal/user/check' },
    { key: 'roleQueryUrl', label: '角色查询地址', description: 'SDK Service 查询游戏角色时请求的完整 HTTP/HTTPS 地址', placeholder: 'https://game.example.com/internal/role/query' },
    { key: 'serverQueryUrl', label: '区服查询地址', description: 'SDK Service 实时分页查询游戏区服的完整 HTTP/HTTPS 地址', placeholder: 'https://game.example.com/internal/server/query' },
    { key: 'roleControlUrl', label: '封解禁地址', description: 'SDK Service 封号、解封、禁言、解禁言共用的游戏 HTTP/HTTPS 地址', placeholder: 'https://game.example.com/internal/role/control' }
  ],
  server_group: [],
  channel_app: [
    { key: 'tsKey', label: 'TS Key', description: 'SDK Core 公开配置接口直接返回此字段（可选）', placeholder: '请输入 TS Key' },
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

function getPath(object: Record<string, unknown>, key: string): unknown {
  return key.split('.').reduce<unknown>((parent, part) =>
    parent && typeof parent === 'object' && !Array.isArray(parent)
      ? (parent as Record<string, unknown>)[part] : undefined, object)
}

function setPath(object: Record<string, unknown>, key: string, value: string): void {
  const segments = key.split('.')
  let parent = object
  for (const part of segments.slice(0, -1)) {
    if (parent[part] === undefined && value !== '') parent[part] = {}
    const nested = parent[part]
    if (nested === undefined && value === '') return
    if (nested === null || typeof nested !== 'object' || Array.isArray(nested)) {
      throw new Error('配置节点必须是 JSON 对象')
    }
    parent = nested as Record<string, unknown>
  }
  const leaf = segments[segments.length - 1]
  if (value === '') delete parent[leaf]
  else parent[leaf] = value
  for (const part of segments.slice(0, -1)) {
    const nested = object[part]
    if (nested && typeof nested === 'object' && !Array.isArray(nested)
        && Object.keys(nested).length === 0) delete object[part]
    break
  }
}

export function validateBusinessConfig(nodeType: BusinessConfigType, config: string): void {
  const value = parseBusinessConfig(config)
  if (nodeType === 'game' && 'serviceAdapters' in value &&
      (value.serviceAdapters === null || Array.isArray(value.serviceAdapters) ||
      typeof value.serviceAdapters !== 'object')) {
    throw new Error('serviceAdapters 必须是 JSON 对象')
  }
  for (const field of businessConfigFields[nodeType]) {
    const fieldValue = getPath(value, field.key)
    if (fieldValue !== undefined && typeof fieldValue !== 'string') {
      throw new Error(`${field.label} 必须是字符串`)
    }
  }
}

export function getBusinessConfigValue(config: string, key: string): string {
  const value = getPath(parseBusinessConfig(config), key)
  return typeof value === 'string' ? value : ''
}

/** 只更新指定业务字段，保留其他 JSON 属性。 */
export function setBusinessConfigValue(config: string, key: string, value: string): string {
  const object = parseBusinessConfig(config)
  setPath(object, key, value)
  return JSON.stringify(object, null, 2)
}

/** 只读视图不展示已登记的服务端密钥。 */
export function redactBusinessConfig(nodeType: BusinessConfigType, config: string): string {
  try {
    const object = parseBusinessConfig(config)
    for (const field of businessConfigFields[nodeType]) {
      if (field.secret && getPath(object, field.key) !== undefined) setPath(object, field.key, '******')
    }
    return JSON.stringify(object, null, 2)
  } catch {
    return '配置格式不正确'
  }
}
