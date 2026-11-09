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

/** UpdateSubscriberRequest 不允许修改接入方编码；secret 为空时保持原密钥。 */
export interface UpdateSubscriberRequest {
  callbackUrl: string
  secret?: string
}

export const listSubscribers = () => api.get<Subscriber[]>('/admin/subscribers')

export const createSubscriber = (request: CreateSubscriberRequest) =>
  api.post('/admin/subscribers', request)

export const updateSubscriber = (id: number, request: UpdateSubscriberRequest) =>
  api.put<void>('/admin/subscribers/' + id, request)

export const setSubscriberEnabled = (id: number, enabled: boolean) =>
  api.patch<void>('/admin/subscribers/' + id + '/enabled', { enabled })

export const deleteSubscriber = (id: number) =>
  api.delete<void>('/admin/subscribers/' + id)
