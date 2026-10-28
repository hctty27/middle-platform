<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'

type NodeType = 'game' | 'region' | 'server_group' | 'channel' | 'sdk_app'
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
interface TreeNode extends NodeRecord { children: TreeNode[] }
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
const nodes = ref<NodeRecord[]>([])
const subscribers = ref<Subscriber[]>([])
const selected = ref<NodeRecord | null>(null)
const editing = ref(false)
const loading = ref(false)
const revision = ref(0)
const epoch = ref('')
const search = ref('')
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

const tree = computed<TreeNode[]>(() => {
  const byId = new Map<number, TreeNode>()
  for (const n of nodes.value) byId.set(n.id, { ...n, children: [] })
  const roots: TreeNode[] = []
  for (const n of byId.values()) {
    const parent = byId.get(n.parentId)
    if (parent) parent.children.push(n)
    else roots.push(n)
  }
  return roots
})
const filteredTree = computed(() => {
  if (!search.value.trim()) return tree.value
  const word = search.value.trim().toLowerCase()
  const filter = (items: TreeNode[]): TreeNode[] => items.flatMap(n => {
    const children = filter(n.children)
    return n.name.toLowerCase().includes(word) || n.code.toLowerCase().includes(word) || children.length
      ? [{ ...n, children }] : []
  })
  return filter(tree.value)
})
const pathInfo = computed(() => selected.value ? labels[selected.value.nodeType] + ' / ' + selected.value.code : '新建配置')

async function refresh() {
  loading.value = true
  try {
    const [nodeResult, revisionResult] = await Promise.all([
      api.get<NodeRecord[]>('/admin/nodes'),
      api.get<{ epoch: string; revision: number }>('/sync/revision')
    ])
    nodes.value = nodeResult.data
    revision.value = revisionResult.data.revision
    epoch.value = revisionResult.data.epoch
    if (selected.value) selected.value = nodes.value.find(n => n.id === selected.value?.id) ?? null
  } catch {
    ElMessage.error('读取配置中心失败')
  } finally {
    loading.value = false
  }
}
function choose(node: NodeRecord) {
  selected.value = node
  editing.value = false
  Object.assign(form, node, { remark: node.remark ?? '' })
  try { form.config = JSON.stringify(JSON.parse(node.config), null, 2) }
  catch { form.config = node.config }
}
function newGame() {
  selected.value = null
  editing.value = true
  Object.assign(form, { parentId: 0, nodeType: 'game', code: '', name: '', status: 1, config: '{}', remark: '' })
}
function newChild() {
  if (!selected.value) return
  const type = nextType[selected.value.nodeType]
  if (!type) return
  const id = selected.value.id
  selected.value = null
  editing.value = true
  Object.assign(form, { parentId: id, nodeType: type, code: '', name: '', status: 1, config: '{}', remark: '' })
}
async function save() {
  if (!form.name.trim() || (editing.value && !form.code.trim())) {
    ElMessage.warning('请填写名称和编码')
    return
  }
  try { JSON.parse(form.config) }
  catch { ElMessage.error('JSON 格式不正确'); return }
  try {
    if (editing.value) {
      const result = await api.post<NodeRecord>('/admin/nodes', form)
      await refresh()
      choose(result.data)
    } else if (selected.value) {
      const result = await api.put<NodeRecord>(`/admin/nodes/${selected.value.id}`, {
        name: form.name, status: form.status, config: form.config, remark: form.remark
      })
      await refresh()
      choose(result.data)
    }
    ElMessage.success('配置已保存，变更将通知接入平台')
  } catch {
    ElMessage.error('保存失败，请检查节点约束和编码唯一性')
  }
}
async function remove() {
  if (!selected.value) return
  await ElMessageBox.confirm('确认删除该节点？有子节点时不可删除。', '删除确认', { type: 'warning' })
  try {
    await api.delete(`/admin/nodes/${selected.value.id}`)
    selected.value = null
    await refresh()
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
onMounted(refresh)
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
          <div class="panel-title"><strong>配置树</strong><el-button type="primary" size="small" @click="newGame">+ 游戏</el-button></div>
          <el-input v-model="search" placeholder="搜索名称 / Code" clearable />
          <el-tree :data="filteredTree" node-key="id" default-expand-all highlight-current
                   :props="{label:'name', children:'children'}" @node-click="choose">
            <template #default="{data}">
              <div class="node-row"><span>{{ data.name }}</span><el-tag size="small" type="info" effect="plain">{{ labels[data.nodeType as NodeType] }}</el-tag></div>
            </template>
          </el-tree>
          <el-empty v-if="!loading && !nodes.length" description="暂无配置，点击新增游戏" :image-size="84" />
        </section>
        <section class="editor-panel">
          <div class="panel-title"><div><strong>{{ editing ? '新增配置节点' : (selected?.name || '选择配置节点') }}</strong>
            <div class="muted">{{ pathInfo }}</div></div>
            <div v-if="selected && !editing">
              <el-button :disabled="selected.nodeType==='sdk_app'" @click="newChild">新增子节点</el-button>
              <el-button type="danger" plain @click="remove">删除</el-button>
            </div>
          </div>
          <el-empty v-if="!selected && !editing" description="从左侧选择一个配置节点" />
          <el-form v-else label-position="top" class="node-form">
            <div class="form-grid">
              <el-form-item label="节点类型"><el-input :model-value="labels[form.nodeType]" disabled /></el-form-item>
              <el-form-item label="父节点 ID"><el-input :model-value="String(form.parentId)" disabled /></el-form-item>
              <el-form-item label="业务编码（不可修改）"><el-input v-model="form.code" :disabled="!editing" maxlength="128" /></el-form-item>
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
