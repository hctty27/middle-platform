/** NodeType 表示固定的配置层级：游戏、大区、区服组、渠道应用。 */
export type NodeType = 'game' | 'region' | 'server_group' | 'channel_app'
/** RegionEnvType 表示大区的运行用途，不是独立的配置节点层级。 */
export type RegionEnvType = 'test' | 'review' | 'prod'
export const regionEnvLabels: Record<RegionEnvType, string> = {
  test: '测试服', review: '提审服', prod: '正式服'
}

/** NodeSummary 是目录树与列表使用的轻量节点，不包含完整 JSON 配置。 */
export interface NodeSummary {
  id: number
  parentId: number
  nodeType: NodeType
  envType: RegionEnvType | null
  code: string
  name: string
  status: number
  leaf: boolean
}
/** NodeRecord 是详情和编辑使用的完整节点信息。 */
export interface NodeRecord extends Omit<NodeSummary, 'leaf'> {
  config: string
  remark: string | null
}
/** NodePage 承载直属子项的分页结果。 */
export interface NodePage {
  items: NodeSummary[]
  total: number
  page: number
  size: number
}
/** NodeMatch 同时返回命中节点及其祖先路径，供搜索定位目录树。 */
export interface NodeMatch {
  node: NodeSummary
  path: NodeSummary[]
}


/** NodeForm 统一创建和编辑抽屉的数据结构。 */
export interface NodeForm {
  parentId: number
  nodeType: NodeType
  envType: RegionEnvType | null
  code: string
  name: string
  status: number
  config: string
  remark: string
}
