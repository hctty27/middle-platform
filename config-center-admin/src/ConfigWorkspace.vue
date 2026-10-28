<script setup lang="ts">
import { nextTick, onBeforeUnmount, reactive, ref, computed } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox, ElTree } from 'element-plus'
import type { LoadFunction } from 'element-plus'

type NodeType = 'game' | 'region' | 'server_group' | 'channel' | 'sdk_app'

interface NodeSummary {
  id: number
  parentId: number
  nodeType: NodeType
  code: string
  name: string
  status: number
  leaf: boolean
}
interface NodeRecord extends Omit<NodeSummary, 'leaf'> {
  config: string
  remark: string | null
}
interface NodePage {
  items: NodeSummary[]
  total: number
  page: number
  size: number
}
interface NodeMatch {
  node: NodeSummary
  path: NodeSummary[]
}

const emit = defineEmits<{ changed: [] }>()
const api = axios.create({ baseURL: '/api/v1', timeout: 10000 })
const labels: Record<NodeType, string> = {
  game: '游戏', region: '大区', server_group: '区服组', channel: '渠道', sdk_app: 'SDK 应用'
}
const nextType: Partial<Record<NodeType, NodeType>> = {
  game: 'region', region: 'server_group', server_group: 'channel', channel: 'sdk_app'
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
let selectionSequence = 0
let detailSequence = 0
let childSequence = 0
let searchController: AbortController | undefined
let childController: AbortController | undefined
let detailController: AbortController | undefined

function toSummary(node: NodeRecord): NodeSummary {
  return { id: node.id, parentId: node.parentId, nodeType: node.nodeType,
    code: node.code, name: node.name, status: node.status,
    leaf: ['server_group', 'channel', 'sdk_app'].includes(node.nodeType) }
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
    const result = await api.get<NodeSummary[]>('/admin/nodes/tree', { params: { parentId } })
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
    const result = await api.get<NodeSummary[]>('/admin/nodes/tree', { params: { parentId } })
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
    const result = await api.get<NodeMatch[]>('/admin/nodes/search', {
      params: { keyword }, signal: controller.signal
    })
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
  if (!node || node.nodeType === 'sdk_app') {
    children.value = []
    childTotal.value = 0
    childrenLoading.value = false
    return
  }
  const controller = new AbortController()
  childController = controller
  childrenLoading.value = true
  try {
    const result = await api.get<NodePage>('/admin/nodes/children', {
      params: { parentId: node.id, page: childPage.value, size: childSize, keyword: childSearch.value },
      signal: controller.signal
    })
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
  ++selectionSequence
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
  while (current && current.level > 0 && path.length < 5) {
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
    const result = await api.get<NodeRecord>('/admin/nodes/' + node.id, { signal: controller.signal })
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
      const result = await api.post<NodeRecord>('/admin/nodes', form)
      await refreshTreeParent(result.data.parentId)
      if (selected.value?.id === result.data.parentId) await loadChildren()
      if (result.data.parentId === 0) {
        await chooseAfterSave(toSummary(result.data), [toSummary(result.data)])
      }
    } else if (editorNode.value) {
      const result = await api.put<NodeRecord>('/admin/nodes/' + editorNode.value.id, {
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
    await api.delete('/admin/nodes/' + node.id)
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
</script>

<template>
  <main class="workspace">
    <section class="tree-panel">
      <div class="panel-title">
        <strong>配置目录</strong>
        <div class="panel-actions">
          <el-button size="small" @click="refresh">刷新</el-button>
          <el-button size="small" type="primary" @click="openCreate(null)">+ 游戏</el-button>
        </div>
      </div>
      <el-input v-model="searchWord" clearable placeholder="全局搜索名称或 Code"
                @input="onSearchInput" @clear="onSearchInput" />
      <div v-if="searchWord.trim()" class="tree-scroll search-results" v-loading="searchLoading">
        <div class="search-count">跨所有游戏、大区、区服组、渠道和 SDK 应用搜索</div>
        <el-empty v-if="!searchLoading && !searchMatches.length" description="无匹配节点" :image-size="75" />
        <button v-for="match in searchMatches" :key="match.node.id" class="search-result"
                @click="openMatch(match)">
          <span class="search-result-title">{{ match.node.name }}
            <el-tag size="small" type="info">{{ labels[match.node.nodeType] }}</el-tag>
          </span>
          <small>{{ match.path.map(item => item.name).join(' / ') }}</small>
          <small>Code: {{ match.node.code }}</small>
        </button>
      </div>
      <div v-show="!searchWord.trim()" ref="treeScrollRef" class="tree-scroll">
        <el-tree :key="treeVersion" ref="treeRef" node-key="id" lazy :load="loadTree"
                 :props="treeProps" :default-expanded-keys="expandedKeys"
                 :current-node-key="selected?.id" highlight-current
                 :expand-on-click-node="false" @node-click="onTreeClick"
                 @node-expand="onExpand" @node-collapse="onCollapse">
          <template #default="{ data }">
            <span class="node-row">
              <span class="node-name" :title="data.name">{{ data.name }}</span>
              <el-tag size="small" effect="plain" type="info">{{ labels[data.nodeType as NodeType] }}</el-tag>
            </span>
          </template>
        </el-tree>
      </div>
      <div class="tree-hint">目录只展开前三层，渠道和 SDK 应用在右侧列表中逐级查看。</div>
    </section>

    <section class="editor-panel">
      <div class="page-crumbs" v-if="breadcrumbs.length">
        <el-breadcrumb separator="/">
          <el-breadcrumb-item v-for="(item, index) in breadcrumbs" :key="item.id">
            <button class="crumb-button" :class="{ current: index === breadcrumbs.length - 1 }"
                    @click="navigateBreadcrumb(index)">{{ item.name }}</button>
          </el-breadcrumb-item>
        </el-breadcrumb>
      </div>
      <div v-if="!selected" class="empty-workspace">
        <el-empty description="从左侧选择一个节点，或直接搜索名称 / Code" />
      </div>
      <template v-else>
        <div class="node-heading">
          <div>
            <div class="node-heading-title">
              <h2>{{ selected.name }}</h2>
              <el-tag :type="selected.status === 1 ? 'success' : 'info'" effect="plain">
                {{ selected.status === 1 ? '已启用' : '已停用' }}
              </el-tag>
            </div>
            <div class="node-meta">{{ labels[selected.nodeType] }} · ID {{ selected.id }} · Code {{ selected.code }}</div>
          </div>
          <div class="panel-actions">
            <el-button @click="openEditor(selected)">查看 / 编辑</el-button>
            <el-button type="danger" plain @click="removeNode(selected)">删除</el-button>
          </div>
        </div>
        <div class="children-toolbar">
          <div>
            <strong>{{ selected.nodeType === 'sdk_app' ? '节点详情' : nextLabel + '列表' }}</strong>
            <span v-if="selected.nodeType !== 'sdk_app'" class="muted">共 {{ childTotal }} 条</span>
          </div>
          <div class="children-tools" v-if="selected.nodeType !== 'sdk_app'">
            <el-input v-model="childSearch" clearable placeholder="筛选直属子项" @input="onChildSearch" />
            <el-button type="primary" @click="openCreate(selected)">+ 新增{{ nextLabel }}</el-button>
          </div>
        </div>
        <div v-if="selected.nodeType === 'sdk_app'" class="leaf-tip">
          SDK 应用是末级节点，可以通过右上角“查看 / 编辑”维护配置。
        </div>
        <template v-else>
          <div class="children-table">
            <el-table v-loading="childrenLoading" :data="children" stripe height="100%"
                      empty-text="暂无子项" @row-click="openChild">
              <el-table-column prop="name" label="名称" min-width="180" show-overflow-tooltip />
              <el-table-column prop="code" label="Code" min-width="145" show-overflow-tooltip />
              <el-table-column label="状态" width="92">
                <template #default="{ row }">
                  <el-tag :type="row.status === 1 ? 'success' : 'info'" effect="plain" size="small">
                    {{ row.status === 1 ? '启用' : '停用' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="154" fixed="right">
                <template #default="{ row }">
                  <el-button link type="primary" @click.stop="openChild(row)">进入</el-button>
                  <el-button link @click.stop="openEditor(row)">编辑</el-button>
                  <el-button link type="danger" @click.stop="removeNode(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
          <el-pagination v-if="childTotal > childSize" class="children-pagination"
                         :current-page="childPage" :page-size="childSize" :total="childTotal"
                         layout="total, prev, pager, next" @current-change="onPageChange" />
        </template>
      </template>
    </section>

    <el-drawer v-model="drawerVisible" :title="editorTitle" size="min(720px, 96vw)"
               :before-close="beforeDrawerClose" :close-on-click-modal="false">
      <div v-loading="detailLoading" class="drawer-content">
        <el-form v-if="!detailLoading" label-position="top" class="node-form">
          <div class="form-grid">
            <el-form-item label="节点类型"><el-input :model-value="labels[form.nodeType]" disabled /></el-form-item>
            <el-form-item label="父节点 ID（根据层级推导）"><el-input :model-value="String(form.parentId)" disabled /></el-form-item>
            <el-form-item label="业务编码"><el-input v-model="form.code" :disabled="!creating" maxlength="128" /></el-form-item>
            <el-form-item label="节点名称"><el-input v-model="form.name" maxlength="128" show-word-limit /></el-form-item>
          </div>
          <el-form-item label="状态">
            <el-radio-group v-model="form.status">
              <el-radio :value="1">启用</el-radio><el-radio :value="0">停用</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="本节点 JSON 配置">
            <el-input v-model="form.config" type="textarea" :rows="15" class="json-input" />
          </el-form-item>
          <div class="muted form-tip">配置不继承父节点；保存后版本递增并通知接入方拉取。</div>
          <el-form-item label="备注">
            <el-input v-model="form.remark" type="textarea" maxlength="500" show-word-limit />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button :disabled="saving" @click="closeEditor">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="detailLoading" @click="save">保存配置</el-button>
      </template>
    </el-drawer>
  </main>
</template>
