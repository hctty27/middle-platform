<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { nodeApi } from '../../api/nodeApi'
import { api } from '../../api/http'
import { regionEnvLabels } from './types'
import type { NodeSummary, RegionEnvType } from './types'

interface ChannelApp {
  id: number; gameId: number; code: string; name: string
  status: number; config: string; remark: string | null
}
interface Binding {
  id: number; channelAppId: number; serverGroupId: number
  serverGroupName?: string; regionName?: string; regionEnvType?: RegionEnvType | null
  status: number; config: string; effectiveConfig: string
}
const props = defineProps<{ gameId: number | null }>()
const emit = defineEmits<{ changed: [] }>()
const apps = ref<ChannelApp[]>([])
const active = ref<ChannelApp | null>(null)
const bindings = ref<Binding[]>([])
const appSearch = ref('')
const appLoading = ref(false)
const bindingLoading = ref(false)
const bindingSearch = ref('')
const envFilter = ref<RegionEnvType | ''>('')
const activeTab = ref<'bindings' | 'config' | 'details'>('bindings')
const busy = ref(false)
const appVisible = ref(false)
const bindingVisible = ref(false)
const editApp = ref<number | null>(null)
const editBinding = ref<number | null>(null)
const appForm = ref({ code: '', name: '', status: 1, config: '{}', remark: '' })
const bindingForm = ref({ regionId: null as number | null, serverGroupId: null as number | null, status: 1, config: '{}' })
const regions = ref<NodeSummary[]>([])
const groups = ref<NodeSummary[]>([])
const groupLoading = ref(false)
const preview = ref<Binding | null>(null)
const previewVisible = ref(false)
const filteredApps = computed(() => apps.value.filter(app =>
  (app.name + app.code).toLowerCase().includes(appSearch.value.trim().toLowerCase())))
const filteredBindings = computed(() => bindings.value.filter(item =>
  (!envFilter.value || item.regionEnvType === envFilter.value) &&
  (item.regionName + item.serverGroupName + item.serverGroupId).toLowerCase().includes(bindingSearch.value.trim().toLowerCase())))
let appSequence = 0
let bindingSequence = 0
let groupSequence = 0

function pretty(value: string) {
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
}
function validJSON(value: string) {
  try { const v: unknown = JSON.parse(value); return v !== null && typeof v === 'object' && !Array.isArray(v) }
  catch { return false }
}
async function loadApps() {
  const id = ++appSequence
  if (!props.gameId) { apps.value = []; active.value = null; bindings.value = []; return }
  appLoading.value = true
  try {
    const response = await api.get<ChannelApp[]>('/admin/channel-apps', { params: { gameId: props.gameId } })
    if (id !== appSequence) return
    apps.value = response.data
    active.value = apps.value.find(item => item.id === active.value?.id) ?? apps.value[0] ?? null
    await loadBindings()
  } catch {
    if (id === appSequence) ElMessage.error('加载渠道应用失败')
  } finally { if (id === appSequence) appLoading.value = false }
}
async function loadBindings() {
  const id = ++bindingSequence
  if (!active.value) { bindings.value = []; return }
  bindingLoading.value = true
  try {
    const response = await api.get<Binding[]>(`/admin/channel-apps/${active.value.id}/bindings`)
    if (id === bindingSequence) bindings.value = response.data
  } catch {
    if (id === bindingSequence) ElMessage.error('加载绑定失败')
  } finally { if (id === bindingSequence) bindingLoading.value = false }
}
function selectApp(app: ChannelApp) {
  if (active.value?.id === app.id) return
  active.value = app
  envFilter.value = ''
  bindingSearch.value = ''
  activeTab.value = 'bindings'
  bindings.value = []
  void loadBindings()
}
function openApp(item?: ChannelApp) {
  editApp.value = item?.id ?? null
  appForm.value = {
    code: item?.code ?? '', name: item?.name ?? '', status: item?.status ?? 1,
    config: item ? pretty(item.config) : '{}', remark: item?.remark ?? ''
  }
  appVisible.value = true
}
async function saveApp() {
  if (!props.gameId || !appForm.value.code.trim() || !appForm.value.name.trim() || !validJSON(appForm.value.config)) {
    ElMessage.warning('请填写名称、编码及合法 JSON 对象'); return
  }
  busy.value = true
  try {
    const payload = { ...appForm.value, gameId: props.gameId }
    if (editApp.value) await api.put(`/admin/channel-apps/${editApp.value}`, payload)
    else await api.post('/admin/channel-apps', payload)
    appVisible.value = false
    await loadApps()
    emit('changed')
    ElMessage.success('渠道应用已保存')
  } catch { ElMessage.error('保存失败，请检查渠道编码是否重复') }
  finally { busy.value = false }
}
async function removeApp(app: ChannelApp) {
  try { await ElMessageBox.confirm(`删除「${app.name}」？请先解除所有区服组绑定。`, '删除渠道应用', { type: 'warning' }) }
  catch { return }
  try {
    await api.delete(`/admin/channel-apps/${app.id}`)
    if (active.value?.id === app.id) { active.value = null; bindings.value = [] }
    await loadApps()
    emit('changed')
    ElMessage.success('已删除渠道应用')
  } catch { ElMessage.error('删除失败，渠道应用可能仍有关联') }
}
async function loadRegions() {
  if (!props.gameId) return
  try { regions.value = (await nodeApi.getTreeChildren(props.gameId)).data }
  catch { ElMessage.error('加载大区列表失败') }
}
async function loadGroups(query = '') {
  const id = ++groupSequence
  if (!bindingForm.value.regionId) { groups.value = []; return }
  groupLoading.value = true
  try {
    const response = await nodeApi.getChildren({
      parentId: bindingForm.value.regionId, page: 1, size: 100, keyword: query
    })
    if (id === groupSequence) groups.value = response.data.items
  } catch {
    if (id === groupSequence) ElMessage.error('加载区服组失败')
  } finally { if (id === groupSequence) groupLoading.value = false }
}
function onRegionChange() {
  bindingForm.value.serverGroupId = null
  void loadGroups()
}
function openBinding(item?: Binding) {
  editBinding.value = item?.id ?? null
  bindingForm.value = {
    regionId: null, serverGroupId: item?.serverGroupId ?? null,
    status: item?.status ?? 1, config: item ? pretty(item.config) : '{}'
  }
  regions.value = []
  groups.value = []
  bindingVisible.value = true
  if (!item) void loadRegions()
}
async function saveBinding() {
  if (!active.value || !bindingForm.value.serverGroupId || !validJSON(bindingForm.value.config)) {
    ElMessage.warning('请选择区服组并输入合法 JSON 对象'); return
  }
  busy.value = true
  try {
    const payload = { status: bindingForm.value.status, config: bindingForm.value.config }
    if (editBinding.value) await api.put(`/admin/channel-app-bindings/${editBinding.value}`, payload)
    else await api.post(`/admin/channel-apps/${active.value.id}/bindings`,
      { ...payload, serverGroupId: bindingForm.value.serverGroupId })
    bindingVisible.value = false
    await loadBindings()
    emit('changed')
    ElMessage.success('绑定已保存')
  } catch { ElMessage.error('保存失败，可能已绑定或区服组不属于当前游戏') }
  finally { busy.value = false }
}
async function removeBinding(item: Binding) {
  try { await ElMessageBox.confirm(`解除「${item.regionName ?? ''} / ${item.serverGroupName ?? item.serverGroupId}」的绑定？`, '解除绑定', { type: 'warning' }) }
  catch { return }
  try {
    await api.delete(`/admin/channel-app-bindings/${item.id}`)
    await loadBindings()
    emit('changed')
    ElMessage.success('已解除绑定')
  } catch { ElMessage.error('解除绑定失败') }
}
function openPreview(item: Binding) { preview.value = item; previewVisible.value = true }

watch(() => props.gameId, () => {
  active.value = null
  bindings.value = []
  appSearch.value = ''
  bindingSearch.value = ''
  envFilter.value = ''
  void loadApps()
}, { immediate: true })
</script>

<template>
  <main class="channel-page">
    <div class="page-heading">
      <div><div class="eyebrow">CHANNEL APPLICATIONS</div><h1>渠道应用</h1>
        <p class="muted">一个游戏只需创建一次渠道应用，通过绑定关联不同环境的区服组。</p></div>
      <el-button type="primary" :disabled="!gameId" @click="openApp()">+ 新增渠道应用</el-button>
    </div>
    <div class="channel-layout">
      <section class="channel-list-panel">
        <div class="section-heading"><h3>应用列表 <span class="muted">{{ apps.length }}</span></h3>
          <el-button link @click="loadApps">刷新</el-button></div>
        <el-input v-model="appSearch" clearable placeholder="搜索名称或 Code" />
        <div v-loading="appLoading" class="channel-list-scroll">
          <button v-for="app in filteredApps" :key="app.id" class="channel-list-item"
                  :class="{ active: active?.id === app.id }" @click="selectApp(app)">
            <span class="app-icon">{{ app.name.slice(0, 1) }}</span>
            <span class="app-item-text"><strong>{{ app.name }}</strong><small>{{ app.code }}</small></span>
            <el-tag v-if="app.status === 0" size="small" type="info">停用</el-tag>
          </button>
          <el-empty v-if="!appLoading && !filteredApps.length" description="暂无渠道应用" :image-size="75" />
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
              <el-button @click="openApp(active)">编辑应用</el-button>
              <el-button type="danger" plain @click="removeApp(active)">删除</el-button>
            </div>
          </div>
          <el-tabs v-model="activeTab" class="channel-tabs">
            <el-tab-pane label="区服组绑定" name="bindings">
              <div class="section-heading">
                <div><h3>已绑定区服组 <span class="muted">{{ bindings.length }}</span></h3>
                  <p class="muted">同一应用可以绑定正式服、提审服及测试服。</p></div>
                <el-button type="primary" @click="openBinding()">+ 新增绑定</el-button>
              </div>
              <div class="filter-toolbar">
                <el-select v-model="envFilter" clearable placeholder="全部环境" style="width: 160px">
                  <el-option v-for="(label, key) in regionEnvLabels" :key="key" :label="label" :value="key" />
                </el-select>
                <el-input v-model="bindingSearch" clearable placeholder="搜索大区 / 区服组" />
              </div>
              <el-table v-loading="bindingLoading" :data="filteredBindings" stripe empty-text="暂无匹配的区服组绑定">
                <el-table-column label="区服组" min-width="200">
                  <template #default="{ row }">
                    <div class="table-main">{{ row.serverGroupName || ('区服组 ' + row.serverGroupId) }}</div>
                    <div class="muted">{{ row.regionName || '大区未知' }} · ID {{ row.serverGroupId }}</div>
                  </template>
                </el-table-column>
                <el-table-column label="环境" width="110">
                  <template #default="{ row }">
                    <el-tag v-if="row.regionEnvType" size="small" :type="row.regionEnvType === 'prod' ? 'success' : row.regionEnvType === 'test' ? 'warning' : 'primary'">
                      {{ regionEnvLabels[row.regionEnvType as RegionEnvType] }}</el-tag>
                    <span v-else class="muted">未分类</span>
                  </template>
                </el-table-column>
                <el-table-column label="状态" width="88">
                  <template #default="{ row }"><el-tag :type="row.status ? 'success' : 'info'" size="small">{{ row.status ? '启用' : '停用' }}</el-tag></template>
                </el-table-column>
                <el-table-column label="操作" width="190" fixed="right">
                  <template #default="{ row }">
                    <el-button link type="primary" @click="openPreview(row)">预览配置</el-button>
                    <el-button link @click="openBinding(row)">编辑</el-button>
                    <el-button link type="danger" @click="removeBinding(row)">解除</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </el-tab-pane>
            <el-tab-pane label="公共配置" name="config">
              <div class="section-heading"><div><h3>公共 JSON 配置</h3>
                <p class="muted">所有绑定的区服组默认继承此配置。</p></div>
                <el-button @click="openApp(active)">修改公共配置</el-button></div>
              <pre class="config-preview">{{ pretty(active.config) }}</pre>
            </el-tab-pane>
            <el-tab-pane label="应用信息" name="details">
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

    <el-drawer v-model="appVisible" :title="editApp ? '编辑渠道应用' : '新增渠道应用'" size="min(650px, 96vw)" :close-on-click-modal="false">
      <el-form label-position="top">
        <div class="form-grid">
          <el-form-item label="编码" required><el-input v-model="appForm.code" :disabled="!!editApp" maxlength="128" /></el-form-item>
          <el-form-item label="名称" required><el-input v-model="appForm.name" maxlength="128" /></el-form-item>
        </div>
        <el-form-item label="状态"><el-switch v-model="appForm.status" :active-value="1" :inactive-value="0" /></el-form-item>
        <el-form-item label="公共配置（JSON 对象）" required><el-input v-model="appForm.config" type="textarea" class="json-input" :rows="14" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="appForm.remark" type="textarea" /></el-form-item>
      </el-form>
      <template #footer><el-button @click="appVisible = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="saveApp">保存应用</el-button></template>
    </el-drawer>

    <el-drawer v-model="bindingVisible" :title="editBinding ? '编辑区服组绑定' : '新增区服组绑定'"
               size="min(650px, 96vw)" :close-on-click-modal="false">
      <el-form label-position="top">
        <template v-if="!editBinding">
          <el-form-item label="选择大区" required>
            <el-select v-model="bindingForm.regionId" filterable placeholder="从当前游戏选择大区"
                       style="width:100%" @change="onRegionChange">
              <el-option v-for="region in regions" :key="region.id" :value="region.id"
                         :label="`${region.name} (${region.envType ? regionEnvLabels[region.envType] : '未分类'})`" />
            </el-select>
          </el-form-item>
          <el-form-item label="选择区服组" required>
            <el-select v-model="bindingForm.serverGroupId" filterable remote :remote-method="loadGroups"
                       :loading="groupLoading" :disabled="!bindingForm.regionId"
                       placeholder="输入名称或 Code 搜索当前大区的区服组" style="width:100%">
              <el-option v-for="group in groups" :key="group.id" :value="group.id"
                         :label="`${group.name} (${group.code})`" />
            </el-select>
          </el-form-item>
        </template>
        <el-form-item v-else label="区服组 ID"><el-input :model-value="String(bindingForm.serverGroupId)" disabled /></el-form-item>
        <el-form-item label="绑定状态"><el-switch v-model="bindingForm.status" :active-value="1" :inactive-value="0" /></el-form-item>
        <el-form-item label="覆盖 JSON 配置" required>
          <el-input v-model="bindingForm.config" type="textarea" class="json-input" :rows="14" />
        </el-form-item>
        <p class="muted">最终生效配置由 Go 后端按公共配置与覆盖配置合并计算。</p>
      </el-form>
      <template #footer><el-button @click="bindingVisible = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="saveBinding">保存绑定</el-button></template>
    </el-drawer>

    <el-drawer v-model="previewVisible" title="配置预览（服务端计算）" size="min(680px, 96vw)">
      <div v-if="preview">
        <p class="muted">{{ preview.regionName }} / {{ preview.serverGroupName }}</p>
        <h4>当前覆盖配置</h4><pre class="config-preview">{{ pretty(preview.config) }}</pre>
        <h4>最终生效配置</h4><pre class="config-preview">{{ pretty(preview.effectiveConfig) }}</pre>
      </div>
    </el-drawer>
  </main>
</template>
