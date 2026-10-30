<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../../api/http'
import JsonConfigEditor from '../../components/JsonConfigEditor.vue'
import type { RegionEnvType } from './types'

type GroupField = 'prodServerGroupId' | 'testServerGroupId' | 'reviewServerGroupId'
type AppForm = { code: string; name: string; status: number; config: string; remark: string } & Record<GroupField, number | null>
interface ChannelApp extends AppForm { id: number; gameId: number }
interface GroupOption { id: number; name: string; code: string; regionName: string }
const environments: { key: RegionEnvType; field: GroupField; label: string }[] = [
  { key: 'prod', field: 'prodServerGroupId', label: '正式服' },
  { key: 'test', field: 'testServerGroupId', label: '测试服' },
  { key: 'review', field: 'reviewServerGroupId', label: '提审服' }
]
const props = defineProps<{ gameId: number | null }>()
const emit = defineEmits<{ changed: [] }>()
const apps = ref<ChannelApp[]>([])
const active = ref<ChannelApp | null>(null)
const appSearch = ref('')
const loading = ref(false)
const saving = ref(false)
const visible = ref(false)
const editing = ref<number | null>(null)
const groupOptions = reactive<Record<RegionEnvType, GroupOption[]>>({ prod: [], test: [], review: [] })
const groupLoading = reactive<Record<RegionEnvType, boolean>>({ prod: false, test: false, review: false })
const groupSequences: Record<RegionEnvType, number> = { prod: 0, test: 0, review: 0 }
const form = ref<AppForm>(emptyForm())
let sequence = 0
const filteredApps = computed(() => apps.value.filter(item =>
  (item.name + item.code).toLowerCase().includes(appSearch.value.trim().toLowerCase())))

function emptyForm(): AppForm {
  return { code: '', name: '', status: 1, config: '{}', remark: '',
    prodServerGroupId: null, testServerGroupId: null, reviewServerGroupId: null }
}
function pretty(value: string): string {
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
}
function validJSON(value: string): boolean {
  try { const obj: unknown = JSON.parse(value); return obj !== null && typeof obj === 'object' && !Array.isArray(obj) }
  catch { return false }
}
async function loadApps() {
  const request = ++sequence
  if (!props.gameId) { apps.value = []; active.value = null; return }
  loading.value = true
  try {
    const { data } = await api.get<ChannelApp[]>('/admin/channel-apps', { params: { gameId: props.gameId } })
    if (request !== sequence) return
    apps.value = data
    active.value = data.find(item => item.id === active.value?.id) ?? data[0] ?? null
  } catch { if (request === sequence) ElMessage.error('加载渠道应用失败') }
  finally { if (request === sequence) loading.value = false }
}
function groupLabel(group: GroupOption): string {
  return `${group.regionName} / ${group.name} (${group.code})`
}
async function loadOptions(env: RegionEnvType, keyword = '') {
  if (!props.gameId) return
  const request = ++groupSequences[env]
  groupLoading[env] = true
  try {
    const { data } = await api.get<GroupOption[]>('/admin/channel-apps/server-groups', {
      params: { gameId: props.gameId, envType: env, keyword }
    })
    if (request === groupSequences[env]) groupOptions[env] = data
  } catch { if (request === groupSequences[env]) ElMessage.error('加载区服组选项失败') }
  finally { if (request === groupSequences[env]) groupLoading[env] = false }
}
function openEditor(app?: ChannelApp) {
  editing.value = app?.id ?? null
  form.value = app ? {
    code: app.code, name: app.name, status: app.status, config: pretty(app.config),
    remark: app.remark ?? '',
    prodServerGroupId: app.prodServerGroupId, testServerGroupId: app.testServerGroupId,
    reviewServerGroupId: app.reviewServerGroupId
  } : emptyForm()
  for (const env of environments) void loadOptions(env.key)
  visible.value = true
}
async function saveApp() {
  if (!props.gameId || !form.value.code.trim() || !form.value.name.trim() || !validJSON(form.value.config)) {
    ElMessage.warning('请填写名称、编码和合法的 JSON 对象'); return
  }
  saving.value = true
  try {
    const payload = { ...form.value, gameId: props.gameId }
    if (editing.value) await api.put(`/admin/channel-apps/${editing.value}`, payload)
    else await api.post('/admin/channel-apps', payload)
    visible.value = false
    await loadApps()
    emit('changed')
    ElMessage.success('渠道应用已保存')
  } catch { ElMessage.error('保存失败：渠道 Code 在所有游戏中必须唯一，请检查区服组所属游戏及环境') }
  finally { saving.value = false }
}
async function deleteApp(app: ChannelApp) {
  try { await ElMessageBox.confirm(`删除「${app.name}」？请先清空三个环境的区服组绑定。`,
    '删除渠道应用', { type: 'warning' }) } catch { return }
  try {
    await api.delete(`/admin/channel-apps/${app.id}`)
    active.value = null
    await loadApps()
    emit('changed')
    ElMessage.success('渠道应用已删除')
  } catch { ElMessage.error('删除失败，请先解除区服组绑定') }
}
function selectedOption(env: RegionEnvType, id: number | null): GroupOption[] {
  if (id === null || groupOptions[env].some(item => item.id === id)) return []
  return [{ id, name: `已绑定区服组 #${id}`, code: String(id), regionName: '' }]
}
function displayGroup(env: RegionEnvType, id: number | null): string {
  if (id === null) return '尚未绑定'
  const option = groupOptions[env].find(item => item.id === id)
  return option ? groupLabel(option) : `区服组 ID ${id}`
}
watch(() => props.gameId, () => { active.value = null; appSearch.value = ''; void loadApps() }, { immediate: true })
</script>

<template>
  <main class="channel-page">
    <div class="page-heading">
      <div><div class="eyebrow">CHANNEL APPLICATIONS</div><h1>渠道应用</h1>
        <p class="muted">一个 AppID 统一管理正式服、测试服、提审服；三个环境共用一份 JSON 配置。</p></div>
      <el-button type="primary" :disabled="!gameId" @click="openEditor()">+ 新增渠道应用</el-button>
    </div>
    <div class="channel-layout">
      <section class="channel-list-panel">
        <div class="section-heading"><h3>应用列表 <span class="muted">{{ apps.length }}</span></h3>
          <el-button link @click="loadApps">刷新</el-button></div>
        <el-input v-model="appSearch" clearable placeholder="搜索名称或 Code" />
        <div v-loading="loading" class="channel-list-scroll">
          <button v-for="app in filteredApps" :key="app.id" class="channel-list-item"
                  :class="{ active: active?.id === app.id }" @click="active = app">
            <span class="app-icon">{{ app.name.slice(0, 1) }}</span>
            <span class="app-item-text"><strong>{{ app.name }}</strong><small>{{ app.code }}</small></span>
            <el-tag v-if="app.status === 0" size="small" type="info">停用</el-tag>
          </button>
          <el-empty v-if="!loading && !filteredApps.length" description="暂无渠道应用" :image-size="75" />
        </div>
      </section>
      <section class="channel-detail-panel">
        <el-empty v-if="!active" description="选择左侧渠道应用查看详情" />
        <template v-else>
          <div class="channel-detail-header">
            <div><div class="eyebrow">CHANNEL DETAILS</div>
              <h2>{{ active.name }} <el-tag :type="active.status ? 'success' : 'info'" size="small">{{ active.status ? '已启用' : '已停用' }}</el-tag></h2>
              <p class="muted">Code: {{ active.code }} · ID {{ active.id }}</p></div>
            <div class="panel-actions">
              <el-button @click="openEditor(active)">编辑应用</el-button>
              <el-button type="danger" plain @click="deleteApp(active)">删除</el-button>
            </div>
          </div>
          <el-tabs class="channel-tabs">
            <el-tab-pane label="环境绑定">
              <div class="section-heading"><div><h3>三个环境的区服组</h3>
                <p class="muted">每个环境只能绑定一个区服组，未使用的环境可以留空。</p></div>
                <el-button @click="openEditor(active)">调整绑定</el-button></div>
              <el-descriptions :column="1" border>
                <el-descriptions-item v-for="env in environments" :key="env.key" :label="env.label">
                  {{ displayGroup(env.key, active[env.field]) }}
                </el-descriptions-item>
              </el-descriptions>
            </el-tab-pane>
            <el-tab-pane label="共享配置">
              <div class="section-heading"><div><h3>共享 JSON 配置</h3>
                <p class="muted">三个环境读取同一份 JSON，不存在独立覆盖配置。</p></div>
                <el-button @click="openEditor(active)">修改配置</el-button></div>
              <pre class="config-preview">{{ pretty(active.config) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="应用信息">
              <el-descriptions :column="1" border>
                <el-descriptions-item label="名称">{{ active.name }}</el-descriptions-item>
                <el-descriptions-item label="编码">{{ active.code }}</el-descriptions-item>
                <el-descriptions-item label="状态">{{ active.status ? '启用' : '停用' }}</el-descriptions-item>
                <el-descriptions-item label="备注">{{ active.remark || '—' }}</el-descriptions-item>
              </el-descriptions>
            </el-tab-pane>
          </el-tabs>
        </template>
      </section>
    </div>
    <el-drawer v-model="visible" :title="editing ? '编辑渠道应用' : '新增渠道应用'" size="min(650px, 96vw)" :close-on-click-modal="false">
      <el-form label-position="top">
        <div class="form-grid">
          <el-form-item label="编码" required><el-input v-model="form.code" :disabled="!!editing" maxlength="128" /></el-form-item>
          <el-form-item label="名称" required><el-input v-model="form.name" maxlength="128" /></el-form-item>
        </div>
        <el-form-item label="状态"><el-switch v-model="form.status" :active-value="1" :inactive-value="0" /></el-form-item>
        <el-form-item v-for="env in environments" :key="env.key" :label="env.label + '区服组（可选）'">
          <el-select v-model="form[env.field]" filterable remote clearable :value-on-clear="null" :remote-method="(keyword: string) => loadOptions(env.key, keyword)"
                     :loading="groupLoading[env.key]" placeholder="搜索当前游戏对应环境的区服组" style="width:100%">
            <el-option v-for="group in [...selectedOption(env.key, form[env.field]), ...groupOptions[env.key]]"
                       :key="group.id" :value="group.id" :label="groupLabel(group)" />
          </el-select>
        </el-form-item>
        <el-form-item label="共享配置（JSON 对象）" required><JsonConfigEditor v-model="form.config" :rows="12" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="form.remark" type="textarea" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="saveApp">保存应用</el-button></template>
    </el-drawer>
  </main>
</template>
