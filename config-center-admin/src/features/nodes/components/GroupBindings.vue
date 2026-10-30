<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '../../../api/http'

interface AppOption { id: number; name: string; code: string }
interface GroupBinding {
  id: number; channelAppId: number; channelAppName: string; channelAppCode: string
  serverGroupId: number; status: number; config: string; effectiveConfig: string
}

const props = defineProps<{ gameId: number; groupId: number }>()
const emit = defineEmits<{ changed: [] }>()
const bindings = ref<GroupBinding[]>([])
const apps = ref<AppOption[]>([])
const loading = ref(false)
const saving = ref(false)
const visible = ref(false)
const editId = ref<number | null>(null)
const form = ref({ channelAppId: null as number | null, status: 1, config: '{}' })
const preview = ref<GroupBinding | null>(null)
let sequence = 0

function pretty(value: string) {
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
}
function validObject(value: string) {
  try { const data: unknown = JSON.parse(value); return data !== null && typeof data === 'object' && !Array.isArray(data) }
  catch { return false }
}
async function reload() {
  const id = ++sequence
  loading.value = true
  try {
    const [bindingResponse, appResponse] = await Promise.all([
      api.get<GroupBinding[]>(`/admin/server-groups/${props.groupId}/channel-app-bindings`),
      api.get<AppOption[]>('/admin/channel-apps', { params: { gameId: props.gameId } })
    ])
    if (id === sequence) { bindings.value = bindingResponse.data; apps.value = appResponse.data }
  } catch {
    if (id === sequence) ElMessage.error('加载区服组渠道绑定失败')
  } finally {
    if (id === sequence) loading.value = false
  }
}
function openAdd() {
  editId.value = null
  form.value = { channelAppId: null, status: 1, config: '{}' }
  visible.value = true
}
function openEdit(binding: GroupBinding) {
  editId.value = binding.id
  form.value = { channelAppId: binding.channelAppId, status: binding.status, config: pretty(binding.config) }
  visible.value = true
}
async function save() {
  if (!form.value.channelAppId || !validObject(form.value.config)) {
    ElMessage.warning('请选择渠道应用并填写 JSON 对象')
    return
  }
  saving.value = true
  try {
    const payload = { status: form.value.status, config: form.value.config }
    if (editId.value) {
      await api.put(`/admin/channel-app-bindings/${editId.value}`, payload)
    } else {
      await api.post(`/admin/channel-apps/${form.value.channelAppId}/bindings`,
        { ...payload, serverGroupId: props.groupId })
    }
    visible.value = false
    await reload()
    emit('changed')
    ElMessage.success('绑定已保存')
  } catch {
    ElMessage.error('保存绑定失败，请检查是否重复绑定')
  } finally { saving.value = false }
}
async function remove(binding: GroupBinding) {
  try { await ElMessageBox.confirm(`解除「${binding.channelAppName}」与当前区服组的绑定？`, '解除绑定', { type: 'warning' }) }
  catch { return }
  try {
    await api.delete(`/admin/channel-app-bindings/${binding.id}`)
    await reload()
    emit('changed')
    ElMessage.success('已解除绑定')
  } catch { ElMessage.error('解除绑定失败') }
}
watch(() => [props.groupId, props.gameId], () => void reload(), { immediate: true })
</script>

<template>
  <div class="group-bindings">
    <div class="section-heading">
      <div><h3>已绑定渠道应用</h3><p class="muted">可直接在区服组下新增、修改或解除渠道绑定。</p></div>
      <el-button type="primary" @click="openAdd">+ 绑定渠道应用</el-button>
    </div>
    <el-table v-loading="loading" :data="bindings" stripe empty-text="暂无绑定的渠道应用">
      <el-table-column label="渠道应用" min-width="160">
        <template #default="{ row }"><div class="table-main">{{ row.channelAppName }}</div>
          <div class="muted">{{ row.channelAppCode }}</div></template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }"><el-tag :type="row.status ? 'success' : 'info'" size="small">
          {{ row.status ? '已启用' : '已停用' }}</el-tag></template>
      </el-table-column>
      <el-table-column label="覆盖配置" min-width="180" show-overflow-tooltip>
        <template #default="{ row }"><code>{{ row.config }}</code></template>
      </el-table-column>
      <el-table-column label="操作" width="195" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" @click="preview = row">预览</el-button>
          <el-button link @click="openEdit(row)">编辑</el-button>
          <el-button link type="danger" @click="remove(row)">解除</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-drawer v-model="visible" :title="editId ? '编辑渠道绑定' : '绑定渠道应用'" size="min(620px, 96vw)" :close-on-click-modal="false">
      <el-form label-position="top">
        <el-form-item label="渠道应用" required>
          <el-select v-model="form.channelAppId" filterable :disabled="!!editId" placeholder="选择当前游戏的渠道应用" style="width:100%">
            <el-option v-for="app in apps.filter(a => editId || !bindings.some(b => b.channelAppId === a.id))"
              :key="app.id" :label="`${app.name} (${app.code})`" :value="app.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="绑定状态"><el-switch v-model="form.status" :active-value="1" :inactive-value="0" /></el-form-item>
        <el-form-item label="区服组覆盖配置（JSON 对象）" required>
          <el-input v-model="form.config" type="textarea" class="json-input" :rows="14" />
        </el-form-item>
        <p class="muted">公共配置由渠道应用管理维护。本次保存只修改该区服组的覆盖配置。</p>
      </el-form>
      <template #footer><el-button @click="visible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="save">保存绑定</el-button></template>
    </el-drawer>
    <el-drawer v-model="preview" title="最终生效配置（服务端计算）" size="min(650px, 96vw)">
      <div v-if="preview">
        <p class="muted">{{ preview.channelAppName }} · 当前区服组</p>
        <h4>区服组覆盖配置</h4><pre class="config-preview">{{ pretty(preview.config) }}</pre>
        <h4>最终生效配置</h4><pre class="config-preview">{{ pretty(preview.effectiveConfig) }}</pre>
      </div>
    </el-drawer>
  </div>
</template>
