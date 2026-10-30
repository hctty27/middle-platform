<script setup lang="ts">
// 配置工作区负责展示懒加载目录树、直属子项列表和节点编辑抽屉。
// 数据加载、并发控制及表单状态集中交由 useNodeWorkspace 管理。
import type { NodeType, RegionEnvType } from './features/nodes/types'
import { regionEnvLabels } from './features/nodes/types'

function envLabel(env: RegionEnvType | null | undefined) {
  return env ? regionEnvLabels[env] : '未分类'
}
// 环境类型仅影响展示标签，不参与数据过滤或后端写入校验。
function envTagType(env: RegionEnvType | null | undefined): 'success' | 'warning' | 'primary' | 'info' {
  return env === 'prod' ? 'success' : env === 'test' ? 'warning' : env === 'review' ? 'primary' : 'info'
}
import { useNodeWorkspace } from './features/nodes/composables/useNodeWorkspace'
import NodeEditor from './features/nodes/components/NodeEditor.vue'

const emit = defineEmits<{ changed: [] }>()
const { labels, nextType, treeProps, treeRef, treeScrollRef, treeVersion, restoredExpandedKeys, searchWord, searchMatches, searchLoading, selected, breadcrumbs, children, childTotal, childPage, childSize, childSearch, regionFilter, onRegionFilterChange, childrenLoading, drawerVisible, creating, envTypeLocked, detailLoading, saving, form, nextLabel, editorTitle, loadTree, onExpand, onCollapse, onTreeClick, onSearchInput, openMatch, openChild, navigateBreadcrumb, onChildSearch, onPageChange, openEditor, openCreate, beforeDrawerClose, closeEditor, save, removeNode, refresh } = useNodeWorkspace(emit)
</script>

<template>
  <main class="workspace">
    <!-- 目录树只展示游戏、大区和区服组；渠道应用独立管理。 -->
    <section class="tree-panel">
      <div class="panel-title">
        <strong>配置目录</strong>
        <div class="panel-actions">
          <el-button size="small" @click="refresh">刷新</el-button>
          <el-button size="small" type="primary" @click="openCreate(null)">+ 游戏</el-button>
        </div>
      </div>
      <el-input v-model="searchWord" clearable placeholder="全局搜索名称或 Code"
                @input="onSearchInput" @clear="onSearchInput" />
      <div v-if="searchWord.trim()" class="tree-scroll search-results" v-loading="searchLoading">
        <div class="search-count">跨所有游戏、大区、区服组和渠道应用搜索</div>
        <el-empty v-if="!searchLoading && !searchMatches.length" description="无匹配节点" :image-size="75" />
        <button v-for="match in searchMatches" :key="match.node.id" class="search-result"
                @click="openMatch(match)">
          <span class="search-result-title">{{ match.node.name }}
            <el-tag size="small" type="info">{{ labels[match.node.nodeType] }}</el-tag>
            <el-tag v-if="match.node.nodeType === 'region'" size="small" :type="envTagType(match.node.envType)">{{ envLabel(match.node.envType) }}</el-tag>
          </span>
          <small>{{ match.path.map(item => item.name).join(' / ') }}</small>
          <small>Code: {{ match.node.code }}</small>
        </button>
      </div>
      <div v-show="!searchWord.trim()" ref="treeScrollRef" class="tree-scroll">
        <el-tree :key="treeVersion" ref="treeRef" node-key="id" lazy :load="loadTree"
                 :props="treeProps" :default-expanded-keys="restoredExpandedKeys"
                 :auto-expand-parent="false"
                 :current-node-key="selected?.id" highlight-current
                 :expand-on-click-node="false" @node-click="onTreeClick"
                 @node-expand="onExpand" @node-collapse="onCollapse">
          <template #default="{ data }">
            <span class="node-row">
              <span class="node-name" :title="data.name">{{ data.name }}</span>
              <el-tag size="small" effect="plain" type="info">{{ labels[data.nodeType as NodeType] }}</el-tag>
              <el-tag v-if="data.nodeType === 'region'" size="small" :type="envTagType(data.envType)">{{ envLabel(data.envType) }}</el-tag>
            </span>
          </template>
        </el-tree>
      </div>
      <div class="tree-hint">目录共三级；渠道应用请使用左侧「渠道应用」页面统一管理。</div>
    </section>

    <!-- 右侧展示当前节点及直属子项，避免一次渲染整棵配置树。 -->
    <section class="editor-panel">
      <div class="page-crumbs" v-if="breadcrumbs.length">
        <el-breadcrumb separator="/">
          <el-breadcrumb-item v-for="(item, index) in breadcrumbs" :key="item.id">
            <button class="crumb-button" :class="{ current: index === breadcrumbs.length - 1 }"
                    @click="navigateBreadcrumb(index)">{{ item.name }}</button>
          </el-breadcrumb-item>
        </el-breadcrumb>
      </div>
      <div v-if="!selected" class="empty-workspace">
        <el-empty description="从左侧选择一个节点，或直接搜索名称 / Code" />
      </div>
      <template v-else>
        <div class="node-heading">
          <div>
            <div class="node-heading-title">
              <h2>{{ selected.name }}</h2>
              <el-tag :type="selected.status === 1 ? 'success' : 'info'" effect="plain">
                {{ selected.status === 1 ? '已启用' : '已停用' }}
              </el-tag>
              <el-tag v-if="selected.nodeType === 'region'" :type="envTagType(selected.envType)">{{ envLabel(selected.envType) }}</el-tag>
            </div>
            <div class="node-meta">{{ labels[selected.nodeType] }} · ID {{ selected.id }} · Code {{ selected.code }}</div>
          </div>
          <div class="panel-actions">
            <el-button @click="openEditor(selected)">查看 / 编辑</el-button>
            <el-button type="danger" plain @click="removeNode(selected)">删除</el-button>
          </div>
        </div>
        <div class="children-toolbar">
          <div>
            <strong>{{ selected.nodeType === 'server_group' ? '渠道绑定' : nextLabel + '列表' }}</strong>
            <span v-if="selected.nodeType !== 'server_group'" class="muted">共 {{ childTotal }} 条</span>
          </div>
          <div class="children-tools" v-if="selected.nodeType !== 'channel_app'">
            <el-select v-if="selected.nodeType === 'game'" v-model="regionFilter" placeholder="全部大区类型"
                       clearable @change="onRegionFilterChange" style="width: 140px">
              <el-option v-for="(label, key) in regionEnvLabels" :key="key" :label="label" :value="key" />
            </el-select>
            <el-input v-model="childSearch" clearable placeholder="筛选直属子项" @input="onChildSearch" />
            <el-button type="primary" @click="openCreate(selected)">+ 新增{{ nextLabel }}</el-button>
          </div>
        </div>
        <div v-if="selected.nodeType === 'server_group'" class="leaf-tip">
          渠道应用在「渠道应用」页面统一创建，通过绑定关联到此区服组。
        </div>
        <template v-else>
          <div class="children-table">
            <el-table v-loading="childrenLoading" :data="children" stripe height="100%"
                      empty-text="暂无子项" @row-click="openChild">
              <el-table-column prop="name" label="名称" min-width="180" show-overflow-tooltip />
              <el-table-column prop="code" label="Code" min-width="145" show-overflow-tooltip />
              <el-table-column v-if="selected.nodeType === 'game'" label="大区类型" width="110">
                <template #default="{ row }">
                  <el-tag size="small" :type="envTagType(row.envType)">{{ envLabel(row.envType) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="状态" width="92">
                <template #default="{ row }">
                  <el-tag :type="row.status === 1 ? 'success' : 'info'" effect="plain" size="small">
                    {{ row.status === 1 ? '启用' : '停用' }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column label="操作" width="154" fixed="right">
                <template #default="{ row }">
                  <el-button link type="primary" @click.stop="openChild(row)">进入</el-button>
                  <el-button link @click.stop="openEditor(row)">编辑</el-button>
                  <el-button link type="danger" @click.stop="removeNode(row)">删除</el-button>
                </template>
              </el-table-column>
            </el-table>
          </div>
          <el-pagination v-if="childTotal > childSize" class="children-pagination"
                         :current-page="childPage" :page-size="childSize" :total="childTotal"
                         layout="total, prev, pager, next" @current-change="onPageChange" />
        </template>
      </template>
    </section>

    <NodeEditor v-model:visible="drawerVisible" :title="editorTitle"
                :detail-loading="detailLoading" :saving="saving" :creating="creating" :env-type-locked="envTypeLocked"
                :form="form" :labels="labels" :before-close="beforeDrawerClose"
                @cancel="closeEditor" @save="save" />
  </main>
</template>
