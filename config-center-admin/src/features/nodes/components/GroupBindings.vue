<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '../../../api/http'

interface GroupApp {
  channelAppId: number
  channelAppName: string
  channelAppCode: string
  serverGroupId: number
  status: number
  config: string
}
const props = defineProps<{ gameId: number; groupId: number }>()
const apps = ref<GroupApp[]>([])
const loading = ref(false)
let sequence = 0
async function reload() {
  const request = ++sequence
  loading.value = true
  try {
    const { data } = await api.get<GroupApp[]>(`/admin/server-groups/${props.groupId}/channel-apps`)
    if (request === sequence) apps.value = data
  } catch { if (request === sequence) ElMessage.error('查询关联的渠道应用失败') }
  finally { if (request === sequence) loading.value = false }
}
watch(() => [props.groupId, props.gameId], () => { void reload() }, { immediate: true })
</script>

<template>
  <div class="group-bindings">
    <div class="section-heading">
      <div><h3>关联的渠道应用</h3>
        <p class="muted">绑定关系由渠道应用的三个环境字段统一管理。请在「渠道应用」页面修改。</p></div>
      <el-button @click="reload">刷新</el-button>
    </div>
    <el-table v-loading="loading" :data="apps" stripe empty-text="暂无关联的渠道应用">
      <el-table-column label="渠道应用" min-width="160">
        <template #default="{ row }"><div class="table-main">{{ row.channelAppName }}</div>
          <div class="muted">{{ row.channelAppCode }}</div></template>
      </el-table-column>
      <el-table-column label="状态" width="100">
        <template #default="{ row }"><el-tag :type="row.status ? 'success' : 'info'" size="small">
          {{ row.status ? '已启用' : '已停用' }}</el-tag></template>
      </el-table-column>
      <el-table-column label="共享 JSON" min-width="200" show-overflow-tooltip>
        <template #default="{ row }"><code>{{ row.config }}</code></template>
      </el-table-column>
    </el-table>
  </div>
</template>
