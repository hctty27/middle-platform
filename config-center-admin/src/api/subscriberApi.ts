import { api } from './http'

/** Subscriber 包含接入方订阅信息和服务器记录的回调投递状态。 */
export interface Subscriber {
  id: number
  code: string
  callbackUrl: string
  enabled: boolean
  acknowledgedRevision: number
  failures: number
  lastError: string | null
}

/** CreateSubscriberRequest 的密钥仅在注册时提交，不从查询接口返回。 */
export interface CreateSubscriberRequest {
  code: string
  callbackUrl: string
  secret: string
}

export const listSubscribers = () => api.get<Subscriber[]>('/admin/subscribers')

export const createSubscriber = (request: CreateSubscriberRequest) =>
  api.post('/admin/subscribers', request)
