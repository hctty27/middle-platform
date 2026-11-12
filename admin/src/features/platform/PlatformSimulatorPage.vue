<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { apiErrorMessage, sdkApi, sdkServiceApi } from '../../api/http'
import { generateNonce, signPaymentCallback, signServiceRequest } from './signature'

type Environment = 'test' | 'review' | 'prod'
type ApiKey = 'role_query' | 'server_query' | 'role_control' | 'payment_callback'
type ServiceApiKey = Exclude<ApiKey, 'payment_callback'>
type RoleControlType = 'close' | 'unClose' | 'lock' | 'unLock'
type RequestBody = Record<string, string | number>

interface ChannelApp {
  id: number
  code: string
  name: string
  status: number
  config: string
}

interface BuiltRequest {
  url: string
  headers: Record<string, string>
  requestBody: string
  acknowledgement?: string
}

interface RequestResult {
  status: number | null
  durationMs: number
  url: string
  headers: Record<string, string>
  requestBody: string
  responseBody: string
  error?: string
  acknowledged?: boolean
  preview?: boolean
}

interface ServicePayload {
  path: string
  body: RequestBody
  signatureValues: string[]
}

const props = defineProps<{ gameId: number | null }>()

const apps = ref<ChannelApp[]>([])
const selectedAppCode = ref('')
const environment = ref<Environment>('test')
const activeApi = ref<ApiKey>('role_query')
const loadingApps = ref(false)
const submitting = ref(false)
const allowPaymentCallback = ref(false)
const results = ref<Partial<Record<ApiKey, RequestResult>>>({})
let appRequestSequence = 0

const roleQuery = ref({
  openId: '',
  roleId: '',
  originalServerId: '',
  currentServerId: '',
  extension: ''
})
const serverQuery = ref({
  originalServerId: '',
  currentServerId: '',
  page: '1',
  pageSize: '',
  zonekey: ''
})
const roleControl = ref({
  type: 'close' as RoleControlType,
  serverId: '',
  openId: '',
  roleId: '',
  roleName: '',
  ip: '',
  duration: '3600',
  describe: '',
  extension: ''
})
const paymentCallback = ref({
  orderNo: 'P' + Date.now(),
  gameOrderId: '',
  amount: '600',
  attach: '',
  paidAt: ''
})

const selectedApp = computed(() => apps.value.find(item => item.code === selectedAppCode.value) ?? null)

function readAppSecret(app: ChannelApp | null): string {
  if (!app) return ''
  try {
    const config = JSON.parse(app.config) as { appSecret?: unknown }
    return typeof config.appSecret === 'string' ? config.appSecret : ''
  } catch {
    return ''
  }
}

const appSecret = computed(() => readAppSecret(selectedApp.value))
const currentResult = computed(() => results.value[activeApi.value] ?? null)
const canPreview = computed(() => !!selectedApp.value && !!appSecret.value)
const canSend = computed(() => {
  if (!canPreview.value || selectedApp.value?.status !== 1) return false
  if (activeApi.value === 'payment_callback') return allowPaymentCallback.value
  if (activeApi.value === 'role_control' && environment.value === 'prod') return false
  return true
})
const sdkServiceBaseUrl = computed(() => sdkServiceApi.defaults.baseURL || '/api/service')
const sdkCoreBaseUrl = computed(() => sdkApi.defaults.baseURL || '/api/core')

function resolveUrl(baseUrl: string, path: string): string {
  const base = new URL(baseUrl.replace(/\/+$/, '') + '/', window.location.href)
  return new URL(path.replace(/^\/+/, ''), base).toString()
}

function parseInteger(raw: string, label: string, min: number, max: number): number {
  const value = raw.trim()
  if (!/^\d+$/.test(value)) throw new Error(label + '必须是整数')
  const number = Number(value)
  if (!Number.isSafeInteger(number) || number < min || number > max) {
    throw new Error(label + '范围必须为 ' + min + ' 到 ' + max)
  }
  return number
}

function addOptional(body: RequestBody, values: string[], key: string, raw: string): void {
  values.push(raw)
  if (raw !== '') body[key] = raw
}

function buildServicePayload(key: ServiceApiKey): ServicePayload {
  if (key === 'role_query') {
    const body: RequestBody = {}
    const values: string[] = []
    addOptional(body, values, 'open_id', roleQuery.value.openId)
    addOptional(body, values, 'role_id', roleQuery.value.roleId)
    addOptional(body, values, 'original_server_id', roleQuery.value.originalServerId)
    addOptional(body, values, 'current_server_id', roleQuery.value.currentServerId)
    addOptional(body, values, 'extension', roleQuery.value.extension)
    if (!values.slice(0, 4).some(value => value.trim() !== '')) {
      throw new Error('open_id、role_id、original_server_id、current_server_id 至少填写一项')
    }
    return { path: 'role/query', body, signatureValues: values }
  }

  if (key === 'server_query') {
    const page = parseInteger(serverQuery.value.page, 'page', 1, 1000000)
    const body: RequestBody = { page }
    const values: string[] = []
    addOptional(body, values, 'original_server_id', serverQuery.value.originalServerId)
    addOptional(body, values, 'current_server_id', serverQuery.value.currentServerId)
    values.push(String(page))

    const pageSizeRaw = serverQuery.value.pageSize.trim()
    if (pageSizeRaw) {
      const pageSize = parseInteger(pageSizeRaw, 'page_size', 1, 1000)
      body.page_size = pageSize
      values.push(String(pageSize))
    }
    addOptional(body, values, 'zonekey', serverQuery.value.zonekey)
    return { path: 'server/query', body, signatureValues: values }
  }

  const input = roleControl.value
  const body: RequestBody = { type: input.type }
  const values: string[] = [input.type]
  addOptional(body, values, 'server_id', input.serverId)
  addOptional(body, values, 'open_id', input.openId)
  addOptional(body, values, 'role_id', input.roleId)
  addOptional(body, values, 'role_name', input.roleName)
  addOptional(body, values, 'ip', input.ip)

  const durationRaw = input.duration.trim()
  if (durationRaw) {
    const duration = parseInteger(durationRaw, 'duration', 0, 9007199254740991)
    body.duration = duration
    values.push(String(duration))
  } else if (input.type === 'close' || input.type === 'lock') {
    throw new Error('封号和禁言必须填写 duration')
  }

  addOptional(body, values, 'describe', input.describe)
  addOptional(body, values, 'extension', input.extension)

  if (![input.openId, input.roleId, input.roleName, input.ip].some(value => value.trim())) {
    throw new Error('open_id、role_id、role_name、ip 至少填写一项')
  }
  return { path: 'role/control', body, signatureValues: values }
}

function validateOrderId(value: string, label: string): string {
  const id = value.trim()
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(id)) throw new Error(label + '格式无效')
  return id
}

function validatePaymentCallback(): {
  orderNo: string
  gameOrderId: string
  amount: number
  attach: string
  paidAt: string
} {
  const orderNo = validateOrderId(paymentCallback.value.orderNo, '平台订单号')
  const gameOrderId = validateOrderId(paymentCallback.value.gameOrderId, '游戏订单号')
  const amount = parseInteger(paymentCallback.value.amount, '金额（分）', 1, Number.MAX_SAFE_INTEGER)
  const attach = paymentCallback.value.attach
  if (new TextEncoder().encode(attach).byteLength > 4096) throw new Error('attach 超过 4096 字节')

  const paidAt = paymentCallback.value.paidAt.trim() || new Date().toISOString()
  const rfc3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/
  if (!rfc3339.test(paidAt) || !Number.isFinite(Date.parse(paidAt))) {
    throw new Error('paid_at 必须是 RFC3339 时间格式')
  }
  return { orderNo, gameOrderId, amount, attach, paidAt }
}

async function buildSignedRequest(apiKey: ApiKey): Promise<BuiltRequest> {
  const app = selectedApp.value
  const secret = appSecret.value
  const env = environment.value
  if (!app) throw new Error('请先选择渠道应用')
  if (!secret) throw new Error('当前渠道应用未配置 appSecret')
  if (apiKey === 'payment_callback' && !/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(app.code)) {
    throw new Error('当前 AppID 不符合 SDK Core 支付回调接口要求')
  }
  if (apiKey !== 'payment_callback' && !/^[A-Za-z0-9_-]{1,128}$/.test(app.code)) {
    throw new Error('当前 AppID 不符合 SDK Service 接口要求')
  }

  const timestamp = String(Math.floor(Date.now() / 1000))
  const nonce = generateNonce()

  if (apiKey === 'payment_callback') {
    const input = validatePaymentCallback()
    const signature = await signPaymentCallback(
      secret, input.orderNo, input.gameOrderId, nonce, timestamp
    )
    const body = {
      order_no: input.orderNo,
      game_order_id: input.gameOrderId,
      amount: input.amount,
      attach: input.attach,
      paid_at: input.paidAt
    }
    return {
      url: resolveUrl(sdkCoreBaseUrl.value, 'pay/notify/' + encodeURIComponent(app.code)),
      headers: { 'x-timestamp': timestamp, 'x-nonce': nonce, 'x-signature': signature },
      requestBody: JSON.stringify(body),
      acknowledgement: nonce
    }
  }

  const payload = buildServicePayload(apiKey)
  const signature = await signServiceRequest(
    secret, app.code, timestamp, nonce, payload.signatureValues
  )
  return {
    url: resolveUrl(sdkServiceBaseUrl.value, payload.path + '/' + env),
    headers: {
      'x-app-id': app.code,
      'x-timestamp': timestamp,
      'x-nonce': nonce,
      'x-signature': signature
    },
    requestBody: JSON.stringify(payload.body)
  }
}

async function executeRequest(request: BuiltRequest): Promise<RequestResult> {
  const started = performance.now()
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 40000)
  const headers = { 'Content-Type': 'application/json', ...request.headers }
  try {
    const response = await fetch(request.url, {
      method: 'POST',
      headers,
      body: request.requestBody,
      signal: controller.signal,
      credentials: 'omit',
      cache: 'no-store'
    })
    const responseBody = await response.text()
    return {
      status: response.status,
      durationMs: Math.round(performance.now() - started),
      url: request.url,
      headers,
      requestBody: request.requestBody,
      responseBody,
      acknowledged: request.acknowledgement
        ? response.status === 200 && responseBody === request.acknowledgement
        : undefined
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    return {
      status: null,
      durationMs: Math.round(performance.now() - started),
      url: request.url,
      headers,
      requestBody: request.requestBody,
      responseBody: '',
      error: '请求失败：' + message + '（请检查服务地址、反向代理、CORS 和 HTTPS 混合内容限制）'
    }
  } finally {
    window.clearTimeout(timeout)
  }
}

async function previewRequest(): Promise<void> {
  try {
    const apiKey = activeApi.value
    const appCode = selectedAppCode.value
    const env = environment.value
    const request = await buildSignedRequest(apiKey)
    if (appCode === selectedAppCode.value && env === environment.value) {
      results.value = {
        ...results.value,
        [apiKey]: {
          status: null,
          durationMs: 0,
          url: request.url,
          headers: { 'Content-Type': 'application/json', ...request.headers },
          requestBody: request.requestBody,
          responseBody: '',
          preview: true
        }
      }
    }
    ElMessage.success('请求已生成，尚未发送')
  } catch (error) {
    ElMessage.warning(error instanceof Error ? error.message : '请求参数不合法')
  }
}

async function sendRequest(): Promise<void> {
  if (submitting.value || !canSend.value) return
  const apiKey = activeApi.value
  const appCode = selectedAppCode.value
  const env = environment.value
  submitting.value = true

  try {
    if (apiKey === 'role_control') {
      try {
        await ElMessageBox.confirm(
          '此操作会调用真实游戏服的封禁、解封或禁言接口。请确认使用的是测试角色。',
          '确认角色控制操作',
          { type: 'warning', confirmButtonText: '确认发送', cancelButtonText: '取消' }
        )
      } catch {
        return
      }
    }

    if (apiKey === 'payment_callback') {
      try {
        await ElMessageBox.confirm(
          '此操作会确认订单支付，并可能触发游戏服真实发货。请确认订单属于隔离测试环境。',
          '确认平台充值回调',
          { type: 'warning', confirmButtonText: '确认发送', cancelButtonText: '取消' }
        )
      } catch {
        return
      }
    }

    const request = await buildSignedRequest(apiKey)
    const result = await executeRequest(request)
    if (appCode === selectedAppCode.value && env === environment.value) {
      results.value = { ...results.value, [apiKey]: result }
    }

    if (apiKey === 'payment_callback') {
      if (result.acknowledged) {
        ElMessage.success('支付回调已确认；游戏发货由后台 Worker 异步处理')
      } else {
        ElMessage.error(result.error || '支付回调未被正确确认，请查看响应内容')
      }
    } else if (result.status !== null && result.status >= 200 && result.status < 300) {
      ElMessage.success('请求已发送，请检查响应内容')
    } else {
      ElMessage.error(result.error || '请求失败，请查看响应内容')
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '请求发送失败')
  } finally {
    submitting.value = false
    if (apiKey === 'payment_callback') allowPaymentCallback.value = false
  }
}

function prettyJSON(value: string): string {
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

async function copyText(value: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value)
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败，请检查浏览器剪贴板权限')
  }
}

async function loadApps(): Promise<void> {
  const request = ++appRequestSequence
  if (!props.gameId) {
    apps.value = []
    selectedAppCode.value = ''
    loadingApps.value = false
    return
  }

  loadingApps.value = true
  try {
    const { data } = await sdkApi.get<ChannelApp[]>('/admin/channel-apps', {
      params: { gameId: props.gameId }
    })
    if (request !== appRequestSequence) return
    apps.value = data
    const selected = data.find(item => item.code === selectedAppCode.value)
      ?? data.find(item => item.status === 1)
      ?? data[0]
    selectedAppCode.value = selected?.code ?? ''
  } catch (error) {
    if (request === appRequestSequence) {
      ElMessage.error(apiErrorMessage(error, '加载渠道应用失败'))
    }
  } finally {
    if (request === appRequestSequence) loadingApps.value = false
  }
}

watch(() => props.gameId, () => {
  results.value = {}
  allowPaymentCallback.value = false
  void loadApps()
}, { immediate: true })

watch(selectedAppCode, () => {
  results.value = {}
  allowPaymentCallback.value = false
})

watch(environment, () => {
  results.value = {}
  allowPaymentCallback.value = false
})

watch(() => roleControl.value.type, type => {
  if (type === 'unClose' || type === 'unLock') {
    roleControl.value.duration = ''
  } else if (!roleControl.value.duration) {
    roleControl.value.duration = '3600'
  }
})
</script>

<template>
  <main class="platform-simulator-page">
    <header class="simulator-heading">
      <div>
        <div class="simulator-eyebrow">PLATFORM API TESTING</div>
        <h1>发行平台模拟</h1>
        <p>模拟发行平台调用中台接口，自动读取渠道密钥、生成签名并查看真实响应。</p>
      </div>
      <el-tag type="warning" effect="plain">真实接口联调</el-tag>
    </header>

    <el-alert
      v-if="!props.gameId"
      title="请先选择游戏，再选择对应的渠道应用。"
      type="info"
      show-icon
      :closable="false"
      class="simulator-alert"
    />

    <section class="simulator-panel">
      <el-form label-position="top" autocomplete="off" class="simulator-common-form">
        <div class="simulator-common-fields">
        <el-form-item label="渠道应用" required>
          <el-select
            v-model="selectedAppCode"
            filterable
            :loading="loadingApps"
            :disabled="!props.gameId"
            placeholder="选择当前游戏的渠道应用"
            style="width: 100%"
            autocomplete="off"
          >
            <el-option
              v-for="app in apps"
              :key="app.id"
              :value="app.code"
              :label="app.name + ' (' + app.code + ')' + (app.status === 1 ? '' : ' · 已停用')"
            />
          </el-select>
        </el-form-item>
        <el-form-item v-if="activeApi !== 'payment_callback'" label="请求环境" required>
          <el-select v-model="environment" style="width: 100%">
            <el-option label="测试服（test）" value="test" />
            <el-option label="提审服（review）" value="review" />
            <el-option label="正式服（prod）" value="prod" />
          </el-select>
        </el-form-item>
        <el-form-item label="密钥状态">
          <el-tag v-if="appSecret" type="success">已读取 appSecret</el-tag>
          <el-tag v-else type="danger">未配置 appSecret</el-tag>
          <span class="simulator-help">密钥仅在当前页面内存中使用，不展示、不持久化。</span>
        </el-form-item>
        </div>
      </el-form>

      <div class="simulator-endpoints">
        <span>SDK Service：{{ sdkServiceBaseUrl }}</span>
        <span>SDK Core：{{ sdkCoreBaseUrl }}</span>
      </div>

      <el-alert
        v-if="selectedApp && selectedApp.status !== 1"
        title="当前渠道应用已停用，不能发送真实请求。"
        type="warning"
        show-icon
        :closable="false"
        class="simulator-alert"
      />
      <el-alert
        v-if="selectedApp && !appSecret"
        title="当前渠道应用配置中未找到 appSecret，请先到渠道应用配置中设置。"
        type="error"
        show-icon
        :closable="false"
        class="simulator-alert"
      />
      <el-alert
        v-if="activeApi === 'payment_callback'"
        title="充值回调会确认订单支付，并由后台 Worker 异步通知游戏服；此操作可能触发真实发货。"
        type="warning"
        show-icon
        :closable="false"
        class="simulator-alert"
      />
      <el-alert
        v-if="activeApi === 'role_control' && environment === 'prod'"
        title="正式服禁止通过模拟页面执行封禁、解封或禁言操作；可以生成请求预览，但不会发送。"
        type="warning"
        show-icon
        :closable="false"
        class="simulator-alert"
      />

      <el-tabs v-model="activeApi" class="simulator-tabs">
        <el-tab-pane label="角色查询" name="role_query">
          <el-form label-position="top" autocomplete="off" class="simulator-form" @submit.prevent="sendRequest">
            <div class="simulator-fields">
              <el-form-item label="open_id"><el-input v-model="roleQuery.openId" placeholder="user_10001" /></el-form-item>
              <el-form-item label="role_id"><el-input v-model="roleQuery.roleId" placeholder="role_10001" /></el-form-item>
              <el-form-item label="original_server_id"><el-input v-model="roleQuery.originalServerId" placeholder="1001" /></el-form-item>
              <el-form-item label="current_server_id"><el-input v-model="roleQuery.currentServerId" placeholder="1001" /></el-form-item>
            </div>
            <el-form-item label="extension"><el-input v-model="roleQuery.extension" placeholder="可选扩展参数" /></el-form-item>
            <p class="simulator-help">open_id、role_id、original_server_id、current_server_id 至少填写一项。</p>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="区服查询" name="server_query">
          <el-form label-position="top" autocomplete="off" class="simulator-form" @submit.prevent="sendRequest">
            <div class="simulator-fields">
              <el-form-item label="original_server_id"><el-input v-model="serverQuery.originalServerId" placeholder="1001" /></el-form-item>
              <el-form-item label="current_server_id"><el-input v-model="serverQuery.currentServerId" placeholder="1001" /></el-form-item>
              <el-form-item label="page" required><el-input v-model="serverQuery.page" type="number" min="1" /></el-form-item>
              <el-form-item label="page_size"><el-input v-model="serverQuery.pageSize" type="number" min="1" max="1000" placeholder="不填写时默认 1000" /></el-form-item>
            </div>
            <el-form-item label="zonekey"><el-input v-model="serverQuery.zonekey" placeholder="可选区服标识" /></el-form-item>
            <p class="simulator-help">page 必须提交；page_size 不填写时不会参与签名，由中台使用默认值 1000。</p>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="角色封禁/解禁" name="role_control">
          <el-form label-position="top" autocomplete="off" class="simulator-form" @submit.prevent="sendRequest">
            <div class="simulator-fields">
              <el-form-item label="操作类型" required>
                <el-select v-model="roleControl.type" style="width: 100%">
                  <el-option label="封号（close）" value="close" />
                  <el-option label="解封（unClose）" value="unClose" />
                  <el-option label="禁言（lock）" value="lock" />
                  <el-option label="解禁言（unLock）" value="unLock" />
                </el-select>
              </el-form-item>
              <el-form-item label="server_id"><el-input v-model="roleControl.serverId" placeholder="1001" /></el-form-item>
              <el-form-item label="open_id"><el-input v-model="roleControl.openId" placeholder="user_10001" /></el-form-item>
              <el-form-item label="role_id"><el-input v-model="roleControl.roleId" placeholder="role_10001" /></el-form-item>
              <el-form-item label="role_name"><el-input v-model="roleControl.roleName" placeholder="玩家名称" /></el-form-item>
              <el-form-item label="ip"><el-input v-model="roleControl.ip" placeholder="192.0.2.10" /></el-form-item>
              <el-form-item label="duration（秒）">
                <el-input v-model="roleControl.duration" type="number" min="0" placeholder="封号/禁言必填" />
              </el-form-item>
            </div>
            <el-form-item label="describe"><el-input v-model="roleControl.describe" placeholder="操作原因，可选" /></el-form-item>
            <el-form-item label="extension"><el-input v-model="roleControl.extension" placeholder="扩展参数，可选" /></el-form-item>
            <p class="simulator-help">open_id、role_id、role_name、ip 至少填写一项；封号和禁言必须填写 duration。</p>
          </el-form>
        </el-tab-pane>

        <el-tab-pane label="平台充值回调" name="payment_callback">
          <el-form label-position="top" autocomplete="off" class="simulator-form" @submit.prevent="sendRequest">
            <div class="simulator-fields">
              <el-form-item label="平台订单号 order_no" required>
                <el-input v-model="paymentCallback.orderNo" placeholder="P202611120001" />
              </el-form-item>
              <el-form-item label="游戏订单号 game_order_id" required>
                <el-input v-model="paymentCallback.gameOrderId" placeholder="G202611120001" />
              </el-form-item>
              <el-form-item label="金额 amount（分）" required>
                <el-input v-model="paymentCallback.amount" type="number" min="1" />
              </el-form-item>
              <el-form-item label="支付时间 paid_at（RFC3339，可留空）">
                <el-input v-model="paymentCallback.paidAt" placeholder="留空使用当前 UTC 时间" />
              </el-form-item>
            </div>
            <el-form-item label="透传参数 attach">
              <el-input v-model="paymentCallback.attach" placeholder="必须与订单登记时完全一致，可为空" />
            </el-form-item>
            <el-checkbox v-model="allowPaymentCallback">
              我确认这是隔离测试订单，并了解回调可能触发游戏服真实发货
            </el-checkbox>
            <p class="simulator-help">必须先完成订单登记；order_no、game_order_id、amount、attach 必须与已登记订单匹配。</p>
            <p class="simulator-help">同一订单重试必须复用首次使用的 order_no；支付确认后不能更换平台订单号。</p>
            <p class="simulator-help">此接口不带环境路径，SDK Core 会根据 game_order 记录定位订单所属环境。</p>
            <p class="simulator-help">V1 签名仅覆盖 order_no、game_order_id、appSecret、timestamp、nonce；amount、attach、paid_at 不参与签名，后端仍会与已登记订单核对金额和透传参数。</p>
          </el-form>
        </el-tab-pane>
      </el-tabs>

      <div class="simulator-actions">
        <el-button :disabled="!canPreview || submitting" @click="previewRequest">预览请求</el-button>
        <el-button
          v-if="!(activeApi === 'role_control' && environment === 'prod')"
          type="primary"
          :loading="submitting"
          :disabled="!canSend"
          @click="sendRequest"
        >
          {{ activeApi === 'payment_callback' ? '确认并发送充值回调' : activeApi === 'role_control' ? '确认并发送操作' : '生成签名并发送' }}
        </el-button>
        <span class="simulator-help">每次发送都会重新生成时间戳、Nonce 和签名。</span>
      </div>
    </section>

    <section v-if="currentResult" class="simulator-panel simulator-result">
      <div class="simulator-result-heading">
        <h3>请求结果</h3>
        <div class="simulator-result-tags">
          <el-tag v-if="currentResult.preview" type="info">仅预览，未发送</el-tag>
          <el-tag v-else-if="currentResult.status !== null" :type="currentResult.status >= 200 && currentResult.status < 300 ? 'success' : 'danger'">
            HTTP {{ currentResult.status }}
          </el-tag>
          <el-tag v-else type="danger">网络错误</el-tag>
          <el-tag v-if="currentResult.acknowledged !== undefined" :type="currentResult.acknowledged ? 'success' : 'warning'">
            {{ currentResult.acknowledged ? '平台回调已确认' : '未确认' }}
          </el-tag>
          <span>{{ currentResult.durationMs }} ms</span>
        </div>
      </div>
      <p class="simulator-request-url">POST {{ currentResult.url }}</p>
      <el-alert v-if="currentResult.error" :title="currentResult.error" type="error" show-icon :closable="false" />
      <div class="simulator-result-label">
        <h4>请求 Header</h4>
        <el-button link size="small" @click="copyText(JSON.stringify(currentResult.headers, null, 2))">复制</el-button>
      </div>
      <pre>{{ JSON.stringify(currentResult.headers, null, 2) }}</pre>
      <div class="simulator-result-label">
        <h4>请求 Body</h4>
        <el-button link size="small" @click="copyText(prettyJSON(currentResult.requestBody))">复制</el-button>
      </div>
      <pre>{{ prettyJSON(currentResult.requestBody) }}</pre>
      <div class="simulator-result-label">
        <h4>响应 Body</h4>
        <el-button link size="small" :disabled="!currentResult.responseBody" @click="copyText(prettyJSON(currentResult.responseBody))">复制</el-button>
      </div>
      <pre>{{ currentResult.responseBody ? prettyJSON(currentResult.responseBody) : currentResult.preview ? '尚未发送' : '（空）' }}</pre>
      <p v-if="currentResult.acknowledged" class="simulator-help">
        SDK Core 已确认支付；游戏发货由后台 Worker 异步处理，此结果不代表游戏服已经发货。
      </p>
    </section>
  </main>
</template>

<style scoped>
.platform-simulator-page { max-width: 1120px; margin: 0 auto; padding: 24px; }
.simulator-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.simulator-heading h1 { font-size: 23px; margin: 3px 0 9px; }
.simulator-heading p { color: #8190a5; font-size: 13px; margin: 0; line-height: 1.6; }
.simulator-eyebrow { font-size: 10px; font-weight: 700; letter-spacing: 1.8px; color: #7186a3; }
.simulator-panel { background: white; border: 1px solid #e5ebf3; border-radius: 12px; padding: 22px; margin-bottom: 16px; box-shadow: 0 2px 12px rgba(30,50,80,.03); }
.simulator-common-fields { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; align-items: end; }
.simulator-common-fields :deep(.el-form-item) { margin-bottom: 10px; }
.simulator-common-form { margin-bottom: 0; }
.simulator-endpoints { display: flex; gap: 18px; flex-wrap: wrap; margin: 2px 0 14px; color: #8996a9; font-size: 12px; }
.simulator-alert { margin: 12px 0; }
.simulator-tabs :deep(.el-tabs__content) { padding-top: 12px; }
.simulator-form { max-width: 900px; }
.simulator-form :deep(.el-form-item) { margin-bottom: 15px; }
.simulator-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); column-gap: 16px; }
.simulator-help { display: block; color: #8996a9; font-size: 12px; line-height: 1.7; }
.simulator-actions { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-top: 16px; }
.simulator-result-heading, .simulator-result-tags { display: flex; align-items: center; gap: 10px; justify-content: space-between; flex-wrap: wrap; }
.simulator-result-heading h3 { font-size: 16px; margin: 0; }
.simulator-result-tags { justify-content: flex-end; color: #8793a6; font-size: 12px; }
.simulator-result-label { display: flex; align-items: center; justify-content: space-between; margin-top: 14px; }
.simulator-result h4 { font-size: 12px; margin: 0 0 6px; color: #43506a; }
.simulator-result pre { padding: 12px; border: 1px solid #ebeff6; background: #f7f9fc; border-radius: 7px; overflow-wrap: anywhere; white-space: pre-wrap; font-size: 12px; line-height: 1.6; }
.simulator-request-url { overflow-wrap: anywhere; color: #667791; font-size: 12px; }
@media (max-width: 760px) {
  .platform-simulator-page { padding: 12px; }
  .simulator-panel { padding: 14px; }
  .simulator-common-fields, .simulator-fields { grid-template-columns: 1fr; }
}
</style>
