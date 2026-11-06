import { nextTick, onBeforeUnmount, reactive, ref, computed } from 'vue'
import axios from 'axios'
import { nodeApi } from '../../../api/nodeApi'
import { getConfigSchema, validateConfigAgainstSchema } from '../../../api/configSchemaApi'
import { apiErrorMessage } from '../../../api/http'
import type { NodeType, NodeSummary, NodeRecord, NodePage, NodeMatch, RegionEnvType, GameMarketType } from '../types'
import { ElMessage, ElMessageBox, ElTree } from 'element-plus'
import type { LoadFunction } from 'element-plus'
import { registerUnsavedChecker } from '../../../app/unsavedChanges'

/**
 * useNodeWorkspace 集中管理配置树、搜索、分页、节点编辑及刷新动作。
 * 通过请求取消和递增序号避免快速切换节点时旧响应覆盖新状态。
 */
export function useNodeWorkspace(emit: (event: 'changed') => void) {
const labels: Record<NodeType, string> = {
  game: '游戏', region: '大区', server_group: '区服组', channel_app: '渠道应用'
}
const nextType: Partial<Record<NodeType, NodeType>> = {
  game: 'region', region: 'server_group'
}
const treeProps = { label: 'name', isLeaf: 'leaf' }
const treeRef = ref<InstanceType<typeof ElTree> | null>(null)
const treeScrollRef = ref<HTMLElement | null>(null)
const treeVersion = ref(0)
// 展开节点、懒加载状态与滚动位置分离，避免刷新整个树时丢失用户视图。
const expandedKeys = ref<number[]>([])
// 仅在重建树时恢复展开状态，避免默认展开属性响应点击事件。
const restoredExpandedKeys = ref<number[]>([])
const searchWord = ref('')
const searchMatches = ref<NodeMatch[]>([])
const searchLoading = ref(false)
const selected = ref<NodeSummary | null>(null)
const breadcrumbs = ref<NodeSummary[]>([])
const children = ref<NodeSummary[]>([])
const childTotal = ref(0)
const childPage = ref(1)
const childSize = 20
const childSearch = ref('')
const regionFilter = ref<RegionEnvType | ''>('')
const childrenLoading = ref(false)
const drawerVisible = ref(false)
const creating = ref(false)
const envTypeLocked = ref(false)
const editorNode = ref<NodeSummary | null>(null)
const detailLoading = ref(false)
const saving = ref(false)
const form = reactive({
  parentId: 0, nodeType: 'game' as NodeType, code: '', name: '',
  status: 1, envType: null as RegionEnvType | null, marketType: 'domestic' as GameMarketType | null, config: '{}', remark: ''
})
const nextLabel = computed(() => {
  const type = selected.value && nextType[selected.value.nodeType]
  return type ? labels[type] : '子项'
})
const editorTitle = computed(() => creating.value ? '新增' + labels[form.nodeType] : '编辑配置节点')
let originalForm = ''
const unregisterUnsaved = registerUnsavedChecker(
  () => drawerVisible.value && JSON.stringify(form) !== originalForm,
  () => { drawerVisible.value = false; originalForm = JSON.stringify(form) }
)
let searchTimer: ReturnType<typeof setTimeout> | undefined
let childTimer: ReturnType<typeof setTimeout> | undefined
// 异步请求序号与 AbortController 配合，确保最后一次用户操作拥有更新权。
let searchSequence = 0
let detailSequence = 0
let childSequence = 0
let searchController: AbortController | undefined
let childController: AbortController | undefined
let detailController: AbortController | undefined

function toSummary(node: NodeRecord): NodeSummary {
  return { id: node.id, parentId: node.parentId, nodeType: node.nodeType,
    code: node.code, name: node.name, status: node.status, envType: node.envType, marketType: node.marketType,
    leaf: ['server_group', 'channel_app'].includes(node.nodeType) }
}

function resetForm(parentId: number, nodeType: NodeType) {
  envTypeLocked.value = false
  Object.assign(form, { parentId, nodeType, code: '', name: '', status: 1, envType: null, marketType: nodeType === 'game' ? 'domestic' : null,
    config: '{}', remark: '' })
  originalForm = JSON.stringify(form)
}

function setForm(node: NodeRecord) {
  envTypeLocked.value = node.nodeType === 'region' && !!node.envType
  Object.assign(form, node, { remark: node.remark ?? '' })
  try { form.config = JSON.stringify(JSON.parse(node.config), null, 2) }
  catch { form.config = node.config }
  originalForm = JSON.stringify(form)
}

async function confirmDiscard(): Promise<boolean> {
  if (!drawerVisible.value || JSON.stringify(form) === originalForm) return true
  try {
    await ElMessageBox.confirm('当前修改尚未保存，确定放弃吗？', '未保存的修改', { type: 'warning' })
    return true
  } catch { return false }
}

async function beforeDrawerClose(done: () => void) {
  if (saving.value) return
  if (await confirmDiscard()) {
    ++detailSequence
    detailController?.abort()
    done()
  }
}

function closeEditor() {
  void beforeDrawerClose(() => { drawerVisible.value = false })
}

// Element Plus 目录树按需读取直接子节点，避免一次性构建整棵树。
const loadTree: LoadFunction = async (node, resolve, reject) => {
  const parentId = node.level === 0 ? 0 : (node.data as NodeSummary).id
  try {
    const result = await nodeApi.getTreeChildren(parentId)
    resolve(result.data)
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '配置目录加载失败，请重新展开'))
    reject()
  }
}

function onExpand(node: NodeSummary) {
  if (!expandedKeys.value.includes(node.id)) expandedKeys.value = [...expandedKeys.value, node.id]
}
// 收起父节点时清理后代展开标记，避免刷新后错误恢复展开状态。
function onCollapse(node: NodeSummary) {
  const collapsedKeys = new Set<number>([node.id])
  const descendants = [...(treeRef.value?.getNode(node.id)?.childNodes ?? [])]
  while (descendants.length) {
    const child = descendants.pop()!
    collapsedKeys.add((child.data as NodeSummary).id)
    descendants.push(...child.childNodes)
    child.collapse()
  }
  expandedKeys.value = expandedKeys.value.filter(id => !collapsedKeys.has(id))
}

// 刷新前记录展开路径和滚动位置，重建后恢复，避免视图跳回顶部。
async function reloadTree() {
  const scrollTop = treeScrollRef.value?.scrollTop ?? 0
  restoredExpandedKeys.value = [...expandedKeys.value]
  treeVersion.value++
  await nextTick()
  if (treeScrollRef.value) treeScrollRef.value.scrollTop = scrollTop
}

async function refreshTreeParent(parentId: number) {
  if (parentId === 0) {
    await reloadTree()
    return
  }
  const parent = treeRef.value?.getNode(parentId)
  if (!parent || parent.data.nodeType === 'server_group') return
  try {
    const result = await nodeApi.getTreeChildren(parentId)
    treeRef.value?.updateKeyChildren(parentId, result.data)
  } catch (error) {
    ElMessage.warning(apiErrorMessage(error, '操作已成功，但目录刷新失败，请手动刷新'))
  }
}

// 全局检索与目录懒加载相互独立，并忽略过期搜索结果。
async function searchAll() {
  const keyword = searchWord.value.trim()
  const sequence = ++searchSequence
  searchController?.abort()
  if (!keyword) {
    searchMatches.value = []
    searchLoading.value = false
    return
  }
  const controller = new AbortController()
  searchController = controller
  searchLoading.value = true
  try {
    const result = await nodeApi.search(keyword, controller.signal)
    if (sequence === searchSequence) searchMatches.value = result.data
  } catch (error) {
    if (sequence === searchSequence && !axios.isCancel(error)) ElMessage.error(apiErrorMessage(error, '全局搜索失败'))
  } finally {
    if (sequence === searchSequence) searchLoading.value = false
  }
}

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchController?.abort()
  ++searchSequence
  searchMatches.value = []
  if (!searchWord.value.trim()) {
    void searchAll()
    return
  }
  searchTimer = setTimeout(() => void searchAll(), 250)
}

// 直属子项独立分页；用户切换节点时取消上一节点的请求。
async function loadChildren() {
  const node = selected.value
  const sequence = ++childSequence
  childController?.abort()
  if (!node || node.nodeType === 'server_group' || node.nodeType === 'channel_app') {
    children.value = []
    childTotal.value = 0
    childrenLoading.value = false
    return
  }
  const controller = new AbortController()
  childController = controller
  childrenLoading.value = true
  try {
    const result = await nodeApi.getChildren({ parentId: node.id, page: childPage.value, size: childSize, keyword: childSearch.value,
      envType: node.nodeType === 'game' ? regionFilter.value : '' }, controller.signal)
    if (sequence !== childSequence) return
    children.value = result.data.items
    childTotal.value = result.data.total
  } catch (error) {
    if (sequence === childSequence && !axios.isCancel(error)) ElMessage.error(apiErrorMessage(error, '子项加载失败'))
  } finally {
    if (sequence === childSequence) childrenLoading.value = false
  }
}

async function choose(node: NodeSummary, path: NodeSummary[]) {
  if (!(await confirmDiscard())) return false
  ++detailSequence
  detailController?.abort()
  drawerVisible.value = false
  selected.value = node
  breadcrumbs.value = [...path]
  if (childTimer) clearTimeout(childTimer)
  childPage.value = 1
  childSearch.value = ''
  regionFilter.value = ''
  const tree = treeRef.value
  tree?.setCurrentKey(tree.getNode(node.id) ? node.id : undefined)
  void loadChildren()
  return true
}

function onTreeClick(node: NodeSummary, rawTreeNode: unknown) {
  type TreePathNode = { data: NodeSummary; parent: TreePathNode | null; level: number }
  const path: NodeSummary[] = []
  let current = rawTreeNode as TreePathNode | null
  while (current && current.level > 0 && path.length < 4) {
    path.unshift(current.data)
    current = current.parent
  }
  void choose(node, path.length ? path : [node])
}

async function openMatch(match: NodeMatch) {
  if (await choose(match.node, match.path)) {
    searchWord.value = ''
    if (searchTimer) clearTimeout(searchTimer)
    void searchAll()
  }
}
function openChild(node: NodeSummary) {
  void choose(node, [...breadcrumbs.value, node])
}
function navigateBreadcrumb(index: number) {
  const path = breadcrumbs.value.slice(0, index + 1)
  const node = path.at(-1)
  if (node) void choose(node, path)
}
function onRegionFilterChange() {
  childPage.value = 1
  void loadChildren()
}
function onChildSearch() {
  if (childTimer) clearTimeout(childTimer)
  childTimer = setTimeout(() => {
    childPage.value = 1
    void loadChildren()
  }, 300)
}
function onPageChange(page: number) {
  childPage.value = page
  void loadChildren()
}

// 详情加载按序号校验，防止快速点击不同节点后编辑抽屉显示错误数据。
async function openEditor(node: NodeSummary) {
  if (!(await confirmDiscard())) return
  ++detailSequence
  detailController?.abort()
  editorNode.value = node
  creating.value = false
  drawerVisible.value = true
  detailLoading.value = true
  const sequence = ++detailSequence
  const controller = new AbortController()
  detailController = controller
  try {
    const result = await nodeApi.getNode(node.id, controller.signal)
    if (sequence === detailSequence) setForm(result.data)
  } catch (error) {
    if (sequence === detailSequence && !axios.isCancel(error)) {
      ElMessage.error(apiErrorMessage(error, '节点详情加载失败'))
      drawerVisible.value = false
    }
  } finally {
    if (sequence === detailSequence) detailLoading.value = false
  }
}

async function openCreate(parent: NodeSummary | null) {
  if (parent && !nextType[parent.nodeType]) return
  if (!(await confirmDiscard())) return
  ++detailSequence
  detailController?.abort()
  editorNode.value = null
  creating.value = true
  detailLoading.value = false
  resetForm(parent?.id ?? 0, parent ? nextType[parent.nodeType]! : 'game')
  drawerVisible.value = true
}

// 保存后局部刷新受影响的父目录和子项列表，不强制重新请求整棵树。
async function save() {
  if (saving.value) return
  if (form.nodeType === 'game' && !form.marketType) {
    ElMessage.warning('请选择运营地区')
    return
  }
  if (form.nodeType === 'region' && !form.envType) {
    ElMessage.warning('请选择大区类型')
    return
  }
  if (!form.name.trim() || !form.code.trim()) {
    ElMessage.warning('请填写名称和编码')
    return
  }
  try {
    const definition = await getConfigSchema('base', form.nodeType)
    validateConfigAgainstSchema(form.config, definition.schema)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '配置校验失败')
    return
  }
  saving.value = true
  try {
    if (creating.value) {
      const result = await nodeApi.createNode(form)
      await refreshTreeParent(result.data.parentId)
      if (selected.value?.id === result.data.parentId) await loadChildren()
      if (result.data.parentId === 0) {
        await chooseAfterSave(toSummary(result.data), [toSummary(result.data)])
      }
    } else if (editorNode.value) {
      const result = await nodeApi.updateNode(editorNode.value.id, {
        name: form.name, status: form.status, config: form.config, remark: form.remark, envType: form.envType, marketType: form.marketType
      })
      const summary = toSummary(result.data)
      if (selected.value?.id === summary.id) {
        selected.value = summary
        breadcrumbs.value = breadcrumbs.value.map(item => item.id === summary.id ? summary : item)
      }
      const cached = treeRef.value?.getNode(summary.id)
      if (cached) Object.assign(cached.data, { name: summary.name, status: summary.status, envType: summary.envType, marketType: summary.marketType })
      await refreshTreeParent(summary.parentId)
      if (selected.value?.id === summary.parentId) await loadChildren()
    }
    originalForm = JSON.stringify(form)
    drawerVisible.value = false
    emit('changed')
    ElMessage.success('配置保存成功，等待接入方同步')
  } catch (error) {
    if (axios.isAxiosError(error) && error.response?.status === 409) {
      ElMessage.error('数据存在冲突，请刷新后重试')
    } else {
      ElMessage.error(apiErrorMessage(error, '保存配置失败'))
    }
  } finally {
    saving.value = false
  }
}

async function chooseAfterSave(node: NodeSummary, path: NodeSummary[]) {
  selected.value = node
  breadcrumbs.value = path
  childPage.value = 1
  childSearch.value = ''
  void loadChildren()
}

async function removeNode(node: NodeSummary) {
  try {
    await ElMessageBox.confirm('确定删除「' + node.name + '」？存在子项时不能删除。', '删除节点', { type: 'warning' })
  } catch { return }
  try {
    await nodeApi.deleteNode(node.id)
    const isCurrent = selected.value?.id === node.id
    await refreshTreeParent(node.parentId)
    if (isCurrent) {
      const path = breadcrumbs.value.slice(0, -1)
      const parent = path.at(-1)
      selected.value = parent ?? null
      breadcrumbs.value = path
      childPage.value = 1
    }
    if (selected.value) {
      if (children.value.length === 1 && childPage.value > 1) childPage.value--
      await loadChildren()
    }
    emit('changed')
    ElMessage.success('已删除')
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '删除节点失败'))
  }
}

async function refresh() {
  if (!(await confirmDiscard())) return
  await reloadTree()
  if (selected.value) await loadChildren()
  emit('changed')
}

onBeforeUnmount(() => {
  unregisterUnsaved()
  if (searchTimer) clearTimeout(searchTimer)
  if (childTimer) clearTimeout(childTimer)
  searchController?.abort()
  childController?.abort()
  detailController?.abort()
})
  // 通过服务端路径精准定位节点，不依赖目录树是否已经懒加载展开。
  async function selectById(id: number) {
    try {
      const result = await nodeApi.getPath(id)
      return await choose(result.data.node, result.data.path)
    } catch (error) {
      ElMessage.error(apiErrorMessage(error, '定位配置节点失败'))
      return false
    }
  }

  return {
    selectById, labels, nextType, treeProps, treeRef, treeScrollRef, treeVersion, restoredExpandedKeys, searchWord, searchMatches, searchLoading, selected, breadcrumbs, children, childTotal, childPage, childSize, childSearch, regionFilter, onRegionFilterChange, childrenLoading, drawerVisible, creating, envTypeLocked, detailLoading, saving, form, nextLabel, editorTitle, loadTree, onExpand, onCollapse, onTreeClick, onSearchInput, openMatch, openChild, navigateBreadcrumb, onChildSearch, onPageChange, openEditor, openCreate, beforeDrawerClose, closeEditor, save, removeNode, refresh
  }
}
