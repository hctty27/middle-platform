<script setup lang="ts">
import { computed } from 'vue'
import { ElMessage } from 'element-plus'

const props = withDefaults(defineProps<{ modelValue: string; rows?: number; readonly?: boolean }>(), {
  rows: 12, readonly: false
})
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const validation = computed(() => {
  try {
    const value: unknown = JSON.parse(props.modelValue)
    if (value === null || Array.isArray(value) || typeof value !== 'object') return '配置必须是 JSON 对象'
    return ''
  } catch (error) {
    return error instanceof Error ? error.message : 'JSON 格式错误'
  }
})
function format() {
  if (validation.value) return
  emit('update:modelValue', JSON.stringify(JSON.parse(props.modelValue), null, 2))
}
async function copy() {
  try { await navigator.clipboard.writeText(props.modelValue); ElMessage.success('已复制 JSON') }
  catch { ElMessage.warning('复制失败，请手动选择文本复制') }
}
</script>

<template>
  <div class="json-editor">
    <div class="json-editor-toolbar">
      <span :class="validation ? 'json-invalid' : 'json-valid'">
        {{ validation ? 'JSON 校验失败' : 'JSON 格式正确' }}
      </span>
      <div class="panel-actions">
        <el-button link :disabled="readonly || !!validation" @click="format">格式化</el-button>
        <el-button link @click="copy">复制</el-button>
      </div>
    </div>
    <el-input :model-value="modelValue" :readonly="readonly" type="textarea" :rows="rows"
              class="json-input" spellcheck="false"
              @update:model-value="emit('update:modelValue', $event)" />
    <p v-if="validation" class="json-error">{{ validation }}</p>
  </div>
</template>
