import { api } from './http'

/** RevisionInfo 使用 epoch 标识数据集，revision 表示该数据集内的递增版本。 */
export interface RevisionInfo {
  epoch: string
  revision: number
}

export const getRevision = () => api.get<RevisionInfo>('/sync/revision')
