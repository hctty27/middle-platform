<script setup lang="ts">
// 回调订阅者管理页：展示投递进度和失败信息，并支持注册新的接入服务。
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { createSubscriber, listSubscribers } from '../../api/subscriberApi'
import type { Subscriber } from '../../api/subscriberApi'

const subscribers = ref<Subscriber[]>([])
const loadingSubscribers = ref(false)
const showSubscriber = ref(false)
const creatingSubscriber = ref(false)
const newSubscriber = reactive({ code: '', callbackUrl: '', secret: '' })

// 每次查询均更新加载状态，失败时保留可见的错误提示。
async function loadSubscribers() {
  loadingSubscribers.value = true
  try {
    subscribers.value = (await listSubscribers()).data
  } catch { ElMessage.error('读取接入平台失败') }
  finally { loadingSubscribers.value = false }
}

// Webhook 密钥仅在创建时提交，不在订阅者列表中回显。
async function addSubscriber() {
  if (!newSubscriber.code.trim() || !newSubscriber.callbackUrl.trim() || !newSubscriber.secret.trim()) {
    ElMessage.warning('请填写接入方编码、回调地址和签名密钥')
    return
  }
  creatingSubscriber.value = true
  try {
    await createSubscriber(newSubscriber)
    showSubscriber.value = false
    Object.assign(newSubscriber, { code: '', callbackUrl: '', secret: '' })
    await loadSubscribers()
    ElMessage.success('接入平台创建成功')
  } catch { ElMessage.error('创建失败：请确认回调地址在服务端白名单中') }
  finally { creatingSubscriber.value = false }
}

onMounted(() => void loadSubscribers())
</script>

<template>
      <main class="content-panel">
        <div class="panel-title">
          <div><h2>接入平台与同步状态</h2>
            <p class="muted">接入方接收 Webhook 版本通知，再主动拉取变更节点。</p>
          </div>
          <div class="panel-actions">
            <el-button @click="loadSubscribers">刷新</el-button>
            <el-button type="primary" @click="showSubscriber = true">新增接入平台</el-button>
          </div>
        </div>
        <el-table v-loading="loadingSubscribers" :data="subscribers" stripe>
          <el-table-column prop="code" label="接入方" min-width="140" />
          <el-table-column prop="callbackUrl" label="回调 URL" min-width="260" show-overflow-tooltip />
          <el-table-column prop="acknowledgedRevision" label="已确认版本" width="120" />
          <el-table-column prop="failures" label="连续失败" width="95" />
          <el-table-column label="状态" width="105">
            <template #default="{ row }">
              <el-tag :type="!row.enabled ? 'info' : row.failures ? 'danger' : 'success'">
                {{ !row.enabled ? '已停用' : row.failures ? '异常' : '正常' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="lastError" label="最近错误" min-width="160" show-overflow-tooltip />
        </el-table>
        <el-dialog v-model="showSubscriber" title="新增接入平台" width="530px">
          <el-form label-position="top">
            <el-form-item label="唯一编码"><el-input v-model="newSubscriber.code" /></el-form-item>
            <el-form-item label="Webhook 回调地址"><el-input v-model="newSubscriber.callbackUrl" /></el-form-item>
            <el-form-item label="共享签名密钥">
              <el-input v-model="newSubscriber.secret" type="password" show-password />
            </el-form-item>
            <p class="muted">回调域名须在配置中心服务端允许列表中。</p>
          </el-form>
          <template #footer>
            <el-button @click="showSubscriber = false">取消</el-button>
            <el-button type="primary" :loading="creatingSubscriber" @click="addSubscriber">创建</el-button>
          </template>
        </el-dialog>
      </main>
</template>
