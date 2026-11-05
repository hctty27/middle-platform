// 配置中心的 API 基址从运行时配置读取，不需要为不同部署路径重新构建前端。
import axios from 'axios'

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
