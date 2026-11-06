<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import JsonConfigEditor from '../../../components/JsonConfigEditor.vue'
import {
  businessConfigFields,
  getBusinessConfigValue,
  redactBusinessConfig,
  setBusinessConfigValue,
  validateBusinessConfig,
  type BusinessConfigField,
  type BusinessConfigType
} from '../businessConfig'

const props = withDefaults(defineProps<{
  modelValue: string
  nodeType: BusinessConfigType
  readonly?: boolean
}>(), { readonly: false })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const advanced = ref<string[]>([])

const fields = computed(() => businessConfigFields[props.nodeType])
const groupedFields = computed(() => {
  const groups = new Map<string, BusinessConfigField[]>()
  for (const field of fields.value) {
    const name = field.group ?? '其他配置'
    if (!groups.has(name)) groups.set(name, [])
    groups.get(name)!.push(field)
  }
  return [...groups].map(([name, items]) => ({ name, items }))
})
const error = computed(() => {
  try {
    validateBusinessConfig(props.nodeType, props.modelValue)
    return ''
  } catch (cause) {
    return cause instanceof Error ? cause.message : '配置格式错误'
  }
})

function valueOf(key: string): string {
  try { return getBusinessConfigValue(props.modelValue, key) }
  catch { return '' }
}

function changeField(key: string, value: string) {
  try { emit('update:modelValue', setBusinessConfigValue(props.modelValue, key, value)) }
  catch { ElMessage.warning('当前 JSON 无效，请先在高级配置中修正') }
}

function displayValue(field: BusinessConfigField): string {
  const value = valueOf(field.key)
  if (!value) return field.defaultValue ? `默认：${field.defaultValue}` : '未配置'
  return field.secret ? '已配置（隐藏）' : value
}
</script>

<template>
  <div class="business-config-editor">
    <template v-if="fields.length && !readonly">
      <section v-for="group in groupedFields" :key="group.name" class="config-field-group">
        <div class="section-heading"><h3>{{ group.name }}</h3></div>
        <el-form-item v-for="field in group.items" :key="field.key" :label="field.label">
          <el-input :model-value="valueOf(field.key)" :placeholder="field.placeholder"
                    :type="field.secret ? 'password' : 'text'" :show-password="!!field.secret"
                    :name="'sdk-config-' + field.key.replace(/\\./g, '-')"
                    :autocomplete="field.secret ? 'new-password' : 'off'" clearable
                    @update:model-value="(value: string) => changeField(field.key, value)" />
          <div class="muted form-tip">{{ field.description }}（{{ field.key }}）</div>
        </el-form-item>
      </section>
    </template>
    <template v-else-if="fields.length">
      <section v-for="group in groupedFields" :key="group.name" class="config-field-group">
        <div class="section-heading"><h3>{{ group.name }}</h3></div>
        <el-descriptions :column="1" border>
          <el-descriptions-item v-for="field in group.items" :key="field.key" :label="field.label">
            {{ displayValue(field) }}
          </el-descriptions-item>
        </el-descriptions>
      </section>
    </template>
    <p v-else class="muted">当前节点没有预置的业务配置字段，支持通过高级 JSON 自定义扩展。</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <el-collapse v-model="advanced" style="margin-top: 16px">
      <el-collapse-item title="高级 JSON 配置" name="json">
        <p class="muted">与上面的字段使用同一份 config；自定义字段会完整保留。原始 JSON 包含服务端密钥，请谨慎操作。</p>
        <JsonConfigEditor :model-value="readonly ? redactBusinessConfig(nodeType, modelValue) : modelValue"
                          :readonly="readonly" :rows="10"
                          @update:model-value="emit('update:modelValue', $event)" />
      </el-collapse-item>
    </el-collapse>
  </div>
</template>
