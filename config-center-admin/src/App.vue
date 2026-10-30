<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { getRevision } from './api/syncApi'
import { currentGameId, focusNodeId, games, gamesLoading, loadGames } from './app/gameContext'
import ConfigWorkspace from './ConfigWorkspace.vue'
import GameOverview from './features/dashboard/GameOverview.vue'
import ChannelAppPage from './features/nodes/ChannelAppPage.vue'
import SubscriberPage from './features/subscribers/SubscriberPage.vue'

type Page = 'overview' | 'nodes' | 'channel_apps' | 'subscribers' | 'about'
const pageSet: Page[] = ['overview', 'nodes', 'channel_apps', 'subscribers', 'about']
const navigation = ref<Page>('overview')
const sidebarCollapsed = ref(false)
const revision = ref(0)
const epoch = ref('')
const titles: Record<Page, string> = {
  overview: '工作台', nodes: '配置管理', channel_apps: '渠道应用',
  subscribers: '接入同步', about: '系统说明'
}
const currentTitle = computed(() => titles[navigation.value])
const activeGame = computed(() => games.value.find(game => game.id === currentGameId.value))
const updating = ref(false)
watch(currentGameId, () => { focusNodeId.value = null }, { flush: 'sync' })

function readRoute() {
  const target = window.location.hash.replace(/^#\/?/, '').split('/')[0] as Page
  navigation.value = pageSet.includes(target) ? target : 'overview'
}
function navigate(page: Page) {
  if (navigation.value === page) return
  window.location.hash = '/' + page
  navigation.value = page
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
onMounted(() => {
  readRoute()
  window.addEventListener('hashchange', readRoute)
  void refreshAll()
})
onUnmounted(() => window.removeEventListener('hashchange', readRoute))
</script>

<template>
  <div class="shell v2-shell">
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
      <button title="配置管理" :class="{ active: navigation === 'nodes' }" @click="navigate('nodes')">
        <span class="nav-symbol">▦</span><span v-if="!sidebarCollapsed">配置管理</span>
      </button>
      <button title="渠道应用" :class="{ active: navigation === 'channel_apps' }" @click="navigate('channel_apps')">
        <span class="nav-symbol">▣</span><span v-if="!sidebarCollapsed">渠道应用</span>
      </button>
      <button title="接入同步" :class="{ active: navigation === 'subscribers' }" @click="navigate('subscribers')">
        <span class="nav-symbol">↗</span><span v-if="!sidebarCollapsed">接入同步</span>
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
          <div class="topbar-breadcrumb">研发中台 / 配置中心 / <span>{{ currentTitle }}</span></div>
          <strong>{{ currentTitle }}</strong>
        </div>
        <div class="top-right">
          <span class="topbar-game-label">当前游戏</span>
          <el-select v-model="currentGameId" :loading="gamesLoading" filterable placeholder="选择游戏"
                     class="topbar-game-switcher">
            <el-option v-for="game in games" :key="game.id" :value="game.id" :label="game.name" />
          </el-select>
          <el-tag type="success" effect="plain">Revision {{ revision }}</el-tag>
          <el-button :loading="updating" @click="refreshAll">刷新</el-button>
        </div>
      </header>
      <div class="page-content">
        <GameOverview v-if="navigation === 'overview'" :game-id="currentGameId" @navigate="navigate" />
        <div v-show="navigation === 'nodes'" class="workspace-shell">
          <ConfigWorkspace :game-id="currentGameId" @changed="refreshAll" />
        </div>
        <ChannelAppPage v-if="navigation === 'channel_apps'" :game-id="currentGameId" @changed="refreshAll" />
        <SubscriberPage v-if="navigation === 'subscribers'" />
        <main v-if="navigation === 'about'" class="content-panel">
          <h2>系统说明</h2>
          <p>当前游戏：{{ activeGame?.name ?? '未选择' }}</p>
          <p>当前权威数据集：{{ epoch }}</p>
          <p>当前配置版本：{{ revision }}</p>
          <p>配置树：游戏 → 大区 → 区服组；渠道应用归属游戏，通过绑定关联区服组，公共配置支持环境覆盖。</p>
          <el-alert title="管理后台没有登录和账号权限，请仅通过内网或受控网关开放。"
                    type="warning" show-icon :closable="false" />
        </main>
      </div>
    </div>
  </div>
</template>
