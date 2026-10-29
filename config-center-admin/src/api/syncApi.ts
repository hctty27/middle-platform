import { api } from './http'

export interface RevisionInfo {
  epoch: string
  revision: number
}

export const getRevision = () => api.get<RevisionInfo>('/sync/revision')
