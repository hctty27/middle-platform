<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import axios from 'axios'
import { ElMessage } from 'element-plus'
import ConfigWorkspace from './ConfigWorkspace.vue'

interface Subscriber {
  id: number
  code: string
  callbackUrl: string
  enabled: boolean
  acknowledgedRevision: number
  failures: number
  lastError: string | null
}

const api = axios.create({ baseURL: '/api/v1', timeout: 10000 })
const navigation = ref<'nodes' | 'subscribers' | 'about'>('nodes')
const sidebarCollapsed = ref(false)
const revision = ref(0)
const epoch = ref('')
const subscribers = ref<Subscriber[]>([])
const loadingSubscribers = ref(false)
const showSubscriber = ref(false)
const creatingSubscriber = ref(false)
const newSubscriber = reactive({ code: '', callbackUrl: '', secret: '' })

async function refreshRevision() {
  try {
    const result = await api.get<{ epoch: string; revision: number }>('/sync/revision')
    revision.value = result.data.revision
    epoch.value = result.data.epoch
  } catch { ElMessage.error('读取配置版本失败') }
}

async function loadSubscribers() {
  loadingSubscribers.value = true
  try {
    subscribers.value = (await api.get<Subscriber[]>('/admin/subscribers')).data
  } catch { ElMessage.error('读取接入平台失败') }
  finally { loadingSubscribers.value = false }
}

async function addSubscriber() {
  if (!newSubscriber.code.trim() || !newSubscriber.callbackUrl.trim() || !newSubscriber.secret.trim()) {
    ElMessage.warning('请填写接入方编码、回调地址和签名密钥')
    return
  }
  creatingSubscriber.value = true
  try {
    await api.post('/admin/subscribers', newSubscriber)
    showSubscriber.value = false
    Object.assign(newSubscriber, { code: '', callbackUrl: '', secret: '' })
    await loadSubscribers()
    ElMessage.success('接入平台创建成功')
  } catch { ElMessage.error('创建失败：请确认回调地址在服务端白名单中') }
  finally { creatingSubscriber.value = false }
}

function navigate(tab: 'nodes' | 'subscribers' | 'about') {
  navigation.value = tab
  if (tab === 'subscribers') void loadSubscribers()
}

onMounted(() => void refreshRevision())
</script>

<template>
  <div class="shell">
    <aside class="sidebar" :class="{ collapsed: sidebarCollapsed }">
      <div class="brand">
        <div class="brand-mark">M</div>
        <div v-if="!sidebarCollapsed">
          <strong>研发中台</strong><small>CONFIG CENTER</small>
        </div>
      </div>
      <div v-if="!sidebarCollapsed" class="group-title">工作空间</div>
      <button :title="'配置管理'" :class="{ active: navigation === 'nodes' }" @click="navigate('nodes')">
        <span class="nav-symbol">▦</span><span v-if="!sidebarCollapsed">配置管理</span>
      </button>
      <button title="接入与同步" :class="{ active: navigation === 'subscribers' }" @click="navigate('subscribers')">
        <span class="nav-symbol">↗</span><span v-if="!sidebarCollapsed">接入与同步</span>
      </button>
      <button title="系统说明" :class="{ active: navigation === 'about' }" @click="navigate('about')">
        <span class="nav-symbol">ⓘ</span><span v-if="!sidebarCollapsed">系统说明</span>
      </button>
      <div class="sidebar-bottom">
        <button class="collapse-button" @click="sidebarCollapsed = !sidebarCollapsed">
          {{ sidebarCollapsed ? '»' : '« 收起导航' }}
        </button>
        <small v-if="!sidebarCollapsed">无登录模式 · 内网使用</small>
      </div>
    </aside>

    <div class="main">
      <header class="topbar">
        <div class="topbar-heading">
          <strong>{{ navigation === 'nodes' ? '配置管理' : navigation === 'subscribers' ? '接入与同步' : '系统说明' }}</strong>
          <span class="subline">研发中台 / 配置中心</span>
        </div>
        <div class="top-right">
          <el-tag type="success" effect="plain">Revision {{ revision }}</el-tag>
          <el-button @click="refreshRevision">刷新版本</el-button>
        </div>
      </header>
      <div v-show="navigation === 'nodes'" class="workspace-shell">
        <ConfigWorkspace @changed="refreshRevision" />
      </div>
      <main v-if="navigation === 'subscribers'" class="content-panel">
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
      <main v-else-if="navigation === 'about'" class="content-panel">
        <h2>系统说明</h2>
        <p>当前权威数据集：{{ epoch }}</p>
        <p>当前配置版本：{{ revision }}</p>
        <p>游戏 → 大区 → 区服组 → 渠道 → SDK 应用，配置只在对应节点保存。</p>
        <el-alert title="管理后台没有登录和账号权限，必须通过内网或受控网关限制访问。"
                  type="warning" show-icon :closable="false" />
      </main>
    </div>
  </div>
</template>
