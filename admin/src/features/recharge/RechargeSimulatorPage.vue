<script setup lang="ts">
import { ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { generateNonce, parseRegisterEndpoint, signRegistration } from './signature'

interface RequestResult {
  status: number | null
  durationMs: number
  url: string
  headers: Record<string, string>
  requestBody: string
  responseBody: string
  error?: string
}

const registerKey = 'middle-platform:recharge:register-url'

function restoreRegisterUrl(): string {
  try {
    const saved = localStorage.getItem(registerKey) ?? ''
    if (!saved) return ''
    const url = new URL(saved)
    url.pathname = url.pathname.replace(/^\/sdk(?=\/api\/core\/)/, '')
    url.pathname = url.pathname.replace(
      /\/api\/core\/(test|prod|review)\/([^/]+)\/orders$/,
      '/api/core/orders/$1/$2'
    )
    const migrated = url.toString()
    if (migrated !== saved) localStorage.setItem(registerKey, migrated)
    return migrated
  } catch {
    try { return localStorage.getItem(registerKey) ?? '' } catch { return '' }
  }
}

function saveRegisterUrl(value: string): void {
  try { localStorage.setItem(registerKey, value) } catch { /* 本地存储不可用时不影响使用 */ }
}

const registerUrl = ref(restoreRegisterUrl())
const gameOrderID = ref('')
const amount = ref('600')
const attach = ref('')
const gameSecret = ref('')
const registerSubmitting = ref(false)
const registerResult = ref<RequestResult | null>(null)

watch(registerUrl, saveRegisterUrl)

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
    const response = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...headers },
      body: requestBody,
      signal: controller.signal,
      credentials: 'omit'
    })
    return {
      status: response.status,
      url,
      headers,
      requestBody,
      responseBody: await response.text(),
      durationMs: Math.round(performance.now() - started)
    }
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error)
    return {
      status: null,
      url,
      headers,
      requestBody,
      responseBody: '',
      durationMs: Math.round(performance.now() - started),
      error: '请求失败：' + message + '（请检查接口可达性、CORS 和 HTTPS 混合内容限制）'
    }
  } finally {
    window.clearTimeout(timeout)
  }
}

async function register(): Promise<void> {
  if (registerSubmitting.value) return
  registerSubmitting.value = true
  registerResult.value = null

  try {
    const endpoint = parseRegisterEndpoint(registerUrl.value)
    if (endpoint.env === 'prod') {
      throw new Error('模拟工具不允许向 prod 环境登记，请使用 test 或 review')
    }

    const body = assertOrder()
    const rawBody = JSON.stringify(body)
    const timestamp = String(Math.floor(Date.now() / 1000))
    const nonce = generateNonce()
    const signature = await signRegistration(
      gameSecret.value, rawBody, endpoint.appid, timestamp, nonce
    )
    const headers = {
      'x-app-id': endpoint.appid,
      'x-timestamp': timestamp,
      'x-nonce': nonce,
      'x-signature': signature
    }
    const result = await request(endpoint.url, headers, rawBody)
    registerResult.value = result

    if (result.status === 200) {
      let succeeded = false
      try {
        const parsed: { code?: number; data?: { registration_status?: string } } = JSON.parse(result.responseBody)
        succeeded = parsed.code === 0 && parsed.data?.registration_status === 'REGISTERED'
      } catch { /* 响应格式异常时不能视为成功 */ }
      if (succeeded) ElMessage.success('订单登记成功')
      else ElMessage.warning('登记响应不符合预期，请查看返回内容')
    } else {
      ElMessage.error(result.error || '订单登记失败，请查看请求结果')
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '订单登记失败')
  } finally {
    registerSubmitting.value = false
  }
}

function prettyJSON(value: string): string {
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}
</script>

<template>
  <main class="recharge-page">
    <div class="recharge-heading">
      <div>
        <div class="recharge-eyebrow">ORDER REGISTRATION</div>
        <h1>订单登记模拟</h1>
        <p>模拟游戏服向 SDK Core 登记充值订单，自动生成游戏签名并发送真实请求。</p>
      </div>
      <el-tag type="warning" effect="plain">仅支持 test / review</el-tag>
    </div>
    <el-alert
      title="订单登记只创建待支付订单，不代表支付成功；正式服登记已在模拟页面中禁用。"
      type="info"
      show-icon
      :closable="false"
      class="recharge-warning"
    />
    <section class="recharge-panel">
      <el-form label-position="top" class="recharge-form" autocomplete="off" @submit.prevent="register">
        <el-form-item label="请求 URL（完整地址）" required>
          <el-input v-model="registerUrl" placeholder="https://example.com/api/core/orders/test/110006" clearable />
        </el-form-item>
        <div class="recharge-fields">
          <el-form-item label="游戏订单号" required>
            <el-input v-model="gameOrderID" placeholder="G202611120001" />
          </el-form-item>
          <el-form-item label="金额（分）" required>
            <el-input v-model="amount" type="number" min="1" placeholder="600" />
          </el-form-item>
        </div>
        <el-form-item label="透传参数 attach">
          <el-input v-model="attach" placeholder="roleId=10001&productId=gem60（可为空）" />
        </el-form-item>
        <el-form-item label="游戏密钥 gameSecret" required>
          <el-input
            v-model="gameSecret"
            type="password"
            show-password
            autocomplete="off"
            placeholder="填写与 game.config.gameSecret 相同的密钥"
          />
        </el-form-item>
        <div class="recharge-help">游戏密钥按原始 UTF-8 字符串使用，采用 SHA-1 排序签名。</div>
        <div class="recharge-actions">
          <el-button type="primary" native-type="submit" :loading="registerSubmitting">
            生成签名并发送登记
          </el-button>
          <span class="recharge-help">自动填写 x-app-id / x-timestamp / x-nonce / x-signature</span>
        </div>
      </el-form>
    </section>

    <section v-if="registerResult" class="recharge-panel recharge-result">
      <div class="recharge-result-heading">
        <h3>请求结果</h3>
        <div class="recharge-result-tags">
          <el-tag :type="registerResult.status === 200 ? 'success' : 'danger'">
            {{ registerResult.status ? 'HTTP ' + registerResult.status : '网络错误' }}
          </el-tag>
          <span>{{ registerResult.durationMs }} ms</span>
        </div>
      </div>
      <p class="recharge-request-url">POST {{ registerResult.url }}</p>
      <el-alert v-if="registerResult.error" :title="registerResult.error" type="error" show-icon :closable="false" />
      <h4>请求 Header（不包含密钥原文）</h4>
      <pre>{{ JSON.stringify(registerResult.headers, null, 2) }}</pre>
      <h4>请求 Body</h4>
      <pre>{{ prettyJSON(registerResult.requestBody) }}</pre>
      <h4>响应 Body</h4>
      <pre>{{ registerResult.responseBody ? prettyJSON(registerResult.responseBody) : '（空）' }}</pre>
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
@media (max-width: 700px) {
  .recharge-page { padding: 12px; }
  .recharge-panel { padding: 14px; }
  .recharge-fields { grid-template-columns: 1fr; }
}
</style>
