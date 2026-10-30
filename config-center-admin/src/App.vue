<script setup lang="ts">
// 管理平台入口：切换配置管理与订阅者页面，并展示权威配置的当前版本。
import { onMounted, ref } from 'vue'
import SubscriberPage from './features/subscribers/SubscriberPage.vue'
import { getRevision } from './api/syncApi'
import { ElMessage } from 'element-plus'
import ConfigWorkspace from './ConfigWorkspace.vue'
import ChannelAppPage from './features/nodes/ChannelAppPage.vue'

const navigation = ref<'nodes' | 'channel_apps' | 'subscribers' | 'about'>('nodes')
const sidebarCollapsed = ref(false)
const revision = ref(0)
const epoch = ref('')

// 页面首次加载及节点变更后刷新版本，避免展示过期的同步状态。
async function refreshRevision() {
  try {
    const result = await getRevision()
    revision.value = result.data.revision
    epoch.value = result.data.epoch
  } catch { ElMessage.error('读取配置版本失败') }
}

function navigate(tab: 'nodes' | 'channel_apps' | 'subscribers' | 'about') {
  navigation.value = tab
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
      <button title="渠道应用" :class="{ active: navigation === 'channel_apps' }" @click="navigate('channel_apps')">
        <span class="nav-symbol">▣</span><span v-if="!sidebarCollapsed">渠道应用</span>
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
          <strong>{{ navigation === 'nodes' ? '配置管理' : navigation === 'channel_apps' ? '渠道应用' : navigation === 'subscribers' ? '接入与同步' : '系统说明' }}</strong>
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
      <ChannelAppPage v-if="navigation === 'channel_apps'" @changed="refreshRevision" />
      <SubscriberPage v-if="navigation === 'subscribers'" />
      <main v-else-if="navigation === 'about'" class="content-panel">
        <h2>系统说明</h2>
        <p>当前权威数据集：{{ epoch }}</p>
        <p>当前配置版本：{{ revision }}</p>
        <p>配置树：游戏 → 大区 → 区服组；渠道应用归属游戏，通过绑定关联区服组，公共配置支持环境覆盖。</p>
        <el-alert title="管理后台没有登录和账号权限，必须通过内网或受控网关限制访问。"
                  type="warning" show-icon :closable="false" />
      </main>
    </div>
  </div>
</template>
