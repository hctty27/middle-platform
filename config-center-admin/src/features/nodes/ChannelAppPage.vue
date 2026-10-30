<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { nodeApi } from '../../api/nodeApi'
import { api } from '../../api/http'
import type { NodeMatch, NodeSummary } from './types'

interface ChannelApp {
  id: number
  gameId: number
  code: string
  name: string
  status: number
  config: string
  remark: string | null
}
interface Binding {
  id: number
  channelAppId: number
  serverGroupId: number
  status: number
  config: string
  effectiveConfig: string
}
const emit = defineEmits<{ changed: [] }>()
const games = ref<NodeSummary[]>([])
const gameId = ref<number | null>(null)
const apps = ref<ChannelApp[]>([])
const active = ref<ChannelApp | null>(null)
const bindings = ref<Binding[]>([])
const busy = ref(false)
const appVisible = ref(false)
const bindingVisible = ref(false)
const editApp = ref<number | null>(null)
const editBinding = ref<number | null>(null)
const appForm = ref({ code: '', name: '', status: 1, config: '{}', remark: '' })
const bindingForm = ref({ serverGroupId: null as number | null, status: 1, config: '{}' })
const groupKeyword = ref('')
const groupResults = ref<NodeMatch[]>([])
const groupLoading = ref(false)

async function loadApps() {
  if (!gameId.value) { apps.value = []; active.value = null; return }
  try {
    const response = await api.get<ChannelApp[]>('/admin/channel-apps', { params: { gameId: gameId.value } })
    apps.value = response.data
    if (active.value) {
      active.value = apps.value.find(item => item.id === active.value?.id) ?? null
      if (active.value) await loadBindings()
      else bindings.value = []
    }
  } catch { ElMessage.error('加载渠道应用失败') }
}
async function loadBindings() {
  if (!active.value) { bindings.value = []; return }
  try {
    bindings.value = (await api.get<Binding[]>(`/admin/channel-apps/${active.value.id}/bindings`)).data
  } catch { ElMessage.error('加载区服组绑定失败') }
}
function selectApp(item: ChannelApp) { active.value = item; void loadBindings() }
function openApp(item?: ChannelApp) {
  editApp.value = item?.id ?? null
  appForm.value = { code: item?.code ?? '', name: item?.name ?? '', status: item?.status ?? 1,
    config: item ? pretty(item.config) : '{}', remark: item?.remark ?? '' }
  appVisible.value = true
}
function pretty(value: string) {
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
}
function validJSON(text: string) {
  try { return JSON.parse(text) !== null && !Array.isArray(JSON.parse(text)) && typeof JSON.parse(text) === 'object' }
  catch { return false }
}
async function saveApp() {
  if (!gameId.value || !appForm.value.code.trim() || !appForm.value.name.trim() || !validJSON(appForm.value.config)) {
    ElMessage.warning('请填写名称、编码，并输入 JSON 对象'); return
  }
  busy.value = true
  try {
    const payload = { ...appForm.value, gameId: gameId.value }
    if (editApp.value) await api.put(`/admin/channel-apps/${editApp.value}`, payload)
    else await api.post('/admin/channel-apps', payload)
    appVisible.value = false
    await loadApps()
    emit('changed')
    ElMessage.success('应用配置已保存')
  } catch { ElMessage.error('保存失败：检查应用编码是否重复') }
  finally { busy.value = false }
}
async function removeApp(item: ChannelApp) {
  try { await ElMessageBox.confirm(`删除「${item.name}」？需先解除所有绑定。`, '确认删除', { type: 'warning' }) }
  catch { return }
  try {
    await api.delete(`/admin/channel-apps/${item.id}`)
    if (active.value?.id === item.id) { active.value = null; bindings.value = [] }
    await loadApps(); emit('changed'); ElMessage.success('已删除')
  } catch { ElMessage.error('删除失败：应用仍有关联或数据库操作失败') }
}
function openBinding(item?: Binding) {
  editBinding.value = item?.id ?? null
  bindingForm.value = { serverGroupId: item?.serverGroupId ?? null, status: item?.status ?? 1,
    config: item ? pretty(item.config) : '{}' }
  groupResults.value = []
  groupKeyword.value = ''
  bindingVisible.value = true
}
async function searchGroups(query: string) {
  groupKeyword.value = query
  if (!query.trim() || !gameId.value) { groupResults.value = []; return }
  groupLoading.value = true
  try {
    const response = await nodeApi.search(query)
    groupResults.value = response.data.filter(item => item.node.nodeType === 'server_group' && item.path[0]?.id === gameId.value)
  } catch { ElMessage.error('搜索区服组失败') }
  finally { groupLoading.value = false }
}
async function saveBinding() {
  if (!active.value || !bindingForm.value.serverGroupId || !validJSON(bindingForm.value.config)) {
    ElMessage.warning('选择区服组并填写合法 JSON 对象'); return
  }
  busy.value = true
  try {
    const payload = { status: bindingForm.value.status, config: bindingForm.value.config }
    if (editBinding.value) await api.put(`/admin/channel-app-bindings/${editBinding.value}`, payload)
    else await api.post(`/admin/channel-apps/${active.value.id}/bindings`,
      { ...payload, serverGroupId: bindingForm.value.serverGroupId })
    bindingVisible.value = false
    await loadBindings(); emit('changed')
    ElMessage.success('绑定配置已保存')
  } catch { ElMessage.error('保存失败：区服组可能属于其他游戏或已存在绑定') }
  finally { busy.value = false }
}
async function removeBinding(item: Binding) {
  try { await ElMessageBox.confirm(`解除区服组 ${item.serverGroupId} 的绑定？`, '确认解除', { type: 'warning' }) }
  catch { return }
  try {
    await api.delete(`/admin/channel-app-bindings/${item.id}`)
    await loadBindings(); emit('changed'); ElMessage.success('已解除绑定')
  } catch { ElMessage.error('解除绑定失败') }
}
watch(gameId, () => { active.value = null; bindings.value = []; void loadApps() })
onMounted(async () => {
  try {
    games.value = (await nodeApi.getTreeChildren(0)).data
    if (games.value.length) gameId.value = games.value[0].id
  } catch { ElMessage.error('加载游戏列表失败') }
})
</script>

<template>
  <main class="content-panel">
    <h2>渠道应用管理</h2>
    <p class="muted">一个游戏只创建一次渠道应用；在正式服、提审服等不同区服组绑定，并按需覆盖公共 JSON 配置。</p>
    <div class="panel-actions" style="margin: 16px 0; display:flex; gap:12px; align-items:center">
      <el-select v-model="gameId" placeholder="选择游戏" style="width:260px">
        <el-option v-for="game in games" :key="game.id" :label="game.name" :value="game.id" />
      </el-select>
      <el-button type="primary" :disabled="!gameId" @click="openApp()">新增渠道应用</el-button>
    </div>
    <el-table :data="apps" stripe @row-click="selectApp">
      <el-table-column prop="name" label="渠道应用" min-width="160" />
      <el-table-column prop="code" label="编码" min-width="130" />
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">{{ row.status ? '启用' : '停用' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <el-button link type="primary" @click.stop="selectApp(row)">绑定</el-button>
          <el-button link @click.stop="openApp(row)">编辑</el-button>
          <el-button link type="danger" @click.stop="removeApp(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div v-if="active" style="margin-top:30px">
      <div class="panel-actions" style="display:flex;align-items:center;justify-content:space-between">
        <h3>{{ active.name }} · 区服组绑定</h3>
        <el-button type="primary" @click="openBinding()">新增绑定</el-button>
      </div>
      <el-table :data="bindings" stripe>
        <el-table-column prop="serverGroupId" label="区服组 ID" width="140" />
        <el-table-column label="状态" width="90"><template #default="{row}">{{ row.status ? '启用' : '停用' }}</template></el-table-column>
        <el-table-column label="差异配置" min-width="190"><template #default="{row}"><code>{{ row.config }}</code></template></el-table-column>
        <el-table-column label="合并后配置" min-width="220"><template #default="{row}"><code>{{ row.effectiveConfig }}</code></template></el-table-column>
        <el-table-column label="操作" width="140"><template #default="{row}">
          <el-button link @click="openBinding(row)">编辑</el-button>
          <el-button link type="danger" @click="removeBinding(row)">解除</el-button>
        </template></el-table-column>
      </el-table>
    </div>
    <el-dialog v-model="appVisible" :title="editApp ? '编辑渠道应用' : '新增渠道应用'" width="min(650px, 94vw)" :close-on-click-modal="false">
      <el-form label-position="top">
        <el-form-item label="编码"><el-input v-model="appForm.code" :disabled="!!editApp" maxlength="128" /></el-form-item>
        <el-form-item label="名称"><el-input v-model="appForm.name" maxlength="128" /></el-form-item>
        <el-form-item label="状态"><el-switch v-model="appForm.status" :active-value="1" :inactive-value="0" /></el-form-item>
        <el-form-item label="公共配置（JSON 对象）"><el-input v-model="appForm.config" type="textarea" :rows="8" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="appForm.remark" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="appVisible=false">取消</el-button><el-button type="primary" :loading="busy" @click="saveApp">保存</el-button></template>
    </el-dialog>
    <el-dialog v-model="bindingVisible" title="配置区服组绑定" width="min(650px, 94vw)" :close-on-click-modal="false">
      <el-form label-position="top">
        <el-form-item label="选择区服组（输入名称或 Code 检索）">
          <el-select v-model="bindingForm.serverGroupId" filterable remote :remote-method="searchGroups" :loading="groupLoading"
            :disabled="!!editBinding" placeholder="搜索本游戏下的区服组" style="width:100%">
            <el-option v-if="editBinding && bindingForm.serverGroupId" :label="String(bindingForm.serverGroupId)" :value="bindingForm.serverGroupId" />
            <el-option v-for="match in groupResults" :key="match.node.id" :value="match.node.id"
              :label="match.path.map(item => item.name).join(' / ')" />
          </el-select>
        </el-form-item>
        <el-form-item label="绑定状态"><el-switch v-model="bindingForm.status" :active-value="1" :inactive-value="0" /></el-form-item>
        <el-form-item label="差异配置（JSON 对象）"><el-input v-model="bindingForm.config" type="textarea" :rows="8" /></el-form-item>
        <p class="muted">最终配置 = 游戏渠道公共配置 + 此区服组覆盖配置；相同键以覆盖配置为准。</p>
      </el-form>
      <template #footer><el-button @click="bindingVisible=false">取消</el-button><el-button type="primary" :loading="busy" @click="saveBinding">保存绑定</el-button></template>
    </el-dialog>
  </main>
</template>
