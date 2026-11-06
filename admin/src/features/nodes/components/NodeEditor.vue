<script setup lang="ts">
// 编辑抽屉保持展示层职责：表单校验、保存和关闭决策由父级工作区处理。
import type { NodeForm, NodeType } from '../types'
import JsonConfigEditor from '../../../components/JsonConfigEditor.vue'
import { regionEnvLabels } from '../types'

// envTypeLocked 用于限制已有大区修改环境分类，防止运行环境语义变化。
defineProps<{
  visible: boolean
  title: string
  detailLoading: boolean
  saving: boolean
  creating: boolean
  envTypeLocked: boolean
  form: NodeForm
  labels: Record<NodeType, string>
  beforeClose: (done: () => void) => void
}>()

const emit = defineEmits<{
  'update:visible': [value: boolean]
  cancel: []
  save: []
}>()
</script>

<template>
    <el-drawer  :model-value="visible" @update:model-value="emit('update:visible', $event)" :title="title" size="min(720px, 96vw)"
               :before-close="beforeClose" :close-on-click-modal="false">
      <div v-loading="detailLoading" class="drawer-content">
        <el-form v-if="!detailLoading" label-position="top" class="node-form">
          <div class="form-grid">
            <el-form-item label="节点类型"><el-input :model-value="labels[form.nodeType]" disabled /></el-form-item>
            <el-form-item label="父节点 ID（根据层级推导）"><el-input :model-value="String(form.parentId)" disabled /></el-form-item>
            <el-form-item label="业务编码"><el-input v-model="form.code" :disabled="!creating" maxlength="128" /></el-form-item>
            <el-form-item label="节点名称"><el-input v-model="form.name" maxlength="128" show-word-limit /></el-form-item>
          </div>
          <el-form-item v-if="form.nodeType === 'game'" label="运营地区" required>
            <el-radio-group v-model="form.marketType">
              <el-radio value="domestic">国内</el-radio>
              <el-radio value="overseas">海外</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="form.nodeType === 'region'" label="大区类型" required>
            <el-select v-model="form.envType" placeholder="请选择大区类型" :disabled="envTypeLocked" clearable>
              <el-option v-for="(label, key) in regionEnvLabels" :key="key" :label="label" :value="key" />
            </el-select>
            <div class="muted form-tip" v-if="!creating && !form.envType">历史大区未分类，请核对用途后选择。</div>
          </el-form-item>
          <el-form-item label="状态">
            <el-radio-group v-model="form.status">
              <el-radio :value="1">启用</el-radio><el-radio :value="0">停用</el-radio>
            </el-radio-group>
          </el-form-item>
          <div class="section-heading"><h3>业务配置</h3></div>
          <p class="muted form-tip">仅存储基础节点的自定义配置；SDK 密钥、适配器及回调地址请在「SDK 配置」中编辑。</p>
          <JsonConfigEditor v-model="form.config" :rows="8" />
          <div class="muted form-tip">此处不保存 SDK 专属配置。</div>
          <el-form-item label="备注">
            <el-input v-model="form.remark" type="textarea" maxlength="500" show-word-limit />
          </el-form-item>
        </el-form>
      </div>
      <template #footer>
        <el-button :disabled="saving" @click="emit('cancel')">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="detailLoading" @click="emit('save')">保存配置</el-button>
      </template>
    </el-drawer>
</template>
