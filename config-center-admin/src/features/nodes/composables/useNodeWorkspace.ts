import { nextTick, onBeforeUnmount, reactive, ref, computed } from 'vue'
import axios from 'axios'
import { nodeApi } from '../../../api/nodeApi'
import type { NodeType, NodeSummary, NodeRecord, NodePage, NodeMatch } from '../types'
import { ElMessage, ElMessageBox, ElTree } from 'element-plus'
import type { LoadFunction } from 'element-plus'

export function useNodeWorkspace(emit: (event: 'changed') => void) {
const labels: Record<NodeType, string> = {
  game: '游戏', region: '大区', server_group: '区服组', channel_app: '渠道应用'
}
const nextType: Partial<Record<NodeType, NodeType>> = {
  game: 'region', region: 'server_group', server_group: 'channel_app'
}
const treeProps = { label: 'name', isLeaf: 'leaf' }
const treeRef = ref<InstanceType<typeof ElTree> | null>(null)
const treeScrollRef = ref<HTMLElement | null>(null)
const treeVersion = ref(0)
const expandedKeys = ref<number[]>([])
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
const childrenLoading = ref(false)
const drawerVisible = ref(false)
const creating = ref(false)
const editorNode = ref<NodeSummary | null>(null)
const detailLoading = ref(false)
const saving = ref(false)
const form = reactive({
  parentId: 0, nodeType: 'game' as NodeType, code: '', name: '',
  status: 1, config: '{}', remark: ''
})
const nextLabel = computed(() => {
  const type = selected.value && nextType[selected.value.nodeType]
  return type ? labels[type] : '子项'
})
const editorTitle = computed(() => creating.value ? '新增' + labels[form.nodeType] : '编辑配置节点')
let originalForm = ''
let searchTimer: ReturnType<typeof setTimeout> | undefined
let childTimer: ReturnType<typeof setTimeout> | undefined
let searchSequence = 0
let detailSequence = 0
let childSequence = 0
let searchController: AbortController | undefined
let childController: AbortController | undefined
let detailController: AbortController | undefined

function toSummary(node: NodeRecord): NodeSummary {
  return { id: node.id, parentId: node.parentId, nodeType: node.nodeType,
    code: node.code, name: node.name, status: node.status,
    leaf: ['server_group', 'channel_app'].includes(node.nodeType) }
}

function resetForm(parentId: number, nodeType: NodeType) {
  Object.assign(form, { parentId, nodeType, code: '', name: '', status: 1,
    config: '{}', remark: '' })
  originalForm = JSON.stringify(form)
}

function setForm(node: NodeRecord) {
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

const loadTree: LoadFunction = async (node, resolve, reject) => {
  const parentId = node.level === 0 ? 0 : (node.data as NodeSummary).id
  try {
    const result = await nodeApi.getTreeChildren(parentId)
    resolve(result.data)
  } catch {
    ElMessage.error('配置目录加载失败，请重新展开')
    reject()
  }
}

function onExpand(node: NodeSummary) {
  if (!expandedKeys.value.includes(node.id)) expandedKeys.value = [...expandedKeys.value, node.id]
}
function onCollapse(node: NodeSummary) {
  expandedKeys.value = expandedKeys.value.filter(id => id !== node.id)
}

async function reloadTree() {
  const scrollTop = treeScrollRef.value?.scrollTop ?? 0
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
  } catch {
    ElMessage.warning('操作已成功，但目录刷新失败，请手动刷新')
  }
}

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
    if (sequence === searchSequence && !axios.isCancel(error)) ElMessage.error('全局搜索失败')
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

async function loadChildren() {
  const node = selected.value
  const sequence = ++childSequence
  childController?.abort()
  if (!node || node.nodeType === 'channel_app') {
    children.value = []
    childTotal.value = 0
    childrenLoading.value = false
    return
  }
  const controller = new AbortController()
  childController = controller
  childrenLoading.value = true
  try {
    const result = await nodeApi.getChildren({ parentId: node.id, page: childPage.value, size: childSize, keyword: childSearch.value }, controller.signal)
    if (sequence !== childSequence) return
    children.value = result.data.items
    childTotal.value = result.data.total
  } catch (error) {
    if (sequence === childSequence && !axios.isCancel(error)) ElMessage.error('子项加载失败')
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
      ElMessage.error('节点详情加载失败')
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

async function save() {
  if (saving.value) return
  if (!form.name.trim() || !form.code.trim()) {
    ElMessage.warning('请填写名称和编码')
    return
  }
  try { JSON.parse(form.config) }
  catch { ElMessage.error('JSON 格式不正确'); return }
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
        name: form.name, status: form.status, config: form.config, remark: form.remark
      })
      const summary = toSummary(result.data)
      if (selected.value?.id === summary.id) {
        selected.value = summary
        breadcrumbs.value = breadcrumbs.value.map(item => item.id === summary.id ? summary : item)
      }
      const cached = treeRef.value?.getNode(summary.id)
      if (cached) Object.assign(cached.data, { name: summary.name, status: summary.status })
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
      ElMessage.error('保存失败，请检查配置和节点约束')
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
  } catch {
    ElMessage.error('删除失败，请先删除子项')
  }
}

async function refresh() {
  if (!(await confirmDiscard())) return
  await reloadTree()
  if (selected.value) await loadChildren()
  emit('changed')
}

onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
  if (childTimer) clearTimeout(childTimer)
  searchController?.abort()
  childController?.abort()
  detailController?.abort()
})
  return {
    labels, nextType, treeProps, treeRef, treeScrollRef, treeVersion, expandedKeys, searchWord, searchMatches, searchLoading, selected, breadcrumbs, children, childTotal, childPage, childSize, childSearch, childrenLoading, drawerVisible, creating, detailLoading, saving, form, nextLabel, editorTitle, loadTree, onExpand, onCollapse, onTreeClick, onSearchInput, openMatch, openChild, navigateBreadcrumb, onChildSearch, onPageChange, openEditor, openCreate, beforeDrawerClose, closeEditor, save, removeNode, refresh
  }
}
