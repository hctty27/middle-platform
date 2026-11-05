<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { sdkApi, apiErrorMessage } from '../../api/http'

interface SDKService {
  id: number
  code: string
  name: string
  description: string
  enabled: boolean
}

const items = ref<SDKService[]>([])
const loading = ref(false)
const savingId = ref<number | null>(null)

async function load() {
  loading.value = true
  try {
    const { data } = await api.get<SDKService[]>('/admin/services')
    items.value = data
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '加载服务目录失败'))
  } finally {
    loading.value = false
  }
}

async function updateService(service: SDKService, enabled: boolean | string | number) {
  if (savingId.value !== null) return
  const next = Boolean(enabled)
  savingId.value = service.id
  try {
    await sdkApi.put('/admin/services/' + service.id, { enabled: next })
    service.enabled = next
    ElMessage.success('全局服务开关已保存')
  } catch (error) {
    service.enabled = !next
    ElMessage.error(apiErrorMessage(error, '更新全局服务开关失败'))
  } finally {
    savingId.value = null
  }
}

onMounted(() => void load())
</script>

<template>
  <main class="channel-page">
    <div class="page-heading">
      <div>
        <div class="eyebrow">SDK SERVICES</div>
        <h1>服务管理</h1>
        <p class="muted">全局开关控制服务是否可用；每个渠道包还需独立配置环境开关。</p>
      </div>
      <el-button @click="load">刷新</el-button>
    </div>
    <section class="channel-detail-panel">
      <el-table v-loading="loading" :data="items" stripe>
        <el-table-column prop="name" label="服务名称" min-width="140" />
        <el-table-column prop="code" label="服务编码" min-width="170" />
        <el-table-column prop="description" label="说明" min-width="240" />
        <el-table-column label="全局状态" width="140" align="center">
          <template #default="{ row }">
            <el-switch :model-value="row.enabled" :loading="savingId === row.id"
                       :disabled="savingId !== null" @change="(value: boolean | string | number) => updateService(row, value)" />
          </template>
        </el-table-column>
      </el-table>
      <p class="muted">服务目录包括事件上报、用户核验、角色查询、区服查询、封解禁和订阅消息推送；渠道环境开关需在渠道应用页面单独开启。</p>
    </section>
  </main>
</template>
