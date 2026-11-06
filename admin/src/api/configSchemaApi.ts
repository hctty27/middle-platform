import { api, sdkApi } from './http'

export type ConfigScope = 'base' | 'sdk'
export interface FieldSchema {
  type?: string
  title?: string
  description?: string
  format?: string
  default?: unknown
  enum?: string[]
  writeOnly?: boolean
  properties?: Record<string, FieldSchema>
  additionalProperties?: boolean | FieldSchema
  'x-group'?: string
  'x-order'?: number
  'x-placeholder'?: string
}
export interface ConfigSchema {
  type: 'object'
  properties: Record<string, FieldSchema>
  required?: string[]
  additionalProperties?: boolean | FieldSchema
}
export interface SchemaResponse {
  nodeType: string
  schemaVersion: number
  schema: ConfigSchema
}
const cached = new Map<string, Promise<SchemaResponse>>()

// 统一复用服务端定义，失败后清理缓存，允许重新请求。
export function getConfigSchema(scope: ConfigScope, nodeType: string): Promise<SchemaResponse> {
  const key = scope + ':' + nodeType
  const existing = cached.get(key)
  if (existing) return existing
  const client = scope === 'sdk' ? sdkApi : api
  const request = client.get<SchemaResponse>('/admin/config-schema/' + encodeURIComponent(nodeType))
    .then(response => response.data)
    .catch((error: unknown) => { cached.delete(key); throw error })
  cached.set(key, request)
  return request
}

export function parseConfigObject(raw: string): Record<string, unknown> {
  const value: unknown = JSON.parse(raw)
  if (value === null || Array.isArray(value) || typeof value !== 'object') {
    throw new Error('配置必须是 JSON 对象')
  }
  return value as Record<string, unknown>
}

function validateField(value: unknown, field: FieldSchema, name: string): void {
  if (field.type === 'object') {
    if (value === null || typeof value !== 'object' || Array.isArray(value)) {
      throw new Error(name + ' 必须是 JSON 对象')
    }
    for (const [key, nested] of Object.entries(value as Record<string, unknown>)) {
      const rule = field.properties?.[key] ??
        (typeof field.additionalProperties === 'object' ? field.additionalProperties : undefined)
      if (rule) validateField(nested, rule, name + '.' + key)
    }
    return
  }
  if (field.type === 'string') {
    if (typeof value !== 'string') throw new Error(name + ' 必须是字符串')
    if (field.enum && !field.enum.includes(value)) throw new Error(name + ' 不在可选范围内')
    if (field.format === 'uri' && value.trim()) {
      try {
        const url = new URL(value)
        if (!['http:', 'https:'].includes(url.protocol) || !url.hostname) throw new Error()
      } catch { throw new Error(name + ' 必须是完整的 HTTP/HTTPS 地址') }
    }
  } else if (field.type === 'boolean' && typeof value !== 'boolean') {
    throw new Error(name + ' 必须是布尔值')
  } else if ((field.type === 'number' || field.type === 'integer') &&
    (typeof value !== 'number' || !Number.isFinite(value) ||
     (field.type === 'integer' && !Number.isInteger(value)))) {
    throw new Error(name + ' 必须是数字')
  }
}

export function validateConfigAgainstSchema(raw: string, schema: ConfigSchema): void {
  const value = parseConfigObject(raw)
  for (const name of schema.required ?? []) {
    if (!(name in value)) throw new Error(name + ' 为必填字段')
  }
  for (const [key, item] of Object.entries(value)) {
    const rule = schema.properties[key] ??
      (typeof schema.additionalProperties === 'object' ? schema.additionalProperties : undefined)
    if (rule) validateField(item, rule, key)
  }
}
function lookup(object: Record<string, unknown>, path: string): unknown {
  return path.split('.').reduce<unknown>((parent, key) =>
    parent && typeof parent === 'object' && !Array.isArray(parent)
      ? (parent as Record<string, unknown>)[key] : undefined, object)
}
export function getConfigValue(raw: string, path: string): string {
  const value = lookup(parseConfigObject(raw), path)
  return typeof value === 'string' ? value : ''
}
export function setConfigValue(raw: string, path: string, value: string): string {
  const object = parseConfigObject(raw)
  const parts = path.split('.')
  let parent = object
  for (const key of parts.slice(0, -1)) {
    if (!(key in parent)) {
      if (!value) return JSON.stringify(object, null, 2)
      parent[key] = {}
    }
    const nested = parent[key]
    if (nested === null || Array.isArray(nested) || typeof nested !== 'object') {
      throw new Error('配置节点必须是 JSON 对象')
    }
    parent = nested as Record<string, unknown>
  }
  if (value === '') delete parent[parts[parts.length - 1]]
  else parent[parts[parts.length - 1]] = value
  return JSON.stringify(object, null, 2)
}
export function redactConfig(raw: string, schema: ConfigSchema): string {
  const object = parseConfigObject(raw)
  function mask(target: Record<string, unknown>, properties: Record<string, FieldSchema>) {
    for (const [key, field] of Object.entries(properties)) {
      if (!(key in target)) continue
      if (field.writeOnly) target[key] = '******'
      else if (field.type === 'object' && field.properties &&
        target[key] !== null && typeof target[key] === 'object' && !Array.isArray(target[key])) {
        mask(target[key] as Record<string, unknown>, field.properties)
      }
    }
  }
  mask(object, schema.properties)
  return JSON.stringify(object, null, 2)
}
