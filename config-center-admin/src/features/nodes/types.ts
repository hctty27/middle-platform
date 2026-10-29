export type NodeType = 'game' | 'region' | 'server_group' | 'channel' | 'sdk_app'

export interface NodeSummary {
  id: number
  parentId: number
  nodeType: NodeType
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
  code: string
  name: string
  status: number
  config: string
  remark: string
}
