import { api } from './http'

export interface Subscriber {
  id: number
  code: string
  callbackUrl: string
  enabled: boolean
  acknowledgedRevision: number
  failures: number
  lastError: string | null
}

export interface CreateSubscriberRequest {
  code: string
  callbackUrl: string
  secret: string
}

export const listSubscribers = () => api.get<Subscriber[]>('/admin/subscribers')

export const createSubscriber = (request: CreateSubscriberRequest) =>
  api.post('/admin/subscribers', request)
