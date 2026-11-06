<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import axios from 'axios'
import { api, apiErrorMessage } from './api/http'
import { adminAuthenticated, clearAdminSecret, setAdminSecret, unlockAdmin } from './api/adminAuth'
import { getRevision } from './api/syncApi'
import { confirmDiscardChanges, preventUnloadWithUnsavedChanges } from './app/unsavedChanges'
import { currentGameId, focusNodeId, games, gamesLoading, loadGames } from './app/gameContext'
import { gameMarketLabels } from './features/nodes/types'
import ConfigWorkspace from './ConfigWorkspace.vue'
import GameOverview from './features/dashboard/GameOverview.vue'
import ChannelAppPage from './features/nodes/ChannelAppPage.vue'
import SDKServicePage from './features/services/SDKServicePage.vue'
import SDKNodeConfigPage from './features/services/SDKNodeConfigPage.vue'
import SubscriberPage from './features/subscribers/SubscriberPage.vue'
import RechargeSimulatorPage from './features/recharge/RechargeSimulatorPage.vue'

type Page = 'overview' | 'nodes' | 'channel_apps' | 'sdk_node_config' | 'sdk_services' | 'subscribers' | 'recharge' | 'about'
const pageSet: Page[] = ['overview', 'nodes', 'channel_apps', 'sdk_node_config', 'sdk_services', 'subscribers', 'recharge', 'about']
const navigation = ref<Page>('overview')
const sdkFocusNodeId = ref<number | null>(null)
const sidebarCollapsed = ref(false)
const revision = ref(0)
const epoch = ref('')
const titles: Record<Page, string> = {
  overview: '工作台', nodes: '配置管理', channel_apps: '渠道应用', sdk_node_config: 'SDK 配置', sdk_services: '服务管理',
  subscribers: '接入同步', recharge: '充值模拟', about: '系统说明'
}
const currentTitle = computed(() => titles[navigation.value])
const currentSection = computed(() => ['sdk_node_config', 'sdk_services', 'channel_apps', 'recharge'].includes(navigation.value)
  ? 'SDK 管理' : ['nodes', 'subscribers'].includes(navigation.value) ? '配置中心' : '工作空间')
const activeGame = computed(() => games.value.find(game => game.id === currentGameId.value))
const updating = ref(false)
const loginSecret = ref('')
const loggingIn = ref(false)
watch(currentGameId, () => { focusNodeId.value = null }, { flush: 'sync' })

async function readRoute() {
  const target = window.location.hash.replace(/^#\/?/, '').split('/')[0] as Page
  const next = pageSet.includes(target) ? target : 'overview'
  if (next === navigation.value) return
  if (!(await confirmDiscardChanges())) {
    window.location.hash = '/' + navigation.value
    return
  }
  sdkFocusNodeId.value = null
  navigation.value = next
}
async function navigate(page: Page, sdkNodeId: number | null = null) {
  if (navigation.value !== page && !(await confirmDiscardChanges())) return
  if (page === 'sdk_node_config') sdkFocusNodeId.value = sdkNodeId
  if (navigation.value === page) return
  navigation.value = page
  window.location.hash = '/' + page
}
function openSDKConfig(nodeId: number) {
  void navigate('sdk_node_config', nodeId)
}
async function switchGame(gameId: number) {
  if (gameId === currentGameId.value || !(await confirmDiscardChanges())) return
  currentGameId.value = gameId
}
async function refreshRevision() {
  try {
    const result = await getRevision()
    revision.value = result.data.revision
    epoch.value = result.data.epoch
  } catch {
    ElMessage.error('读取配置版本失败')
  }
}
async function refreshAll() {
  updating.value = true
  try { await Promise.all([loadGames(), refreshRevision()]) }
  catch { ElMessage.error('刷新游戏列表失败') }
  finally { updating.value = false }
}
async function login() {
  if (loggingIn.value) return
  if (loginSecret.value.length < 32) {
    ElMessage.warning('管理密钥至少需要 32 字节')
    return
  }
  loggingIn.value = true
  setAdminSecret(loginSecret.value)
  try {
    await api.get('/admin/nodes/tree', { params: { parentId: 0 } })
    unlockAdmin()
    loginSecret.value = ''
    void refreshAll()
  } catch (error) {
    clearAdminSecret()
    if (axios.isAxiosError(error) && error.response?.status === 401) {
      ElMessage.error('管理密钥错误')
    } else {
      ElMessage.error(apiErrorMessage(error, '验证管理密钥失败'))
    }
  } finally {
    loggingIn.value = false
  }
}
async function logout() {
  if (!(await confirmDiscardChanges())) return
  window.removeEventListener('beforeunload', preventUnloadWithUnsavedChanges)
  clearAdminSecret()
  loginSecret.value = ''
  window.location.reload()
}
onMounted(() => {
  void readRoute()
  window.addEventListener('hashchange', readRoute)
  window.addEventListener('beforeunload', preventUnloadWithUnsavedChanges)
})
onUnmounted(() => {
  window.removeEventListener('hashchange', readRoute)
  window.removeEventListener('beforeunload', preventUnloadWithUnsavedChanges)
})
</script>

<template>
  <div v-if="!adminAuthenticated" class="admin-login">
    <section class="admin-login-panel">
      <div class="admin-login-logo">M</div>
      <h1>研发中台</h1>
      <p>输入管理密钥以访问后台</p>
      <el-input v-model="loginSecret" type="password" show-password clearable
                autocomplete="off" placeholder="请输入管理密钥"
                size="large" @keyup.enter="login" />
      <el-button type="primary" size="large" :loading="loggingIn" class="admin-login-button"
                 @click="login">进入管理后台</el-button>
      <span class="admin-login-hint">密钥仅保存在当前页面内存中，刷新后需重新输入</span>
    </section>
  </div>
  <div v-else class="shell v2-shell">
    <aside class="sidebar" :class="{ collapsed: sidebarCollapsed }">
      <div class="brand">
        <div class="brand-mark">M</div>
        <div v-if="!sidebarCollapsed">
          <strong>研发中台</strong><small>DEVELOPER PLATFORM</small>
        </div>
      </div>
      <div v-if="!sidebarCollapsed" class="group-title">工作空间</div>
      <button title="工作台" :class="{ active: navigation === 'overview' }" @click="navigate('overview')">
        <span class="nav-symbol">◫</span><span v-if="!sidebarCollapsed">工作台</span>
      </button>
      <div v-if="!sidebarCollapsed" class="group-title">配置中心</div>
      <button title="节点与基础配置" :class="{ active: navigation === 'nodes' }" @click="navigate('nodes')">
        <span class="nav-symbol">▦</span><span v-if="!sidebarCollapsed">节点与基础配置</span>
      </button>
      <button title="接入同步" :class="{ active: navigation === 'subscribers' }" @click="navigate('subscribers')">
        <span class="nav-symbol">↗</span><span v-if="!sidebarCollapsed">接入同步</span>
      </button>
      <div v-if="!sidebarCollapsed" class="group-title">SDK 管理</div>
      <button title="SDK 配置" :class="{ active: navigation === 'sdk_node_config' }" @click="navigate('sdk_node_config')">
        <span class="nav-symbol">⚙</span><span v-if="!sidebarCollapsed">SDK 配置</span>
      </button>
      <button title="渠道应用" :class="{ active: navigation === 'channel_apps' }" @click="navigate('channel_apps')">
        <span class="nav-symbol">▣</span><span v-if="!sidebarCollapsed">渠道应用</span>
      </button>
      <button title="服务管理" :class="{ active: navigation === 'sdk_services' }" @click="navigate('sdk_services')">
        <span class="nav-symbol">⚙</span><span v-if="!sidebarCollapsed">服务管理</span>
      </button>
      <div v-if="!sidebarCollapsed" class="group-title">其他</div>
      <button title="充值模拟" :class="{ active: navigation === 'recharge' }" @click="navigate('recharge')">
        <span class="nav-symbol">¥</span><span v-if="!sidebarCollapsed">充值模拟</span>
      </button>
      <button title="系统说明" :class="{ active: navigation === 'about' }" @click="navigate('about')">
        <span class="nav-symbol">ⓘ</span><span v-if="!sidebarCollapsed">系统说明</span>
      </button>
      <div class="sidebar-bottom">
        <button class="collapse-button" @click="sidebarCollapsed = !sidebarCollapsed">
          {{ sidebarCollapsed ? '»' : '« 收起导航' }}
        </button>
        <small v-if="!sidebarCollapsed">配置中心 · V2</small>
      </div>
    </aside>
    <div class="main">
      <header class="topbar">
        <div class="topbar-heading">
          <div class="topbar-breadcrumb">研发中台 / {{ currentSection }} / <span>{{ currentTitle }}</span></div>
          <strong>{{ currentTitle }}</strong>
        </div>
        <div class="top-right">
          <span class="topbar-game-label">当前游戏</span>
          <el-select :model-value="currentGameId" @change="switchGame" :loading="gamesLoading" filterable placeholder="选择游戏"
                     class="topbar-game-switcher">
            <el-option v-for="game in games" :key="game.id" :value="game.id"
                       :label="game.name + (game.marketType ? ' · ' + gameMarketLabels[game.marketType] : '')" />
          </el-select>
          <el-tag v-if="activeGame?.marketType" type="info" effect="plain">{{ gameMarketLabels[activeGame.marketType] }}</el-tag>
          <el-tag type="success" effect="plain">Revision {{ revision }}</el-tag>
          <el-button :loading="updating" @click="refreshAll">刷新</el-button>
          <el-button @click="logout">退出</el-button>
        </div>
      </header>
      <div class="page-content">
        <GameOverview v-if="navigation === 'overview'" :game-id="currentGameId" @navigate="navigate" />
        <div v-show="navigation === 'nodes'" class="workspace-shell">
          <ConfigWorkspace :game-id="currentGameId" @changed="refreshAll" @open-sdk-config="openSDKConfig" />
        </div>
        <ChannelAppPage v-if="navigation === 'channel_apps'" :game-id="currentGameId" @changed="refreshAll" />
        <SDKNodeConfigPage v-if="navigation === 'sdk_node_config'" :game-id="currentGameId" :focus-node-id="sdkFocusNodeId" />
        <SDKServicePage v-if="navigation === 'sdk_services'" />
        <SubscriberPage v-if="navigation === 'subscribers'" />
        <RechargeSimulatorPage v-if="navigation === 'recharge'" />
        <main v-if="navigation === 'about'" class="content-panel">
          <h2>系统说明</h2>
          <p>当前游戏：{{ activeGame?.name ?? '未选择' }}</p>
          <p>当前权威数据集：{{ epoch }}</p>
          <p>当前配置版本：{{ revision }}</p>
          <p>配置树：游戏 → 大区 → 区服组；渠道应用归属游戏，通过绑定关联区服组，公共配置支持环境覆盖。</p>
          <el-alert title="管理页面使用密钥签名认证；仍需通过 HTTPS 和受控网关访问。"
                    type="warning" show-icon :closable="false" />
        </main>
      </div>
    </div>
  </div>
</template>
