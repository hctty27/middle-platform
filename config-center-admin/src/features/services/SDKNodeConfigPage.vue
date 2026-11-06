<script setup lang="ts">
// SDK 节点配置与基础配置中心分离，游戏和大区的业务字段独立读写。
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { confirmDiscardChanges, registerUnsavedChecker } from '../../app/unsavedChanges'
import { sdkApi, apiErrorMessage } from '../../api/http'
import { nodeApi } from '../../api/nodeApi'
import BusinessConfigEditor from '../nodes/components/BusinessConfigEditor.vue'
import { validateBusinessConfig } from '../nodes/businessConfig'
import type { BusinessConfigType } from '../nodes/businessConfig'

interface Option { id: number; name: string; nodeType: BusinessConfigType }
interface NodeSDKConfig { nodeId: number; nodeType: BusinessConfigType; config: string }
const props = defineProps<{ gameId: number | null; focusNodeId: number | null }>()
const options = ref<Option[]>([])
const currentID = ref<number | null>(null)
const rawConfig = ref('{}')
const savedConfig = ref('{}')
const loading = ref(false)
const saving = ref(false)
// 使用递增请求序号，避免快速切换游戏或节点时旧响应覆盖新数据。
let sequence = 0
const current = computed(() => options.value.find(item => item.id === currentID.value) ?? null)
const unregisterUnsaved = registerUnsavedChecker(() => !!currentID.value && rawConfig.value !== savedConfig.value)
onBeforeUnmount(unregisterUnsaved)

// loadNodes 按当前游戏加载游戏和大区节点，并默认选中游戏配置。
async function loadNodes() {
  const request = ++sequence
  options.value = []
  currentID.value = null
  rawConfig.value = '{}'
  savedConfig.value = '{}'
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
    currentID.value = options.value.some(item => item.id === props.focusNodeId)
      ? props.focusNodeId : props.gameId
    loading.value = false
    await loadConfig()
  } catch (error) {
    if (request === sequence) ElMessage.error(apiErrorMessage(error, '加载 SDK 节点失败'))
  } finally {
    if (request === sequence) loading.value = false
  }
}
// loadConfig 仅接受最新请求的响应，避免展示已切换节点的旧配置。
async function loadConfig() {
  const id = currentID.value
  const request = ++sequence
  if (!id) { rawConfig.value = '{}'; return }
  loading.value = true
  try {
    const { data } = await sdkApi.get<NodeSDKConfig>(`/admin/node-configs/${id}`)
    if (request !== sequence) return
    rawConfig.value = JSON.stringify(JSON.parse(data.config), null, 2)
    savedConfig.value = rawConfig.value
  } catch (error) {
    if (request === sequence) ElMessage.error(apiErrorMessage(error, '读取 SDK 配置失败'))
  } finally {
    if (request === sequence) loading.value = false
  }
}
// save 在写入前校验业务字段，保存后重新读取服务端配置。
async function save() {
  if (!current.value) return
  try { validateBusinessConfig(current.value.nodeType, rawConfig.value) }
  catch (error) { ElMessage.warning(error instanceof Error ? error.message : 'SDK 配置不合法'); return }
  saving.value = true
  try {
    await sdkApi.put(`/admin/node-configs/${current.value.id}`, { config: rawConfig.value })
    ElMessage.success('SDK 配置已保存')
    savedConfig.value = rawConfig.value
    await loadConfig()
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '保存 SDK 配置失败'))
  } finally {
    saving.value = false
  }
}
async function selectNode(nextId: number) {
  if (nextId === currentID.value || !(await confirmDiscardChanges())) return
  currentID.value = nextId
  await loadConfig()
}

async function refreshConfig() {
  if (!(await confirmDiscardChanges())) return
  await loadConfig()
}

watch(() => [props.gameId, props.focusNodeId], () => { void loadNodes() }, { immediate: true })
</script>

<template>
  <main class="channel-page">
    <div class="page-heading">
      <div>
        <div class="eyebrow">SDK CONFIGURATION</div>
        <h1>SDK 节点配置</h1>
        <p class="muted">管理游戏密钥、适配器和大区业务接口地址，不再写入配置中心的基础节点 JSON。</p>
      </div>
      <el-button :loading="loading" @click="refreshConfig" :disabled="!currentID">刷新</el-button>
    </div>
    <section class="channel-detail-panel">
      <el-empty v-if="!gameId" description="请先选择游戏" />
      <template v-else>
        <el-form label-position="top" v-loading="loading">
          <el-form-item label="配置节点">
            <el-select :model-value="currentID" filterable placeholder="选择游戏或大区" style="width:100%"
                       :disabled="saving || loading" @change="selectNode">
              <el-option v-for="item in options" :key="item.id"
                         :label="`${item.nodeType === 'game' ? '游戏' : '大区'} / ${item.name}`"
                         :value="item.id" />
            </el-select>
          </el-form-item>
          <BusinessConfigEditor v-if="current" v-model="rawConfig" :node-type="current.nodeType" />
          <el-tag v-if="rawConfig !== savedConfig" type="warning" effect="plain">有未保存的修改</el-tag>
          <div class="panel-actions" style="justify-content: flex-end; margin-top: 20px">
            <el-button type="primary" :disabled="!current || loading" :loading="saving" @click="save">保存 SDK 配置</el-button>
          </div>
        </el-form>
      </template>
    </section>
  </main>
</template>
