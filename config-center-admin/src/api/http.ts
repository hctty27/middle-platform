// 配置中心的 API 基址从运行时配置读取，不需要为不同部署路径重新构建前端。
import axios, { type AxiosInstance } from 'axios'
import { clearAdminSecret, getAdminSecret } from './adminAuth'
import { canonicalQuery, signAdminRequest } from './adminSignature'

declare global {
  interface Window {
    __MIDDLE_PLATFORM_CONFIG__?: {
      apiBaseUrl?: string
      sdkApiBaseUrl?: string
    }
  }
}

const configuredApiBaseUrl = window.__MIDDLE_PLATFORM_CONFIG__?.apiBaseUrl?.trim()
const apiBaseUrl = configuredApiBaseUrl || '/api/config'

const sdkBaseUrl = window.__MIDDLE_PLATFORM_CONFIG__?.sdkApiBaseUrl?.trim()
  || apiBaseUrl.replace(/\/api\/config\/?$/, '/api/core')

export const sdkApi = axios.create({
  baseURL: sdkBaseUrl.replace(/\/+$/, ''),
  timeout: 10000
})

export const api = axios.create({
  baseURL: apiBaseUrl.replace(/\/+$/, ''),
  timeout: 10000
})

// 管理请求复用第三方 SHA-1 协议，签名密钥仅存在页面内存中。
function attachAdminSigning(client: AxiosInstance): void {
  client.interceptors.request.use(async config => {
    const fullURL = new URL(client.getUri(config), window.location.href)
    const match = fullURL.pathname.match(/\/api\/(?:config|core)\/admin(?:\/|$)/)
    if (!match || match.index === undefined) return config

    const secret = getAdminSecret()
    if (!secret) throw new Error('请先输入管理密钥')

    const path = fullURL.pathname.slice(match.index)
    const data = config.data
    if (data !== undefined && data !== null && typeof data !== 'string' && typeof data !== 'object') {
      throw new Error('管理请求体类型不支持')
    }
    if (data instanceof FormData || data instanceof Blob || data instanceof ArrayBuffer) {
      throw new Error('管理接口仅支持 JSON 请求体')
    }
    const body = typeof data === 'string' ? data : data == null ? '' : JSON.stringify(data)
    const headers = await signAdminRequest(secret, config.method || 'get', path, canonicalQuery(fullURL), body)
    config.data = body || undefined
    if (body) config.headers.set('Content-Type', 'application/json')
    config.transformRequest = [(value: unknown) => value]
    for (const [key, value] of Object.entries(headers)) config.headers.set(key, value)
    return config
  })
  client.interceptors.response.use(response => response, error => {
    if (axios.isAxiosError(error) && error.response?.status === 401) clearAdminSecret()
    return Promise.reject(error)
  })
}

attachAdminSigning(api)
attachAdminSigning(sdkApi)

interface ApiErrorPayload {
  message?: unknown
  error?: unknown
}

// 优先显示服务端中文业务错误，网络故障按类型提示；不向界面泄露底层异常。
export function apiErrorMessage(error: unknown, fallback: string): string {
  if (!axios.isAxiosError(error)) return fallback

  const data = error.response?.data as ApiErrorPayload | undefined
  const detail = typeof data?.message === 'string' ? data.message
    : typeof data?.error === 'string' ? data.error : ''
  if (detail.trim()) return `${fallback}：${detail}`

  if (error.code === 'ECONNABORTED' || error.code === 'ETIMEDOUT') {
    return `${fallback}：请求超时`
  }
  if (!error.response) return `${fallback}：网络请求失败，请检查服务地址或跨域配置`
  return `${fallback}（HTTP ${error.response.status}）`
}
