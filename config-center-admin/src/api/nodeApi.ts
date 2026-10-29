import { api } from './http'
import type { NodeForm, NodeMatch, NodePage, NodeRecord, NodeSummary } from '../features/nodes/types'

export const nodeApi = {
  getTreeChildren: (parentId: number) =>
    api.get<NodeSummary[]>('/admin/nodes/tree', { params: { parentId } }),

  search: (keyword: string, signal?: AbortSignal) =>
    api.get<NodeMatch[]>('/admin/nodes/search', { params: { keyword }, signal }),

  getChildren: (params: { parentId: number; page: number; size: number; keyword: string }, signal?: AbortSignal) =>
    api.get<NodePage>('/admin/nodes/children', { params, signal }),

  getNode: (id: number, signal?: AbortSignal) =>
    api.get<NodeRecord>('/admin/nodes/' + id, { signal }),

  createNode: (form: NodeForm) =>
    api.post<NodeRecord>('/admin/nodes', form),

  updateNode: (id: number, changes: Pick<NodeForm, 'name' | 'status' | 'config' | 'remark'>) =>
    api.put<NodeRecord>('/admin/nodes/' + id, changes),

  deleteNode: (id: number) =>
    api.delete('/admin/nodes/' + id)
}
