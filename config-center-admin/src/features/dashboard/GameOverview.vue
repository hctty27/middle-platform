<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { nodeApi } from '../../api/nodeApi'
import { regionEnvLabels } from '../nodes/types'
import type { NodeSummary, RegionEnvType } from '../nodes/types'

const props = defineProps<{ gameId: number | null }>()
const emit = defineEmits<{ navigate: [page: 'nodes' | 'channel_apps'] }>()
const regions = ref<NodeSummary[]>([])
const total = ref(0)
const busy = ref(false)
const filter = ref<RegionEnvType | ''>('')
const visible = computed(() => filter.value ? regions.value.filter(x => x.envType === filter.value) : regions.value)
let requestId = 0

async function load() {
  const id = ++requestId
  if (!props.gameId) { regions.value = []; total.value = 0; return }
  busy.value = true
  try {
    const [tree, page] = await Promise.all([
      nodeApi.getTreeChildren(props.gameId),
      nodeApi.getChildren({ parentId: props.gameId, page: 1, size: 1, keyword: '' })
    ])
    if (id === requestId) { regions.value = tree.data; total.value = page.data.total }
  } catch {
    if (id === requestId) ElMessage.error('加载大区概览失败')
  } finally {
    if (id === requestId) busy.value = false
  }
}
watch(() => props.gameId, () => void load(), { immediate: true })
</script>

<template>
  <main class="dashboard-page">
    <div class="page-heading">
      <div><div class="eyebrow">WORKSPACE OVERVIEW</div><h1>游戏配置概览</h1>
        <p class="muted">从当前游戏进入大区、区服组和渠道应用管理，减少重复切换。</p></div>
      <el-button type="primary" @click="emit('navigate', 'nodes')">进入配置管理</el-button>
    </div>
    <div class="metric-grid">
      <div class="metric-card"><span>大区总数</span><strong>{{ total }}</strong><small>当前游戏</small></div>
      <div class="metric-card"><span>正式服大区</span><strong>{{ regions.filter(x => x.envType === 'prod').length }}</strong><small>Production</small></div>
      <div class="metric-card"><span>提审服大区</span><strong>{{ regions.filter(x => x.envType === 'review').length }}</strong><small>Review</small></div>
      <div class="metric-card"><span>测试服大区</span><strong>{{ regions.filter(x => x.envType === 'test').length }}</strong><small>Test</small></div>
    </div>
    <section class="overview-panel" v-loading="busy">
      <div class="section-heading"><div><h2>大区列表</h2><p class="muted">快速定位当前游戏下的不同环境</p></div>
        <el-select v-model="filter" clearable placeholder="全部环境" style="width: 160px">
          <el-option v-for="(label, key) in regionEnvLabels" :key="key" :label="label" :value="key" />
        </el-select>
      </div>
      <el-empty v-if="!gameId" description="请先在顶部选择游戏" />
      <el-empty v-else-if="!busy && !visible.length" description="暂无匹配的大区" />
      <div v-else class="region-grid">
        <button v-for="region in visible" :key="region.id" class="region-tile" @click="emit('navigate', 'nodes')">
          <div class="region-tile-top"><strong>{{ region.name }}</strong>
            <el-tag size="small" :type="region.envType === 'prod' ? 'success' : region.envType === 'test' ? 'warning' : 'primary'">
              {{ region.envType ? regionEnvLabels[region.envType] : '未分类' }}</el-tag></div>
          <span>Code: {{ region.code }}</span><span>查看区服组 →</span>
        </button>
      </div>
    </section>
    <div class="quick-links">
      <button @click="emit('navigate', 'nodes')"><strong>配置管理</strong><span>游戏 / 大区 / 区服组 →</span></button>
      <button @click="emit('navigate', 'channel_apps')"><strong>渠道应用</strong><span>公共配置 / 区服组绑定 →</span></button>
    </div>
  </main>
</template>
