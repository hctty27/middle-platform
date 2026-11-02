// 配置中心的 API 基址从运行时配置读取，不需要为不同部署路径重新构建前端。
import axios from 'axios'

declare global {
  interface Window {
    __MIDDLE_PLATFORM_CONFIG__?: {
      apiBaseUrl?: string
    }
  }
}

const configuredApiBaseUrl = window.__MIDDLE_PLATFORM_CONFIG__?.apiBaseUrl?.trim()
const apiBaseUrl = configuredApiBaseUrl || '/api/config'

export const api = axios.create({
  baseURL: apiBaseUrl.replace(/\/+$/, ''),
  timeout: 10000
})
