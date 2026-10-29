<script setup lang="ts">
import type { NodeForm } from '../types'

defineProps<{
  visible: boolean
  title: string
  detailLoading: boolean
  saving: boolean
  creating: boolean
  form: NodeForm
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
          <el-form-item label="状态">
            <el-radio-group v-model="form.status">
              <el-radio :value="1">启用</el-radio><el-radio :value="0">停用</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item label="本节点 JSON 配置">
            <el-input v-model="form.config" type="textarea" :rows="15" class="json-input" />
          </el-form-item>
          <div class="muted form-tip">配置不继承父节点；保存后版本递增并通知接入方拉取。</div>
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
