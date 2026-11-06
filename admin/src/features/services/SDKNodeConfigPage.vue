<script setup lang="ts">
// SDK 节点按配置中心的实际层级导航；区服组暂不开放独立 SDK 配置编辑。
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage, ElTree } from 'element-plus'
import type { LoadFunction } from 'element-plus'
import { confirmDiscardChanges, registerUnsavedChecker } from '../../app/unsavedChanges'
import { sdkApi, apiErrorMessage } from '../../api/http'
import { nodeApi } from '../../api/nodeApi'
import type { NodeMatch, NodeRecord, NodeSummary, NodeType } from '../nodes/types'
import { regionEnvLabels } from '../nodes/types'
import ConfigSchemaEditor from '../../components/ConfigSchemaEditor.vue'
import { getConfigSchema, validateConfigAgainstSchema } from '../../api/configSchemaApi'

interface NodeSDKConfig { nodeId: number; nodeType: 'game' | 'region'; config: string }
interface TreePathNode { data: NodeSummary; parent: TreePathNode | null; level: number }

const props = defineProps<{ gameId: number | null; focusNodeId: number | null }>()
const labels: Record<NodeType, string> = {
  game: '游戏', region: '大区', server_group: '区服组', channel_app: '渠道应用'
}
const treeProps = { label: 'name', isLeaf: 'leaf' }
const treeRef = ref<InstanceType<typeof ElTree> | null>(null)
const treeVersion = ref(0)
const expandedKeys = ref<number[]>([])
const gameNode = ref<NodeSummary | null>(null)
const currentNode = ref<NodeSummary | null>(null)
const currentPath = ref<NodeSummary[]>([])
const currentID = computed(() => currentNode.value?.id ?? null)
const editable = computed(() => currentNode.value?.nodeType === 'game' || currentNode.value?.nodeType === 'region')
const rawConfig = ref('{}')
const savedConfig = ref('{}')
const nodesLoading = ref(false)
const configLoading = ref(false)
const saving = ref(false)
const searchWord = ref('')
const searchMatches = ref<NodeMatch[]>([])
const searchLoading = ref(false)
let nodesSequence = 0
let configSequence = 0

const unregisterUnsaved = registerUnsavedChecker(
  () => editable.value && rawConfig.value !== savedConfig.value,
  () => { rawConfig.value = savedConfig.value }
)
onBeforeUnmount(unregisterUnsaved)

function toSummary(node: NodeRecord): NodeSummary {
  return {
    id: node.id, parentId: node.parentId, nodeType: node.nodeType,
    code: node.code, name: node.name, status: node.status,
    envType: node.envType, marketType: node.marketType,
    leaf: node.nodeType === 'server_group' || node.nodeType === 'channel_app'
  }
}

function isEditableNode(node: NodeSummary): node is NodeSummary & { nodeType: 'game' | 'region' } {
  return node.nodeType === 'game' || node.nodeType === 'region'
}

// 树根只放当前游戏，展开时按需查询直属子节点，不批量读取所有区服组。
const loadTree: LoadFunction = async (treeNode, resolve, reject) => {
  if (treeNode.level === 0) {
    resolve(gameNode.value ? [gameNode.value] : [])
    return
  }
  const node = treeNode.data as NodeSummary
  if (!isEditableNode(node)) {
    resolve([])
    return
  }
  try {
    const response = await nodeApi.getTreeChildren(node.id)
    const expectedType = node.nodeType === 'game' ? 'region' : 'server_group'
    resolve(response.data.filter(child => child.nodeType === expectedType))
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '加载 SDK 节点目录失败'))
    reject()
  }
}

function onExpand(node: NodeSummary) {
  if (!expandedKeys.value.includes(node.id)) expandedKeys.value = [...expandedKeys.value, node.id]
}

function onCollapse(node: NodeSummary) {
  expandedKeys.value = expandedKeys.value.filter(id => id !== node.id)
}

async function loadConfig() {
  const node = currentNode.value
  const request = ++configSequence
  rawConfig.value = '{}'
  savedConfig.value = '{}'
  if (!node || !isEditableNode(node)) {
    configLoading.value = false
    return
  }
  configLoading.value = true
  try {
    const { data } = await sdkApi.get<NodeSDKConfig>('/admin/node-configs/' + node.id)
    if (request !== configSequence) return
    rawConfig.value = JSON.stringify(JSON.parse(data.config), null, 2)
    savedConfig.value = rawConfig.value
  } catch (error) {
    if (request === configSequence) ElMessage.error(apiErrorMessage(error, '读取 SDK 配置失败'))
  } finally {
    if (request === configSequence) configLoading.value = false
  }
}

async function loadNodes(preferredID: number | null) {
  const request = ++nodesSequence
  ++configSequence
  gameNode.value = null
  currentNode.value = null
  currentPath.value = []
  rawConfig.value = '{}'
  savedConfig.value = '{}'
  searchWord.value = ''
  if (!props.gameId) return
  nodesLoading.value = true
  try {
    const { data } = await nodeApi.getNode(props.gameId)
    if (request !== nodesSequence) return
    const game = toSummary(data)
    let selected: NodeSummary = game
    let path: NodeSummary[] = [game]
    if (preferredID && preferredID !== game.id) {
      try {
        const result = await nodeApi.getPath(preferredID)
        if (request !== nodesSequence) return
        if (result.data.path[0]?.id === game.id && result.data.node.nodeType !== 'channel_app') {
          selected = result.data.node
          path = result.data.path
        }
      } catch (error) {
        if (request === nodesSequence) ElMessage.warning(apiErrorMessage(error, '节点定位失败，已切换至游戏配置'))
      }
    }
    if (request !== nodesSequence) return
    gameNode.value = game
    currentNode.value = selected
    currentPath.value = path
    expandedKeys.value = [...new Set([game.id, ...path.slice(0, -1).map(item => item.id)])]
    treeVersion.value++
    await loadConfig()
  } catch (error) {
    if (request === nodesSequence) ElMessage.error(apiErrorMessage(error, '加载 SDK 节点失败'))
  } finally {
    if (request === nodesSequence) nodesLoading.value = false
  }
}

async function selectNode(node: NodeSummary, path: NodeSummary[]) {
  if (nodesLoading.value || configLoading.value || saving.value) {
    treeRef.value?.setCurrentKey(currentID.value ?? undefined)
    return
  }
  if (node.id === currentID.value) {
    searchWord.value = ''
    return
  }
  if (!(await confirmDiscardChanges())) {
    treeRef.value?.setCurrentKey(currentID.value ?? undefined)
    return
  }
  currentNode.value = node
  currentPath.value = path
  searchWord.value = ''
  expandedKeys.value = [...new Set([props.gameId ?? node.id, ...path.slice(0, -1).map(item => item.id)])]
  if (treeRef.value?.getNode(node.id)) treeRef.value.setCurrentKey(node.id)
  else treeVersion.value++
  await loadConfig()
}

function onTreeClick(node: NodeSummary, rawTreeNode: unknown) {
  const path: NodeSummary[] = []
  let item = rawTreeNode as TreePathNode | null
  while (item && item.level > 0) {
    path.unshift(item.data)
    item = item.parent
  }
  void selectNode(node, path.length ? path : [node])
}

watch(searchWord, (word, _old, onCleanup) => {
  searchMatches.value = []
  const keyword = word.trim()
  if (!keyword) {
    searchLoading.value = false
    return
  }
  searchLoading.value = true
  const controller = new AbortController()
  const timer = setTimeout(async () => {
    try {
      const result = await nodeApi.search(keyword, controller.signal)
      if (!controller.signal.aborted) {
        searchMatches.value = result.data.filter(match =>
          match.node.nodeType !== 'channel_app' && match.path[0]?.id === props.gameId
        )
      }
    } catch (error) {
      if (!controller.signal.aborted) ElMessage.error(apiErrorMessage(error, '搜索 SDK 节点失败'))
    } finally {
      if (!controller.signal.aborted) searchLoading.value = false
    }
  }, 250)
  onCleanup(() => { clearTimeout(timer); controller.abort() })
})

async function save() {
  const node = currentNode.value
  if (!node || !isEditableNode(node) || saving.value || configLoading.value) return
  try {
    const definition = await getConfigSchema('sdk', node.nodeType)
    validateConfigAgainstSchema(rawConfig.value, definition.schema)
  } catch (error) {
    ElMessage.warning(error instanceof Error ? error.message : 'SDK 配置不合法')
    return
  }
  saving.value = true
  try {
    await sdkApi.put('/admin/node-configs/' + node.id, { config: rawConfig.value })
    ElMessage.success('SDK 配置已保存')
    savedConfig.value = rawConfig.value
    await loadConfig()
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '保存 SDK 配置失败'))
  } finally {
    saving.value = false
  }
}

async function refresh() {
  if (saving.value || !(await confirmDiscardChanges())) return
  await loadNodes(currentID.value)
}

watch(() => [props.gameId, props.focusNodeId], () => {
  void loadNodes(props.focusNodeId ?? props.gameId)
}, { immediate: true })
</script>

<template>
  <main class="channel-page">
    <div class="page-heading">
      <div>
        <div class="eyebrow">SDK CONFIGURATION</div>
        <h1>SDK 节点配置</h1>
        <p class="muted">按游戏、大区和区服组定位节点；游戏与大区的 SDK 配置独立保存。</p>
      </div>
      <el-button :loading="nodesLoading || configLoading" :disabled="saving || !gameId" @click="refresh">刷新</el-button>
    </div>
    <el-empty v-if="!gameId" description="请先在顶部选择游戏" />
    <div v-else class="sdk-config-layout">
      <section class="sdk-config-tree">
        <div class="sdk-config-tree-heading">
          <strong>节点目录</strong>
          <span class="muted">当前游戏</span>
        </div>
        <el-input v-model="searchWord" clearable placeholder="搜索节点名称或 Code" autocomplete="off" />
        <div v-if="searchWord.trim()" class="sdk-config-tree-scroll" v-loading="searchLoading">
          <p class="muted">搜索范围：当前游戏的游戏、大区及区服组</p>
          <el-empty v-if="!searchLoading && !searchMatches.length" description="没有匹配节点" :image-size="70" />
          <button v-for="match in searchMatches" :key="match.node.id" type="button"
                  class="sdk-config-search-result" @click="selectNode(match.node, match.path)">
            <strong>{{ match.node.name }}</strong>
            <small>{{ match.path.map(item => item.name).join(' / ') }}</small>
            <small>{{ labels[match.node.nodeType] }} · {{ match.node.code }}</small>
          </button>
        </div>
        <div v-show="!searchWord.trim()" class="sdk-config-tree-scroll" v-loading="nodesLoading">
          <el-tree v-if="gameNode" :key="treeVersion" ref="treeRef" node-key="id" lazy
                   :load="loadTree" :props="treeProps" :default-expanded-keys="expandedKeys"
                   :current-node-key="currentID" highlight-current :expand-on-click-node="false"
                   @node-click="onTreeClick" @node-expand="onExpand" @node-collapse="onCollapse">
            <template #default="{ data }">
              <span class="sdk-config-node-label" :title="data.name + ' / ' + data.code">
                <span class="sdk-config-node-name">{{ data.name }}</span>
                <el-tag size="small" effect="plain" type="info">{{ labels[data.nodeType as NodeType] }}</el-tag>
                <el-tag v-if="data.nodeType === 'region' && data.envType" size="small" effect="plain">
                  {{ regionEnvLabels[data.envType as keyof typeof regionEnvLabels] }}
                </el-tag>
              </span>
            </template>
          </el-tree>
        </div>
        <div class="sdk-config-tree-tip">区服组仅用于定位，暂不支持独立 SDK 配置。</div>
      </section>

      <section class="channel-detail-panel sdk-config-detail" v-loading="nodesLoading">
        <el-empty v-if="!currentNode" description="请从左侧选择一个节点" />
        <template v-else>
          <el-breadcrumb separator="/" class="sdk-config-breadcrumb">
            <el-breadcrumb-item v-for="(item, index) in currentPath" :key="item.id">
              <button type="button" class="sdk-config-crumb"
                      @click="selectNode(item, currentPath.slice(0, index + 1))">{{ item.name }}</button>
            </el-breadcrumb-item>
          </el-breadcrumb>
          <div class="sdk-config-detail-heading">
            <div>
              <h2>{{ currentNode.name }}</h2>
              <p class="muted">{{ labels[currentNode.nodeType] }} · ID {{ currentNode.id }} · Code {{ currentNode.code }}</p>
            </div>
            <el-tag :type="currentNode.status === 1 ? 'success' : 'info'" effect="plain">
              {{ currentNode.status === 1 ? '启用' : '停用' }}
            </el-tag>
          </div>
          <el-alert v-if="!editable" title="当前区服组暂不支持独立 SDK 配置"
                    description="这里只展示节点信息。区服组基础配置请前往「节点与基础配置」维护。"
                    type="info" :closable="false" show-icon />
          <el-form v-else label-position="top" class="admin-config-form" autocomplete="off" v-loading="configLoading">
            <ConfigSchemaEditor v-model="rawConfig" :node-type="currentNode.nodeType" scope="sdk" />
            <el-tag v-if="rawConfig !== savedConfig" type="warning" effect="plain">有未保存的修改</el-tag>
            <div class="sdk-config-actions">
              <el-button type="primary" :loading="saving" :disabled="configLoading || nodesLoading" @click="save">保存 SDK 配置</el-button>
            </div>
          </el-form>
        </template>
      </section>
    </div>
  </main>
</template>

<style scoped>
.sdk-config-layout {
  display: grid;
  grid-template-columns: minmax(260px, 300px) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}
.sdk-config-tree {
  display: flex;
  flex-direction: column;
  min-width: 0;
  max-height: calc(100vh - 215px);
  min-height: 360px;
  padding: 18px 14px;
  border: 1px solid #e7ecf4;
  border-radius: 12px;
  background: #fff;
}
.sdk-config-tree-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 14px;
}
.sdk-config-tree-scroll {
  flex: 1;
  min-height: 0;
  overflow: auto;
  margin-top: 14px;
}
.sdk-config-node-label {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  width: 100%;
  padding-right: 6px;
}
.sdk-config-node-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
}
.sdk-config-tree :deep(.el-tree-node__content) { height: 38px; border-radius: 6px; }
.sdk-config-tree :deep(.el-tag) { flex-shrink: 0; }
.sdk-config-tree-tip {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid #edf1f6;
  font-size: 12px;
  color: #8492a5;
}
.sdk-config-search-result {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 100%;
  margin: 5px 0;
  padding: 10px;
  border: 1px solid #e7ecf4;
  border-radius: 8px;
  background: #fff;
  text-align: left;
  cursor: pointer;
  color: #24334c;
}
.sdk-config-search-result:hover { background: #f4f7ff; border-color: #c7d8ff; }
.sdk-config-search-result small { color: #8795a8; overflow-wrap: anywhere; }
.sdk-config-detail { min-height: 380px; }
.sdk-config-breadcrumb { margin-bottom: 18px; }
.sdk-config-crumb { padding: 0; border: 0; background: transparent; cursor: pointer; color: #647d9d; }
.sdk-config-crumb:hover { color: #386df6; }
.sdk-config-detail-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 22px;
  padding-bottom: 16px;
  border-bottom: 1px solid #edf1f6;
}
.sdk-config-detail-heading h2 { margin: 0 0 8px; font-size: 20px; }
.sdk-config-detail-heading p { margin: 0; overflow-wrap: anywhere; }
.sdk-config-actions { display: flex; justify-content: flex-end; margin-top: 20px; }
@media (max-width: 900px) {
  .sdk-config-layout { grid-template-columns: minmax(0, 1fr); }
  .sdk-config-tree { max-height: 340px; min-height: 240px; }
}
</style>
