export type NodeType = 'game' | 'region' | 'server_group' | 'channel_app'
export type RegionEnvType = 'test' | 'review' | 'production'
export const regionEnvLabels: Record<RegionEnvType, string> = {
  test: '测试服', review: '提审服', production: '正式服'
}

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
export interface NodeRecord extends Omit<NodeSummary, 'leaf'> {
  config: string
  remark: string | null
}
export interface NodePage {
  items: NodeSummary[]
  total: number
  page: number
  size: number
}
export interface NodeMatch {
  node: NodeSummary
  path: NodeSummary[]
}


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
