import { api } from './http'
import type { NodeForm, NodeMatch, NodePage, NodeRecord, NodeSummary } from '../features/nodes/types'

/** nodeApi 封装节点树、搜索、分页和增删改接口；可取消的查询透传 AbortSignal。 */
export const nodeApi = {
  // 懒加载只查询直属子节点，避免一次取回所有渠道应用。
  getTreeChildren: (parentId: number) =>
    api.get<NodeSummary[]>('/admin/nodes/tree', { params: { parentId } }),

  search: (keyword: string, signal?: AbortSignal) =>
    api.get<NodeMatch[]>('/admin/nodes/search', { params: { keyword }, signal }),

  getChildren: (params: { parentId: number; page: number; size: number; keyword: string; envType?: string }, signal?: AbortSignal) =>
    api.get<NodePage>('/admin/nodes/' + params.parentId + '/children', {
      params: { page: params.page, size: params.size, keyword: params.keyword, envType: params.envType }, signal
    }),

  getNode: (id: number, signal?: AbortSignal) =>
    api.get<NodeRecord>('/admin/nodes/' + id, { signal }),

  createNode: (form: NodeForm) =>
    api.post<NodeRecord>('/admin/nodes', form),

  // 更新操作只提交可修改字段，不改变节点所属层级和编码。
  updateNode: (id: number, changes: Pick<NodeForm, 'name' | 'status' | 'config' | 'remark' | 'envType'>) =>
    api.put<NodeRecord>('/admin/nodes/' + id, changes),

  deleteNode: (id: number) =>
    api.delete('/admin/nodes/' + id)
}
