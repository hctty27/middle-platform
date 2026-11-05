<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { sdkApi, apiErrorMessage } from '../../api/http'
import { nodeApi } from '../../api/nodeApi'
import BusinessConfigEditor from '../nodes/components/BusinessConfigEditor.vue'
import { validateBusinessConfig } from '../nodes/businessConfig'
import type { BusinessConfigType } from '../nodes/businessConfig'

interface Option { id: number; name: string; nodeType: BusinessConfigType }
interface NodeSDKConfig { nodeId: number; nodeType: BusinessConfigType; config: string }
const props = defineProps<{ gameId: number | null }>()
const options = ref<Option[]>([])
const currentID = ref<number | null>(null)
const rawConfig = ref('{}')
const loading = ref(false)
const saving = ref(false)
let sequence = 0
const current = computed(() => options.value.find(item => item.id === currentID.value) ?? null)

async function loadNodes() {
  const request = ++sequence
  options.value = []
  currentID.value = null
  rawConfig.value = '{}'
  if (!props.gameId) return
  loading.value = true
  try {
    const [game, regions] = await Promise.all([
      nodeApi.getNode(props.gameId),
      nodeApi.getTreeChildren(props.gameId)
    ])
    if (request !== sequence) return
    options.value = [
      { id: game.data.id, name: game.data.name, nodeType: 'game' },
      ...regions.data.filter(node => node.nodeType === 'region').map(node => ({
        id: node.id, name: node.name, nodeType: 'region' as const
      }))
    ]
    currentID.value = props.gameId
    await loadConfig()
  } catch (error) {
    if (request === sequence) ElMessage.error(apiErrorMessage(error, '加载 SDK 节点失败'))
  } finally {
    if (request === sequence) loading.value = false
  }
}
async function loadConfig() {
  const id = currentID.value
  const request = ++sequence
  if (!id) { rawConfig.value = '{}'; return }
  loading.value = true
  try {
    const { data } = await sdkApi.get<NodeSDKConfig>(`/admin/node-configs/${id}`)
    if (request !== sequence) return
    rawConfig.value = JSON.stringify(JSON.parse(data.config), null, 2)
  } catch (error) {
    if (request === sequence) ElMessage.error(apiErrorMessage(error, '读取 SDK 配置失败'))
  } finally {
    if (request === sequence) loading.value = false
  }
}
async function save() {
  if (!current.value) return
  try { validateBusinessConfig(current.value.nodeType, rawConfig.value) }
  catch (error) { ElMessage.warning(error instanceof Error ? error.message : 'SDK 配置不合法'); return }
  saving.value = true
  try {
    await sdkApi.put(`/admin/node-configs/${current.value.id}`, { config: rawConfig.value })
    ElMessage.success('SDK 配置已保存')
    await loadConfig()
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '保存 SDK 配置失败'))
  } finally {
    saving.value = false
  }
}
watch(() => props.gameId, () => { void loadNodes() }, { immediate: true })
</script>

<template>
  <main class="channel-page">
    <div class="page-heading">
      <div>
        <div class="eyebrow">SDK CONFIGURATION</div>
        <h1>SDK 节点配置</h1>
        <p class="muted">管理游戏密钥、适配器和大区业务接口地址，不再写入配置中心的基础节点 JSON。</p>
      </div>
      <el-button :loading="loading" @click="loadConfig" :disabled="!currentID">刷新</el-button>
    </div>
    <section class="channel-detail-panel">
      <el-empty v-if="!gameId" description="请先选择游戏" />
      <template v-else>
        <el-form label-position="top" v-loading="loading">
          <el-form-item label="配置节点">
            <el-select v-model="currentID" filterable placeholder="选择游戏或大区" style="width:100%"
                       :disabled="saving" @change="loadConfig">
              <el-option v-for="item in options" :key="item.id"
                         :label="`${item.nodeType === 'game' ? '游戏' : '大区'} / ${item.name}`"
                         :value="item.id" />
            </el-select>
          </el-form-item>
          <BusinessConfigEditor v-if="current" v-model="rawConfig" :node-type="current.nodeType" />
          <div class="panel-actions" style="justify-content: flex-end; margin-top: 20px">
            <el-button type="primary" :disabled="!current || loading" :loading="saving" @click="save">保存 SDK 配置</el-button>
          </div>
        </el-form>
      </template>
    </section>
  </main>
</template>
