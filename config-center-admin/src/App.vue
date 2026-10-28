<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox, ElTree } from 'element-plus'
import type { LoadFunction } from 'element-plus'

type NodeType = 'game' | 'region' | 'server_group' | 'channel' | 'sdk_app'
type ViewTab = 'children' | 'details'

interface NodeRecord {
  id: number
  parentId: number
  nodeType: NodeType
  code: string
  name: string
  status: number
  config: string
  remark: string | null
}
interface NodeSummary {
  id: number
  parentId: number
  nodeType: NodeType
  code: string
  name: string
  status: number
  leaf: boolean
}
interface NodePage {
  items: NodeSummary[]
  total: number
  page: number
  size: number
}
interface Subscriber {
  id: number
  code: string
  callbackUrl: string
  enabled: boolean
  acknowledgedRevision: number
  failures: number
  lastError: string | null
}

const api = axios.create({ baseURL: '/api/v1', timeout: 10000 })
const navigation = ref<'nodes' | 'subscribers' | 'about'>('nodes')
const subscribers = ref<Subscriber[]>([])
const treeRef = ref<InstanceType<typeof ElTree> | null>(null)
const treeVersion = ref(0)
const treeSearch = ref('')
const selected = ref<NodeSummary | null>(null)
const navigationStack = ref<NodeSummary[]>([])
const detail = ref<NodeRecord | null>(null)
const activeView = ref<ViewTab>('children')
const creating = ref(false)
const detailLoading = ref(false)
const childrenLoading = ref(false)
const children = ref<NodeSummary[]>([])
const childTotal = ref(0)
const childPage = ref(1)
const childSize = ref(20)
const childSearch = ref('')
const revision = ref(0)
const epoch = ref('')
const showSubscriber = ref(false)
const newSubscriber = reactive({ code: '', callbackUrl: '', secret: '' })
const form = reactive({
  parentId: 0,
  nodeType: 'game' as NodeType,
  code: '',
  name: '',
  status: 1,
  config: '{}',
  remark: ''
})
const labels: Record<NodeType, string> = {
  game: '游戏', region: '区域', server_group: '服务器组', channel: '渠道', sdk_app: 'SDK 应用'
}
const nextType: Partial<Record<NodeType, NodeType>> = {
  game: 'region', region: 'server_group', server_group: 'channel', channel: 'sdk_app'
}
const treeProps = { label: 'name', isLeaf: 'leaf' }
const pathInfo = computed(() => {
  if (creating.value) return '新增 ' + labels[form.nodeType]
  return selected.value ? labels[selected.value.nodeType] + ' / ' + selected.value.code : '请选择左侧节点'
})
const nextLabel = computed(() => {
  const type = selected.value && nextType[selected.value.nodeType]
  return type ? labels[type] : '子项'
})
let detailRequest = 0
let childrenRequest = 0
let searchTimer: ReturnType<typeof setTimeout> | undefined
let originalForm = ''

function toSummary(node: NodeRecord): NodeSummary {
  return { id: node.id, parentId: node.parentId, nodeType: node.nodeType, code: node.code,
    name: node.name, status: node.status, leaf: node.nodeType === 'server_group' || node.nodeType === 'channel' || node.nodeType === 'sdk_app' }
}

function setForm(node: NodeRecord) {
  Object.assign(form, node, { remark: node.remark ?? '' })
  try { form.config = JSON.stringify(JSON.parse(node.config), null, 2) }
  catch { form.config = node.config }
  originalForm = JSON.stringify(form)
}

function resetForm(parentId: number, nodeType: NodeType) {
  Object.assign(form, { parentId, nodeType, code: '', name: '', status: 1, config: '{}', remark: '' })
  originalForm = JSON.stringify(form)
}

async function confirmDiscard(): Promise<boolean> {
  if (!creating.value && !detail.value) return true
  if (JSON.stringify(form) === originalForm) return true
  try {
    await ElMessageBox.confirm('当前修改尚未保存，确定放弃吗？', '未保存的修改', { type: 'warning' })
    return true
  } catch {
    return false
  }
}

const loadTree: LoadFunction = async (node, resolve, reject) => {
  const parentId = node.level === 0 ? 0 : (node.data as NodeSummary).id
  try {
    const result = await api.get<NodeSummary[]>('/admin/nodes/tree', { params: { parentId } })
    resolve(result.data)
  } catch {
    ElMessage.error('配置树加载失败，请重试展开')
    reject()
  }
}

function filterTree(word: string, data: unknown) {
  const value = word.trim().toLowerCase()
  if (!value) return true
  const node = data as Partial<NodeSummary>
  return String(node.name ?? '').toLowerCase().includes(value) ||
    String(node.code ?? '').toLowerCase().includes(value)
}

function onTreeSearch() {
  treeRef.value?.filter(treeSearch.value)
}

async function refreshRevision() {
  try {
    const result = await api.get<{ epoch: string; revision: number }>('/sync/revision')
    revision.value = result.data.revision
    epoch.value = result.data.epoch
  } catch {
    ElMessage.error('读取配置版本失败')
  }
}

async function loadChildren() {
  const current = selected.value
  const sequence = ++childrenRequest
  if (!current || current.nodeType === 'sdk_app') {
    children.value = []
    childTotal.value = 0
    childrenLoading.value = false
    return
  }
  childrenLoading.value = true
  try {
    const result = await api.get<NodePage>('/admin/nodes/children', {
      params: { parentId: current.id, page: childPage.value, size: childSize.value, keyword: childSearch.value }
    })
    if (sequence !== childrenRequest) return
    children.value = result.data.items
    childTotal.value = result.data.total
  } catch {
    if (sequence === childrenRequest) ElMessage.error('读取子项列表失败')
  } finally {
    if (sequence === childrenRequest) childrenLoading.value = false
  }
}

async function loadDetail() {
  const current = selected.value
  if (!current) return
  const sequence = ++detailRequest
  detailLoading.value = true
  try {
    const result = await api.get<NodeRecord>('/admin/nodes/' + current.id)
    if (sequence !== detailRequest) return
    detail.value = result.data
    setForm(result.data)
  } catch {
    if (sequence === detailRequest) ElMessage.error('读取节点详情失败')
  } finally {
    if (sequence === detailRequest) detailLoading.value = false
  }
}

async function choose(node: NodeSummary, view: ViewTab = 'children') {
  if (!(await confirmDiscard())) return false
  if (searchTimer) clearTimeout(searchTimer)
  const wasDetails = activeView.value === 'details'
  ++detailRequest
  ++childrenRequest
  selected.value = node
  detail.value = null
  creating.value = false
  activeView.value = view
  childPage.value = 1
  childSearch.value = ''
  const treeNode = treeRef.value?.getNode(node.id)
  treeRef.value?.setCurrentKey(treeNode ? node.id : undefined)
  void loadChildren()
  if (view === 'details' && wasDetails) void loadDetail()
  return true
}

async function onTreeClick(node: NodeSummary) {
  if (await choose(node)) navigationStack.value = []
}

async function openChild(node: NodeSummary, view: ViewTab = 'children') {
  const previous = selected.value
  if (await choose(node, view) && previous) navigationStack.value = [...navigationStack.value, previous]
}

function onRowClick(node: NodeSummary) {
  void openChild(node, 'details')
}

async function goBack() {
  const previous = navigationStack.value.at(-1)
  if (previous && await choose(previous)) navigationStack.value = navigationStack.value.slice(0, -1)
}

function onChildSearch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    childPage.value = 1
    void loadChildren()
  }, 300)
}

function onPageChange(page: number) {
  childPage.value = page
  void loadChildren()
}

watch(activeView, view => {
  if (view === 'details' && !creating.value && selected.value && !detail.value) void loadDetail()
})

async function refreshTreeParent(parentId: number) {
  if (parentId === 0) {
    treeVersion.value++
    return
  }
  const parent = treeRef.value?.getNode(parentId)
  if (!parent || parent.data.nodeType === 'server_group') return
  try {
    const result = await api.get<NodeSummary[]>('/admin/nodes/tree', { params: { parentId } })
    treeRef.value?.updateKeyChildren(parentId, result.data)
  } catch {
    ElMessage.warning('保存成功，但导航树刷新失败，可点击顶部刷新')
  }
}

async function refresh() {
  if (!(await confirmDiscard())) return
  treeVersion.value++
  await refreshRevision()
  if (selected.value) {
    if (activeView.value === 'details' && !creating.value) void loadDetail()
    void loadChildren()
  }
}

async function newGame() {
  if (!(await confirmDiscard())) return
  ++detailRequest
  selected.value = null
  detail.value = null
  navigationStack.value = []
  creating.value = true
  activeView.value = 'details'
  resetForm(0, 'game')
}

async function newChild() {
  if (!selected.value || !nextType[selected.value.nodeType]) return
  if (!(await confirmDiscard())) return
  ++detailRequest
  detail.value = null
  creating.value = true
  activeView.value = 'details'
  resetForm(selected.value.id, nextType[selected.value.nodeType]!)
}

async function save() {
  if (!form.name.trim() || !form.code.trim()) {
    ElMessage.warning('请填写名称和编码')
    return
  }
  try { JSON.parse(form.config) }
  catch { ElMessage.error('JSON 格式不正确'); return }

  try {
    if (creating.value) {
      const parent = selected.value
      const result = await api.post<NodeRecord>('/admin/nodes', form)
      creating.value = false
      detail.value = null
      await refreshTreeParent(result.data.parentId)
      if (parent) {
        activeView.value = 'children'
        childPage.value = 1
        childSearch.value = ''
        await loadChildren()
      } else {
        await choose(toSummary(result.data), 'details')
      }
    } else if (selected.value && detail.value) {
      const result = await api.put<NodeRecord>('/admin/nodes/' + selected.value.id, {
        name: form.name, status: form.status, config: form.config, remark: form.remark
      })
      detail.value = result.data
      selected.value = toSummary(result.data)
      setForm(result.data)
      const treeNode = treeRef.value?.getNode(result.data.id)
      if (treeNode) {
        treeNode.data.name = result.data.name
        treeNode.data.status = result.data.status
      }
      await refreshTreeParent(result.data.parentId)
    }
    await refreshRevision()
    ElMessage.success('配置已保存，变更将通知接入平台')
  } catch {
    ElMessage.error('保存失败，请检查节点约束和编码唯一性')
  }
}

async function removeNode(node: NodeSummary | null) {
  if (!node) return
  try {
    await ElMessageBox.confirm('确认删除节点「' + node.name + '」？有子节点时不可删除。', '删除确认', { type: 'warning' })
  } catch { return }

  try {
    await api.delete('/admin/nodes/' + node.id)
    if (selected.value?.id === node.id) {
      ++detailRequest
      ++childrenRequest
      selected.value = null
      detail.value = null
      creating.value = false
      children.value = []
      childTotal.value = 0
      const previous = navigationStack.value.at(-1)
      if (previous) {
        navigationStack.value = navigationStack.value.slice(0, -1)
        void choose(previous)
      }
    }
    await refreshTreeParent(node.parentId)
    if (selected.value) {
      if (children.value.length === 1 && childPage.value > 1) childPage.value--
      await loadChildren()
    }
    await refreshRevision()
    ElMessage.success('已删除')
  } catch {
    ElMessage.error('删除失败，请先删除子节点')
  }
}

async function loadSubscribers() {
  try {
    subscribers.value = (await api.get<Subscriber[]>('/admin/subscribers')).data
  } catch { ElMessage.error('读取订阅者失败') }
}
async function addSubscriber() {
  try {
    await api.post('/admin/subscribers', newSubscriber)
    showSubscriber.value = false
    Object.assign(newSubscriber, { code: '', callbackUrl: '', secret: '' })
    await loadSubscribers()
    ElMessage.success('接入平台已创建')
  } catch { ElMessage.error('创建失败：请确认回调地址已配置到服务端白名单') }
}
function navigate(tab: 'nodes' | 'subscribers' | 'about') {
  navigation.value = tab
  if (tab === 'subscribers') void loadSubscribers()
}

onMounted(() => { void refresh() })
onBeforeUnmount(() => { if (searchTimer) clearTimeout(searchTimer) })
</script>

<template>
  <div class="shell">
    <aside class="sidebar">
      <div class="brand"><div class="brand-mark">M</div><div><strong>研发中台</strong><small>CONFIG CENTER</small></div></div>
      <div class="group-title">工作空间</div>
      <button :class="{active: navigation==='nodes'}" @click="navigate('nodes')">配置管理</button>
      <button :class="{active: navigation==='subscribers'}" @click="navigate('subscribers')">接入与同步</button>
      <button :class="{active: navigation==='about'}" @click="navigate('about')">系统说明</button>
      <div class="sidebar-bottom">无登录模式 · 内网使用</div>
    </aside>
    <div class="main">
      <header class="topbar">
        <div><strong>配置中心</strong><span class="subline">统一权威数据源 · Webhook 轻量通知</span></div>
        <div class="top-right"><el-tag type="success" effect="plain">版本 {{ revision }}</el-tag>
          <el-button @click="refresh">刷新</el-button></div>
      </header>
      <main v-if="navigation==='nodes'" class="workspace">
        <section class="tree-panel">
          <div class="panel-title"><strong>节点导航</strong><el-button type="primary" size="small" @click="newGame">+ 游戏</el-button></div>
          <el-input v-model="treeSearch" placeholder="搜索已加载节点" clearable @input="onTreeSearch" />
          <div class="tree-scroll">
            <el-tree :key="treeVersion" ref="treeRef" node-key="id" lazy :load="loadTree"
                     :props="treeProps" :filter-node-method="filterTree" highlight-current
                     :expand-on-click-node="false" @node-click="onTreeClick">
              <template #default="{ data }">
                <div class="node-row">
                  <span class="node-name" :title="data.name">{{ data.name }}</span>
                  <el-tag size="small" type="info" effect="plain">{{ labels[data.nodeType as NodeType] }}</el-tag>
                </div>
              </template>
            </el-tree>
          </div>
          <div class="tree-hint">按需展开游戏 / 区域 / 服务器组，渠道和 SDK 应用在右侧管理</div>
        </section>
        <section class="editor-panel">
          <div class="panel-title">
            <div><strong>{{ creating ? '新增配置节点' : (selected?.name || '选择配置节点') }}</strong>
              <div class="muted">{{ pathInfo }}</div>
              <el-button v-if="navigationStack.length && !creating" link size="small" @click="goBack">← 返回上级</el-button></div>
            <div v-if="selected && !creating" class="panel-actions">
              <el-button :disabled="selected.nodeType==='sdk_app'" @click="newChild">新增{{ nextLabel }}</el-button>
              <el-button type="danger" plain @click="removeNode(selected)">删除</el-button>
            </div>
          </div>
          <el-empty v-if="!selected && !creating" description="从左侧配置树选择节点，或新建游戏" />
          <template v-else>
            <el-tabs v-if="!creating" v-model="activeView" class="node-tabs">
              <el-tab-pane label="子项列表" name="children" />
              <el-tab-pane label="节点详情" name="details" />
            </el-tabs>
            <div v-if="!creating && activeView==='children'" class="children-section">
              <div class="children-toolbar">
                <div><strong>{{ nextLabel }}列表</strong><span class="muted"> 共 {{ childTotal }} 条</span></div>
                <el-input v-if="selected?.nodeType!=='sdk_app'" v-model="childSearch"
                          placeholder="搜索直属子项名称 / Code" clearable class="children-search" @input="onChildSearch" />
              </div>
              <el-table v-loading="childrenLoading" :data="children" stripe height="100%"
                        empty-text="暂无子项" @row-click="onRowClick">
                <el-table-column prop="name" label="名称" min-width="160" show-overflow-tooltip />
                <el-table-column prop="code" label="Code" min-width="140" show-overflow-tooltip />
                <el-table-column label="类型" width="115">
                  <template #default="{ row }">{{ labels[row.nodeType as NodeType] }}</template>
                </el-table-column>
                <el-table-column label="状态" width="90">
                  <template #default="{ row }"><el-tag :type="row.status === 1 ? 'success' : 'info'">
                    {{ row.status === 1 ? '启用' : '停用' }}</el-tag></template>
                </el-table-column>
                <el-table-column label="操作" width="170" fixed="right">
                  <template #default="{ row }">
                    <el-button link type="primary" @click.stop="openChild(row, 'details')">详情</el-button>
                    <el-button v-if="row.nodeType!=='sdk_app'" link type="primary" @click.stop="openChild(row)">子项</el-button>
                    <el-button link type="danger" @click.stop="removeNode(row)">删除</el-button>
                  </template>
                </el-table-column>
              </el-table>
              <el-pagination v-if="childTotal > childSize" class="children-pagination"
                             :current-page="childPage" :page-size="childSize" :total="childTotal"
                             layout="total, prev, pager, next" @current-change="onPageChange" />
            </div>
            <template v-else>
              <el-skeleton v-if="!creating && detailLoading" :rows="8" animated />
              <el-alert v-else-if="!creating && !detail" title="详情加载失败，请切换标签重试或点击顶部刷新" type="warning" :closable="false" />
              <el-form v-else label-position="top" class="node-form">
                <div class="form-grid">
                  <el-form-item label="节点类型"><el-input :model-value="labels[form.nodeType]" disabled /></el-form-item>
                  <el-form-item label="父节点 ID"><el-input :model-value="String(form.parentId)" disabled /></el-form-item>
                  <el-form-item label="业务编码（创建后不可修改）"><el-input v-model="form.code" :disabled="!creating" maxlength="128" /></el-form-item>
                  <el-form-item label="名称"><el-input v-model="form.name" maxlength="128" show-word-limit /></el-form-item>
                </div>
                <el-form-item label="状态"><el-radio-group v-model="form.status">
                  <el-radio :value="1">启用</el-radio><el-radio :value="0">停用</el-radio>
                </el-radio-group></el-form-item>
                <el-form-item label="本节点 JSON 配置">
                  <el-input v-model="form.config" type="textarea" :rows="12" class="json-input" />
                </el-form-item>
                <div class="muted tip">配置仅属于当前节点，不继承父节点。保存后全局版本递增。</div>
                <el-form-item label="备注"><el-input v-model="form.remark" type="textarea" maxlength="500" show-word-limit /></el-form-item>
                <div class="actions"><el-button type="primary" @click="save">保存配置</el-button></div>
              </el-form>
            </template>
          </template>
        </section>
      </main>
      <main v-else-if="navigation==='subscribers'" class="content-panel">
        <div class="panel-title"><div><h2>接入平台与同步状态</h2><p class="muted">订阅者接收版本通知，再主动拉取变化节点。</p></div>
          <el-button type="primary" @click="showSubscriber=true">新增接入平台</el-button></div>
        <el-table :data="subscribers" stripe>
          <el-table-column prop="code" label="接入方" width="160" />
          <el-table-column prop="callbackUrl" label="回调 URL" min-width="290" />
          <el-table-column prop="acknowledgedRevision" label="已确认版本" width="130" />
          <el-table-column prop="failures" label="连续失败" width="105" />
          <el-table-column label="状态" width="100"><template #default="{row}">
            <el-tag :type="row.failures?'danger':'success'">{{ row.failures?'异常':'正常' }}</el-tag>
          </template></el-table-column>
          <el-table-column prop="lastError" label="最近错误" min-width="140" />
        </el-table>
        <el-dialog v-model="showSubscriber" title="新增接入平台" width="530">
          <el-form label-position="top">
            <el-form-item label="唯一编码"><el-input v-model="newSubscriber.code" /></el-form-item>
            <el-form-item label="Webhook 回调地址"><el-input v-model="newSubscriber.callbackUrl" /></el-form-item>
            <el-form-item label="共享签名密钥"><el-input v-model="newSubscriber.secret" type="password" show-password /></el-form-item>
            <p class="muted">回调域名需要在配置中心服务端允许列表中。</p>
          </el-form>
          <template #footer><el-button @click="showSubscriber=false">取消</el-button>
            <el-button type="primary" @click="addSubscriber">创建</el-button></template>
        </el-dialog>
      </main>
      <main v-else class="content-panel">
        <h2>系统说明</h2>
        <p>当前权威数据集：{{ epoch }}</p>
        <p>当前配置版本：{{ revision }}</p>
        <p>game → region → server_group → channel → sdk_app，所有配置只在对应节点保存。</p>
        <el-alert title="管理后台未设计登录和账号权限，必须通过内网或受控网关限制访问。" type="warning" show-icon :closable="false" />
      </main>
    </div>
  </div>
</template>
