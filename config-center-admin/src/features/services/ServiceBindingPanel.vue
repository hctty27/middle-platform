<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api, apiErrorMessage } from '../../api/http'

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
let sequence = 0

async function load() {
  const request = ++sequence
  loading.value = true
  try {
    const { data } = await api.get<ServiceEntry[]>(endpoint.value, {
      params: { env: env.value }
    })
    if (request !== sequence) return
    entries.value = data.map(item => ({ ...item, configText: JSON.stringify(item.config ?? {}, null, 2) }))
  } catch (error) {
    if (request === sequence) ElMessage.error(apiErrorMessage(error, '加载渠道服务配置失败'))
  } finally {
    if (request === sequence) loading.value = false
  }
}

async function save(item: EditableEntry) {
  if (savingId.value !== null) return
  let config: Record<string, unknown>
  if (item.code === 'event_report') {
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
    await api.put(`${endpoint.value}/${item.serviceId}`, {
      enabled: item.enabled, config
    }, { params: { env: selectedEnv } })
    ElMessage.success('渠道服务配置已保存')
    await load()
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '保存渠道服务配置失败'))
  } finally {
    savingId.value = null
  }
}

watch(() => [props.targetId, env.value], () => {
  void load()
}, { immediate: true })
</script>

<template>
  <div class="node-detail-content">
    <div class="section-heading">
      <div>
        <h3>渠道服务配置</h3>
        <p class="muted">未单独配置的服务默认关闭；全局关闭后渠道无法使用。</p>
      </div>
      <div class="panel-actions">
        <el-select v-model="env" :disabled="savingId !== null" style="width: 130px">
          <el-option label="测试环境" value="test" />
          <el-option label="正式环境" value="prod" />
          <el-option label="提审环境" value="review" />
        </el-select>
        <el-button @click="load">刷新</el-button>
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
            <el-tag v-if="!item.globalEnabled" size="small" type="danger">全局已关闭</el-tag>
            <el-switch v-model="item.enabled" :disabled="!item.globalEnabled || savingId !== null" />
            <el-button type="primary" :loading="savingId === item.serviceId"
                       :disabled="savingId !== null || !item.globalEnabled" @click="save(item)">保存</el-button>
          </div>
        </div>
        <p v-if="item.code === 'event_report'" class="muted">
          事件上报仅使用当前渠道及环境开关；开启后同步转发平台，不配置事件规则，也不保存事件数据。
        </p>
        <el-input v-else v-model="item.configText" type="textarea" :rows="3" spellcheck="false"
                  :disabled="!item.globalEnabled || savingId !== null"
                  placeholder="其他服务的扩展配置 JSON" />
      </div>
    </div>
  </div>
</template>
