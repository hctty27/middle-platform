<script setup lang="ts">
// 本页只负责构造签名并发送真实请求，不把支付回调成功误认为游戏发货成功。
import { ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { generateNonce, parseCallbackEndpoint, parseRegisterEndpoint,
  signPlatformV1, signRegistration } from './signature'

type Tab = 'register' | 'callback'
interface RequestResult {
  status: number | null
  durationMs: number
  url: string
  headers: Record<string, string>
  requestBody: string
  responseBody: string
  error?: string
  acknowledged?: boolean
}
const registerKey = 'middle-platform:recharge:register-url'
const callbackKey = 'middle-platform:recharge:callback-url'
function restoreUrl(key: string): string {
  try { return localStorage.getItem(key) ?? '' } catch { return '' }
}
function saveUrl(key: string, value: string) {
  try { localStorage.setItem(key, value) } catch { /* 禁止持久化时不影响使用 */ }
}

const activeTab = ref<Tab>('register')
const registerUrl = ref(restoreUrl(registerKey))
const callbackUrl = ref(restoreUrl(callbackKey))
const gameOrderID = ref('')
const amount = ref('600')
const attach = ref('')
const gameSecret = ref('')
const platformOrderNo = ref('')
const paidAt = ref('')
const appSecret = ref('')
const allowCallback = ref(false)
const registerSubmitting = ref(false)
const callbackSubmitting = ref(false)
const registerResult = ref<RequestResult | null>(null)
const callbackResult = ref<RequestResult | null>(null)
watch(registerUrl, url => saveUrl(registerKey, url))
watch(callbackUrl, url => saveUrl(callbackKey, url))

function assertOrder(): { game_order_id: string; amount: number; attach: string } {
  const id = gameOrderID.value.trim()
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(id)) throw new Error('游戏订单号格式无效')
  const value = Number(amount.value)
  if (!Number.isSafeInteger(value) || value <= 0) throw new Error('金额须为大于 0 的安全整数（单位：分）')
  if (new TextEncoder().encode(attach.value).byteLength > 4096) throw new Error('attach 超过 4096 字节')
  return { game_order_id: id, amount: value, attach: attach.value }
}

async function request(url: string, headers: Record<string, string>, requestBody: string): Promise<RequestResult> {
  const started = performance.now()
  const controller = new AbortController()
  const timeout = window.setTimeout(() => controller.abort(), 15000)
  try {
    const response = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json', ...headers },
      body: requestBody, signal: controller.signal, credentials: 'omit' })
    return { status: response.status, url, headers, requestBody, responseBody: await response.text(),
      durationMs: Math.round(performance.now() - started) }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    return { status: null, url, headers, requestBody, responseBody: '',
      durationMs: Math.round(performance.now() - started),
      error: `请求失败：${message}（请检查接口可达性、CORS 和 HTTPS 混合内容限制）` }
  } finally { window.clearTimeout(timeout) }
}

async function register() {
  if (registerSubmitting.value) return
  registerSubmitting.value = true
  registerResult.value = null
  try {
    const endpoint = parseRegisterEndpoint(registerUrl.value)
    if (endpoint.env === 'prod') throw new Error('模拟工具不允许向 prod 环境登记，请使用 test 或 review')
    const body = assertOrder()
    const rawBody = JSON.stringify(body)
    const timestamp = String(Math.floor(Date.now() / 1000))
    const nonce = generateNonce()
    const signature = await signRegistration(gameSecret.value, rawBody, endpoint.env ?? '', endpoint.appid, timestamp, nonce)
    const headers = { 'x-app-id': endpoint.appid, 'x-env': endpoint.env ?? '', 'x-timestamp': timestamp,
      'x-nonce': nonce, 'x-signature': signature }
    const result = await request(endpoint.url, headers, rawBody)
    registerResult.value = result
    if (result.status === 200) {
      let succeeded = false
      try {
        const parsed: { code?: number; data?: { registration_status?: string } } = JSON.parse(result.responseBody)
        succeeded = parsed.code === 0 && parsed.data?.registration_status === 'REGISTERED'
      } catch { /* 业务响应格式异常时不能视为成功 */ }
      if (succeeded) {
        if (!platformOrderNo.value) platformOrderNo.value = `P${Date.now()}`
        if (!callbackUrl.value) {
          const suggested = new URL(endpoint.url)
          suggested.pathname = suggested.pathname.replace(/\/(test|prod|review)\/([^/]+)\/orders$/, '/$2/pay/notify')
          callbackUrl.value = suggested.toString()
        }
        ElMessage.success('订单登记成功，已填入回调参数')
      } else ElMessage.warning('登记响应不符合预期，请查看返回内容')
    } else ElMessage.error('订单登记失败，请查看请求结果')
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : String(error)) }
  finally { registerSubmitting.value = false }
}

async function callback() {
  if (callbackSubmitting.value) return
  callbackSubmitting.value = true
  callbackResult.value = null
  try {
    if (!allowCallback.value) throw new Error('请先确认目标是隔离测试环境，并了解回调会触发真实发货流程')
    const endpoint = parseCallbackEndpoint(callbackUrl.value)
    const id = platformOrderNo.value.trim()
    if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(id)) throw new Error('平台订单号格式无效')
    const order = assertOrder()
    const timestamp = String(Math.floor(Date.now() / 1000))
    const nonce = generateNonce()
    const signature = await signPlatformV1(appSecret.value, id, order.game_order_id, nonce, timestamp)
    const body = JSON.stringify({ order_no: id, ...order, paid_at: paidAt.value.trim() || new Date().toISOString() })
    const headers = { 'x-timestamp': timestamp, 'x-nonce': nonce, 'x-signature': signature }
    const result = await request(endpoint.url, headers, body)
    result.acknowledged = result.status === 200 && result.responseBody === nonce
    callbackResult.value = result
    if (result.acknowledged) ElMessage.success('支付回调已确认；游戏发货由 Worker 异步处理')
    else ElMessage.error('支付回调未被正确确认，请查看请求结果')
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : String(error)) }
  finally { callbackSubmitting.value = false }
}

async function confirmCallback() {
  try {
    await ElMessageBox.confirm('此操作将调用真实支付回调接口，可能触发游戏服发货。请确认仅使用隔离测试订单。',
      '确认模拟回调', { type: 'warning', confirmButtonText: '确认发送', cancelButtonText: '取消' })
  } catch { return }
  await callback()
}
</script>

<template>
  <main class="recharge-page">
    <div class="recharge-heading">
      <div>
        <div class="recharge-eyebrow">RECHARGE TESTING</div>
        <h1>充值模拟</h1>
        <p>分别填写完整的订单登记和支付回调 URL，自动生成签名并发送真实请求。</p>
      </div>
      <el-tag type="warning" effect="plain">仅用于联调</el-tag>
    </div>
    <el-alert title="回调确认支付后，会由后台 Worker 异步通知游戏服，可能触发真实发货；请只使用隔离测试订单。"
      type="warning" show-icon :closable="false" class="recharge-warning" />
    <section class="recharge-panel">
      <el-tabs v-model="activeTab">
        <el-tab-pane label="订单登记" name="register">
          <el-form label-position="top" class="recharge-form" @submit.prevent="register">
            <el-form-item label="请求 URL（完整地址）" required>
              <el-input v-model="registerUrl" placeholder="https://example.com/sdk/api/core/test/110006/orders" clearable />
            </el-form-item>
            <div class="recharge-fields">
              <el-form-item label="游戏订单号" required>
                <el-input v-model="gameOrderID" placeholder="G202611030001" />
              </el-form-item>
              <el-form-item label="金额（分）" required>
                <el-input v-model="amount" type="number" min="1" placeholder="600" />
              </el-form-item>
            </div>
            <el-form-item label="透传参数 attach">
              <el-input v-model="attach" placeholder="roleId=10001&productId=gem60（可为空）" />
            </el-form-item>
            <el-form-item label="游戏密钥 gameSecret" required>
              <el-input v-model="gameSecret" type="password" show-password autocomplete="off" placeholder="填写与 game.config.gameSecret 相同的密钥（支持任意非空字符串）" />
            </el-form-item>
            <div class="recharge-help">密钥使用原始 UTF-8 字符串；签名按字段名排序后拼接 MD5 大写。</div>
            <div class="recharge-actions">
              <el-button type="primary" native-type="submit" :loading="registerSubmitting">生成签名并发送登记</el-button>
              <span class="recharge-help">自动填写 x-app-id / x-env / x-timestamp / x-nonce / x-signature</span>
            </div>
          </el-form>
        </el-tab-pane>
        <el-tab-pane label="支付回调" name="callback">
          <el-form label-position="top" class="recharge-form" @submit.prevent="confirmCallback">
            <el-form-item label="请求 URL（完整地址）" required>
              <el-input v-model="callbackUrl" placeholder="https://example.com/sdk/api/core/110006/pay/notify" clearable />
            </el-form-item>
            <div class="recharge-fields">
              <el-form-item label="平台订单号" required>
                <el-input v-model="platformOrderNo" placeholder="P202611030001" />
              </el-form-item>
              <el-form-item label="游戏订单号" required>
                <el-input v-model="gameOrderID" placeholder="G202611030001" />
              </el-form-item>
              <el-form-item label="金额（分）" required>
                <el-input v-model="amount" type="number" min="1" />
              </el-form-item>
              <el-form-item label="支付时间（RFC3339，可留空）">
                <el-input v-model="paidAt" placeholder="留空使用当前 UTC 时间" />
              </el-form-item>
            </div>
            <el-form-item label="透传参数 attach">
              <el-input v-model="attach" placeholder="需与登记时完全一致" />
            </el-form-item>
            <el-form-item label="平台密钥 appSecret" required>
              <el-input v-model="appSecret" type="password" show-password autocomplete="off" placeholder="channel_app.config.appSecret" />
            </el-form-item>
            <el-checkbox v-model="allowCallback" label="确认仅对隔离测试订单操作，了解可能触发游戏服真实发货" />
            <div class="recharge-actions">
              <el-button type="primary" native-type="submit" :loading="callbackSubmitting" :disabled="!allowCallback">生成签名并模拟回调</el-button>
              <span class="recharge-help">自动填写 x-timestamp / x-nonce / x-signature</span>
            </div>
          </el-form>
        </el-tab-pane>
      </el-tabs>
    </section>
    <section v-if="(activeTab === 'register' ? registerResult : callbackResult)" class="recharge-panel recharge-result">
      <template v-for="result in [activeTab === 'register' ? registerResult : callbackResult]" :key="result?.url">
        <div v-if="result" class="recharge-result-heading">
          <h3>请求结果</h3>
          <div class="recharge-result-tags">
            <el-tag :type="result.status === 200 ? 'success' : 'danger'">{{ result.status ? `HTTP ${result.status}` : '网络错误' }}</el-tag>
            <el-tag v-if="activeTab === 'callback'" :type="result.acknowledged ? 'success' : 'warning'">
              {{ result.acknowledged ? '平台回调已确认' : '未确认' }}
            </el-tag>
            <span>{{ result.durationMs }} ms</span>
          </div>
        </div>
        <template v-if="result">
          <p class="recharge-request-url">POST {{ result.url }}</p>
          <el-alert v-if="result.error" :title="result.error" type="error" show-icon :closable="false" />
          <h4>请求 Header（已排除密钥原文）</h4>
          <pre>{{ JSON.stringify(result.headers, null, 2) }}</pre>
          <h4>请求 Body</h4>
          <pre>{{ result.requestBody }}</pre>
          <h4>响应 Body</h4>
          <pre>{{ result.responseBody || '（空）' }}</pre>
          <p v-if="activeTab === 'callback' && result.acknowledged" class="recharge-help">
            SDK Core 已确认支付；发货通知仍由异步 Worker 处理，此结果不代表游戏服已经发货。
          </p>
        </template>
      </template>
    </section>
  </main>
</template>

<style scoped>
.recharge-page { max-width: 1080px; margin: 0 auto; padding: 24px; }
.recharge-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 16px; }
.recharge-heading h1 { font-size: 23px; margin: 3px 0 9px; }
.recharge-heading p { color: #8190a5; font-size: 13px; margin: 0; line-height: 1.6; }
.recharge-eyebrow { font-size: 10px; font-weight: 700; letter-spacing: 1.8px; color: #7186a3; }
.recharge-warning { margin-bottom: 16px; }
.recharge-panel { background: white; border: 1px solid #e5ebf3; border-radius: 12px; padding: 22px; margin-bottom: 16px; box-shadow: 0 2px 12px rgba(30,50,80,.03); }
.recharge-panel :deep(.el-tabs__content) { padding-top: 10px; }
.recharge-form { max-width: 840px; }
.recharge-form :deep(.el-form-item) { margin-bottom: 17px; }
.recharge-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); column-gap: 16px; }
.recharge-help { display: block; font-size: 12px; line-height: 1.7; color: #8996a9; }
.recharge-actions { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-top: 16px; }
.recharge-result-heading, .recharge-result-tags { display: flex; align-items: center; gap: 10px; justify-content: space-between; flex-wrap: wrap; }
.recharge-result-heading h3 { font-size: 16px; margin: 0; }
.recharge-result-tags { justify-content: flex-end; font-size: 12px; color: #8793a6; }
.recharge-result h4 { font-size: 12px; margin: 14px 0 6px; color: #43506a; }
.recharge-result pre { padding: 12px; border: 1px solid #ebeff6; background: #f7f9fc; border-radius: 7px; overflow-wrap: anywhere; white-space: pre-wrap; font-size: 12px; line-height: 1.6; }
.recharge-request-url { overflow-wrap: anywhere; color: #667791; font-size: 12px; }
@media (max-width: 700px) { .recharge-page { padding: 12px; } .recharge-panel { padding: 14px; } .recharge-fields { grid-template-columns: 1fr; } }
</style>
