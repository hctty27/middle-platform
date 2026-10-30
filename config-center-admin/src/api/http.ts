// 所有后台请求统一通过该客户端访问配置中心，避免页面中散落接口前缀和超时配置。
import axios from 'axios'

export const api = axios.create({
  baseURL: '/api/config',
  timeout: 10000
})
