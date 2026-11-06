<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { confirmDiscardChanges, registerUnsavedChecker } from '../../app/unsavedChanges'
import { sdkApi, apiErrorMessage } from '../../api/http'

type Env = 'prod' | 'test' | 'review'
interface ServiceEntry {
  serviceId: number
  code: string
  name: string
  description: string
  globalEnabled: boolean
  enabled: boolean
  effectiveEnabled: boolean
  config: Record<string, unknown>
}
interface EditableEntry extends ServiceEntry { configText: string }
const props = defineProps<{ targetId: number }>()
const endpoint = computed(() => `/admin/channel-apps/${props.targetId}/services`)
const env = ref<Env>('test')
const loading = ref(false)
const savingId = ref<number | null>(null)
const entries = ref<EditableEntry[]>([])
const savedById = ref<Record<number, string>>({})
function snapshot(item: EditableEntry): string {
  return JSON.stringify({ enabled: item.enabled, configText: item.configText })
}
function isDirty(item: EditableEntry): boolean { return savedById.value[item.serviceId] !== snapshot(item) }
const unregisterUnsaved = registerUnsavedChecker(
  () => entries.value.some(isDirty),
  () => { savedById.value = Object.fromEntries(entries.value.map(item => [item.serviceId, snapshot(item)])) }
)
onBeforeUnmount(unregisterUnsaved)
let sequence = 0

async function load() {
  const request = ++sequence
  loading.value = true
  try {
    const { data } = await sdkApi.get<ServiceEntry[]>(endpoint.value, {
      params: { env: env.value }
    })
    if (request !== sequence) return
    entries.value = data.map(item => ({ ...item, configText: JSON.stringify(item.config ?? {}, null, 2) }))
    savedById.value = Object.fromEntries(entries.value.map(item => [item.serviceId, snapshot(item)]))
  } catch (error) {
    if (request === sequence) ElMessage.error(apiErrorMessage(error, '加载渠道服务配置失败'))
  } finally {
    if (request === sequence) loading.value = false
  }
}

async function save(item: EditableEntry) {
  if (savingId.value !== null) return
  let config: Record<string, unknown>
  if ((item.code === 'event_report' || item.code === 'subscribe_message')) {
    config = {}
  } else {
    try {
      const parsed: unknown = JSON.parse(item.configText)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error()
      config = parsed as Record<string, unknown>
    } catch {
      ElMessage.warning('扩展配置必须是合法的 JSON 对象')
      return
    }
  }
  savingId.value = item.serviceId
  const selectedEnv = env.value
  try {
    await sdkApi.put(`${endpoint.value}/${item.serviceId}`, {
      enabled: item.enabled, config
    }, { params: { env: selectedEnv } })
    savedById.value = { ...savedById.value, [item.serviceId]: snapshot(item) }
    // 单独刷新已保存服务的生效状态，不覆盖同页其他尚未保存的服务。
    try {
      const { data } = await sdkApi.get<ServiceEntry[]>(endpoint.value, { params: { env: selectedEnv } })
      const updated = data.find(entry => entry.serviceId === item.serviceId)
      if (updated) {
        item.globalEnabled = updated.globalEnabled
        item.effectiveEnabled = updated.effectiveEnabled
      }
    } catch {
      ElMessage.warning('服务已保存，但生效状态刷新失败')
    }
    ElMessage.success('渠道服务配置已保存')
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '保存渠道服务配置失败'))
  } finally {
    savingId.value = null
  }
}

async function changeEnv(nextEnv: Env) {
  if (nextEnv === env.value || !(await confirmDiscardChanges())) return
  env.value = nextEnv
}

async function refresh() {
  if (!(await confirmDiscardChanges())) return
  await load()
}

watch(() => [props.targetId, env.value], () => { void load() }, { immediate: true })
</script>

<template>
  <div class="node-detail-content">
    <div class="section-heading">
      <div>
        <h3>渠道服务配置</h3>
        <p class="muted">未单独配置的服务默认关闭；全局关闭后渠道无法使用。</p>
      </div>
      <div class="panel-actions">
        <el-select :model-value="env" @change="changeEnv" :disabled="savingId !== null" style="width: 130px">
          <el-option label="测试环境" value="test" />
          <el-option label="正式环境" value="prod" />
          <el-option label="提审环境" value="review" />
        </el-select>
        <el-button @click="refresh">刷新</el-button>
      </div>
    </div>
    <div v-loading="loading">
      <el-empty v-if="!entries.length && !loading" description="暂无服务定义" />
      <div v-for="item in entries" :key="item.serviceId" class="channel-detail-panel" style="margin-bottom: 12px; padding: 16px">
        <div class="section-heading">
          <div>
            <h3>{{ item.name }} <el-tag size="small" type="info">{{ item.code }}</el-tag></h3>
            <p class="muted">{{ item.description }}</p>
          </div>
          <div class="panel-actions">
            <el-tag v-if="isDirty(item)" size="small" type="warning">未保存</el-tag>
            <el-tag v-else-if="item.effectiveEnabled" size="small" type="success">已生效</el-tag>
            <el-tag v-else-if="!item.globalEnabled" size="small" type="danger">全局已关闭</el-tag>
            <el-tag v-else size="small" type="info">{{ item.enabled ? '未生效' : '渠道已关闭' }}</el-tag>
            <el-switch v-model="item.enabled" :disabled="!item.globalEnabled || savingId !== null" />
            <el-button type="primary" :loading="savingId === item.serviceId"
                       :disabled="savingId !== null || !item.globalEnabled" @click="save(item)">保存</el-button>
          </div>
        </div>
        <p v-if="item.code === 'event_report' || item.code === 'subscribe_message'" class="muted">
          事件上报和订阅消息仅使用当前渠道环境开关；开启后同步转发平台，不保存业务数据。
        </p>
        <el-input v-else v-model="item.configText" type="textarea" :rows="3" spellcheck="false"
                  :disabled="!item.globalEnabled || savingId !== null"
                  placeholder="其他服务的扩展配置 JSON" />
      </div>
    </div>
  </div>
</template>
