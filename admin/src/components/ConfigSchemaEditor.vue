<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import JsonConfigEditor from './JsonConfigEditor.vue'
import {
  getConfigSchema, getConfigValue, parseConfigObject, redactConfig, setConfigValue,
  validateConfigAgainstSchema, type FieldSchema, type SchemaResponse, type ConfigScope
} from '../api/configSchemaApi'
import { apiErrorMessage } from '../api/http'

const props = withDefaults(defineProps<{
  modelValue: string
  nodeType: string
  scope: ConfigScope
  readonly?: boolean
}>(), { readonly: false })
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const schema = ref<SchemaResponse | null>(null)
const fetchError = ref('')
const loading = ref(false)
const advanced = ref<string[]>([])
let sequence = 0

watch(() => [props.scope, props.nodeType] as const, async ([scope, nodeType]) => {
  const request = ++sequence
  schema.value = null
  fetchError.value = ''
  loading.value = true
  try {
    const result = await getConfigSchema(scope, nodeType)
    if (request === sequence) schema.value = result
  } catch (error) {
    if (request === sequence) fetchError.value = apiErrorMessage(error, '读取配置字段定义失败')
  } finally {
    if (request === sequence) loading.value = false
  }
}, { immediate: true })

interface FormField {
  key: string
  title: string
  group: string
  order: number
  rule: FieldSchema
}
const fields = computed<FormField[]>(() => {
  const result: FormField[] = []
  for (const [key, rule] of Object.entries(schema.value?.schema.properties ?? {})) {
    const group = rule['x-group'] || '其他配置'
    if (rule.type === 'object' && rule.properties) {
      for (const [nestedKey, nestedRule] of Object.entries(rule.properties)) {
        result.push({
          key: key + '.' + nestedKey, title: nestedRule.title || nestedKey,
          group, order: (rule['x-order'] ?? 0) * 100 + (nestedRule['x-order'] ?? 0),
          rule: nestedRule
        })
      }
    } else {
      result.push({ key, title: rule.title || key, group, order: (rule['x-order'] ?? 0) * 100, rule })
    }
  }
  return result.sort((a, b) => a.order - b.order)
})
const groups = computed(() => {
  const result = new Map<string, FormField[]>()
  for (const field of fields.value) {
    if (!result.has(field.group)) result.set(field.group, [])
    result.get(field.group)!.push(field)
  }
  return [...result].map(([name, items]) => ({ name, items }))
})
const validationError = computed(() => {
  try {
    if (schema.value) validateConfigAgainstSchema(props.modelValue, schema.value.schema)
    else parseConfigObject(props.modelValue)
    return ''
  } catch (error) { return error instanceof Error ? error.message : '配置格式错误' }
})
const displayedRaw = computed(() => {
  if (!props.readonly) return props.modelValue
  if (!schema.value) return '{}'
  try { return redactConfig(props.modelValue, schema.value.schema) }
  catch { return '配置格式不正确' }
})
function valueOf(key: string): string {
  try { return getConfigValue(props.modelValue, key) }
  catch { return '' }
}
function changeField(key: string, value: string) {
  try { emit('update:modelValue', setConfigValue(props.modelValue, key, value)) }
  catch { ElMessage.warning('当前 JSON 无效，请先在高级配置中修正') }
}
function displayValue(field: FormField): string {
  const value = valueOf(field.key)
  if (!value) return typeof field.rule.default === 'string' ? '默认：' + field.rule.default : '未配置'
  return field.rule.writeOnly ? '已配置（隐藏）' : value
}
</script>

<template>
  <div class="business-config-editor" v-loading="loading">
    <el-alert v-if="fetchError" :title="fetchError" type="warning" :closable="false" show-icon />
    <template v-if="fields.length && !readonly">
      <section v-for="group in groups" :key="group.name" class="config-field-group">
        <div class="section-heading"><h3>{{ group.name }}</h3></div>
        <el-form-item v-for="field in group.items" :key="field.key" :label="field.title">
          <el-select v-if="field.rule.enum?.length" :model-value="valueOf(field.key)"
                     :placeholder="field.rule['x-placeholder'] || '请选择'"
                     clearable style="width:100%"
                     @update:model-value="(value: string) => changeField(field.key, value)">
            <el-option v-for="choice in field.rule.enum" :key="choice" :value="choice" :label="choice" />
          </el-select>
          <el-input v-else :model-value="valueOf(field.key)"
                    :placeholder="field.rule['x-placeholder'] || (typeof field.rule.default === 'string' ? '默认：' + field.rule.default : '')"
                    :type="field.rule.writeOnly ? 'password' : 'text'"
                    :show-password="!!field.rule.writeOnly"
                    :name="'config-' + scope + '-' + field.key.replaceAll('.', '-')"
                    :autocomplete="field.rule.writeOnly ? 'new-password' : 'off'" clearable
                    @update:model-value="(value: string) => changeField(field.key, value)" />
          <div class="muted form-tip">{{ field.rule.description }}（{{ field.key }}）</div>
        </el-form-item>
      </section>
    </template>
    <template v-else-if="fields.length">
      <section v-for="group in groups" :key="group.name" class="config-field-group">
        <div class="section-heading"><h3>{{ group.name }}</h3></div>
        <el-descriptions :column="1" border>
          <el-descriptions-item v-for="field in group.items" :key="field.key" :label="field.title">
            {{ displayValue(field) }}
          </el-descriptions-item>
        </el-descriptions>
      </section>
    </template>
    <p v-else-if="!loading && !fetchError" class="muted">当前节点没有预置业务字段，支持通过高级 JSON 自定义扩展。</p>
    <el-alert v-if="validationError" :title="validationError" type="error" :closable="false" show-icon />
    <el-collapse v-model="advanced" style="margin-top:16px">
      <el-collapse-item title="高级 JSON 配置" name="json">
        <p class="muted">与表单共用同一份 config；未知扩展字段会被保留。原始配置可能包含密钥，请谨慎操作。</p>
        <JsonConfigEditor :model-value="displayedRaw" :readonly="readonly" :rows="10"
                          @update:model-value="emit('update:modelValue', $event)" />
      </el-collapse-item>
    </el-collapse>
  </div>
</template>
