<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { sdkApi, apiErrorMessage } from '../../api/http'

const props = defineProps<{ gameId: number | null }>()
interface Order {
  id: string
  appid: string
  env: string
  gameOrderId: string
  platformOrderNo: string
  amount: number
  paymentStatus: number
  notifyStatus: number
  notifyAttempts: number
  createdAt: string
  paidAt: string | null
}
interface Detail extends Order {
  attach: string
  lastNotifyAt: string | null
  notifySuccessAt: string | null
  nextNotifyAt: string | null
  lastNotifyError: string
}
interface Stats {
  registered: number
  paid: number
  paidAmount: string
  pending: number
  failed: number
  paidNotified: number
}
const env = ref('prod')
const appid = ref('')
const orderType = ref('gameOrderId')
const orderNo = ref('')
const payment = ref('')
const notification = ref('')
const dates = ref<[string, string] | null>(null)
const items = ref<Order[]>([])
const stats = ref<Stats | null>(null)
const nextCursor = ref('')
const cursors = ref<string[]>([''])
const active = ref<Record<string, string | number>>({})
const loading = ref(false)
const statsLoading = ref(false)
const drawer = ref(false)
const detail = ref<Detail | null>(null)
const detailLoading = ref(false)
const retrying = ref(false)
let listSeq = 0
let statSeq = 0
let detailSeq = 0
let listController: AbortController | undefined
let statController: AbortController | undefined
let detailController: AbortController | undefined

const envLabel: Record<string, string> = { prod: '正式', test: '测试', review: '审核' }
const displayTime = (value?: string | null) => value ? new Intl.DateTimeFormat('zh-CN', {
  timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
  hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false
}).format(new Date(value)) : '—'
function money(value: string | number) {
  const cents = BigInt(value)
  return '¥' + (cents / 100n).toLocaleString('zh-CN') + '.' + (cents % 100n).toString().padStart(2, '0')
}
function notifyLabel(o: Order) {
  return o.paymentStatus === 0 ? '未触发' : o.notifyStatus === 1 ? '通知成功' : o.notifyStatus === 2 ? '通知失败' : '待通知'
}
function canRetry(o: Detail) {
  return o.paymentStatus === 1 && o.notifyStatus === 2 && !!o.nextNotifyAt
}
const cards = computed(() => [
  { label: '今日登记订单', value: stats.value?.registered ?? '—' },
  { label: '今日支付订单', value: stats.value?.paid ?? '—' },
  { label: '今日支付金额', value: stats.value ? money(stats.value.paidAmount) : '—' },
  { label: '已支付待通知', value: stats.value?.pending ?? '—' },
  { label: '通知失败（含历史）', value: stats.value?.failed ?? '—' },
  { label: '今日支付订单通知成功率', value: stats.value?.paid ? (stats.value.paidNotified / stats.value.paid * 100).toFixed(2) + '%' : '—' }
])

function filters() {
  const params: Record<string, string | number> = { gameId: props.gameId ?? 0, pageSize: 20 }
  if (env.value) params.env = env.value
  if (appid.value.trim()) params.appid = appid.value.trim()
  if (orderNo.value.trim()) params[orderType.value] = orderNo.value.trim()
  if (payment.value !== '') params.paymentStatus = payment.value
  if (notification.value !== '') params.notifyStatus = notification.value
  if (dates.value?.[0] && dates.value?.[1]) {
    params.startTime = new Date(dates.value[0] + 'T00:00:00+08:00').toISOString()
    params.endTime = new Date(new Date(dates.value[1] + 'T00:00:00+08:00').getTime() + 86400000).toISOString()
  }
  return params
}
async function loadList(cursor = '') {
  listController?.abort()
  const seq = ++listSeq
  nextCursor.value = ''
  if (!props.gameId) { items.value = []; loading.value = false; return false }
  const controller = new AbortController()
  listController = controller
  loading.value = true
  try {
    const { data } = await sdkApi.get('/admin/orders', { params: { ...active.value, ...(cursor ? { cursor } : {}) }, signal: controller.signal })
    if (seq !== listSeq) return false
    items.value = data.data.items
    nextCursor.value = data.data.nextCursor
    return true
  } catch (e) {
    if (seq === listSeq) {
      items.value = []
      ElMessage.error(apiErrorMessage(e, '查询订单失败'))
    }
    return false
  } finally {
    if (seq === listSeq) loading.value = false
  }
}
async function loadStats() {
  statController?.abort()
  const seq = ++statSeq
  stats.value = null
  if (!props.gameId) { statsLoading.value = false; return }
  const controller = new AbortController()
  statController = controller
  statsLoading.value = true
  const { gameId, env, appid } = active.value
  try {
    const { data } = await sdkApi.get('/admin/orders/stats', { params: { gameId, env, appid }, signal: controller.signal })
    if (seq === statSeq) stats.value = data.data
  } catch (e) {
    if (seq === statSeq) ElMessage.warning(apiErrorMessage(e, '订单统计暂不可用'))
  } finally {
    if (seq === statSeq) statsLoading.value = false
  }
}
function search() {
  active.value = filters()
  cursors.value = ['']
  void loadList()
  void loadStats()
}
function reset() {
  env.value = 'prod'
  appid.value = ''
  orderNo.value = ''
  orderType.value = 'gameOrderId'
  payment.value = ''
  notification.value = ''
  dates.value = null
  search()
}
async function next() {
  const c = nextCursor.value
  if (c && await loadList(c)) cursors.value.push(c)
}
async function previous() {
  const index = cursors.value.length - 2
  if (index >= 0 && await loadList(cursors.value[index] ?? '')) cursors.value.pop()
}
async function openDetail(row: Order) {
  detailController?.abort()
  const seq = ++detailSeq
  const gameId = props.gameId
  detail.value = null
  drawer.value = true
  detailLoading.value = true
  const controller = new AbortController()
  detailController = controller
  try {
    const { data } = await sdkApi.get('/admin/orders/' + encodeURIComponent(row.id), { params: { gameId }, signal: controller.signal })
    if (seq === detailSeq && gameId === props.gameId) detail.value = data.data
  } catch (e) {
    if (seq === detailSeq) ElMessage.error(apiErrorMessage(e, '查询订单详情失败'))
  } finally {
    if (seq === detailSeq) detailLoading.value = false
  }
}
async function retry() {
  const row = detail.value
  const gameId = props.gameId
  if (!row || !canRetry(row) || retrying.value) return
  try {
    await ElMessageBox.confirm('将提前安排现有发货任务，不会直接发货。租约保护期内可能被拒绝。', '确认重试发货通知', { type: 'warning' })
  } catch { return }
  if (gameId !== props.gameId || detail.value?.id !== row.id || retrying.value) return
  retrying.value = true
  try {
    await sdkApi.post('/admin/orders/' + encodeURIComponent(row.id) + '/retry', undefined, { params: { gameId } })
    ElMessage.success('已安排发货补偿')
    if (props.gameId === gameId && drawer.value && detail.value?.id === row.id) {
      void openDetail(row)
      void loadList(cursors.value[cursors.value.length - 1] ?? '')
    }
  } catch (e) {
    ElMessage.warning(apiErrorMessage(e, '安排补偿失败'))
  } finally {
    retrying.value = false
  }
}
function closeDetail() {
  ++detailSeq
  detailController?.abort()
  drawer.value = false
  detail.value = null
}
watch(() => props.gameId, () => { closeDetail(); reset() }, { immediate: true })
onBeforeUnmount(() => { ++listSeq; ++statSeq; ++detailSeq; listController?.abort(); statController?.abort(); detailController?.abort() })
</script>

<template>
  <main class="content-panel order-page">
    <h2>订单管理</h2>
    <p class="order-hint">当前游戏的充值订单与发货通知状态。时间统一显示北京时间，统计按渠道、环境及今日支付时间计算。</p>
    <div class="order-stats" v-loading="statsLoading">
      <div v-for="card in cards" :key="card.label" class="order-stat"><span>{{ card.label }}</span><strong>{{ card.value }}</strong></div>
    </div>
    <el-form inline @submit.prevent="search">
      <el-form-item label="环境">
        <el-select v-model="env" style="width: 110px">
          <el-option label="全部" value="" /><el-option label="正式" value="prod" />
          <el-option label="测试" value="test" /><el-option label="审核" value="review" />
        </el-select>
      </el-form-item>
      <el-form-item label="渠道"><el-input v-model="appid" placeholder="精确 AppID" clearable maxlength="128" style="width: 160px" /></el-form-item>
      <el-form-item label="支付">
        <el-select v-model="payment" style="width: 110px">
          <el-option label="全部" value="" /><el-option label="未支付" value="0" /><el-option label="已支付" value="1" />
        </el-select>
      </el-form-item>
      <el-form-item label="通知">
        <el-select v-model="notification" style="width: 120px">
          <el-option label="全部" value="" /><el-option label="待通知" value="0" /><el-option label="成功" value="1" /><el-option label="失败" value="2" />
        </el-select>
      </el-form-item>
      <el-form-item label="创建日期">
        <el-date-picker v-model="dates" type="daterange" value-format="YYYY-MM-DD" start-placeholder="开始日期" end-placeholder="结束日期" />
      </el-form-item>
      <el-form-item>
        <el-input v-model="orderNo" placeholder="精确订单号" clearable maxlength="128" style="width: 320px">
          <template #prepend><el-select v-model="orderType" style="width: 125px">
            <el-option label="游戏订单号" value="gameOrderId" /><el-option label="平台订单号" value="platformOrderNo" />
          </el-select></template>
        </el-input>
      </el-form-item>
      <el-form-item>
        <el-button type="primary" native-type="submit" :loading="loading" :disabled="!gameId">查询</el-button>
        <el-button @click="reset">重置</el-button>
      </el-form-item>
    </el-form>
    <p class="order-hint">日期未选时默认查询今日，精确订单号可跨日期查询。普通查询最多 30 天；统计不受订单号、状态及创建日期筛选影响，最多缓存 60 秒。</p>
    <el-table :data="items" v-loading="loading" row-key="id" stripe>
      <el-table-column prop="gameOrderId" label="游戏订单号" min-width="175" show-overflow-tooltip />
      <el-table-column prop="platformOrderNo" label="平台订单号" min-width="175" show-overflow-tooltip />
      <el-table-column prop="appid" label="AppID" min-width="130" show-overflow-tooltip />
      <el-table-column label="环境" width="75"><template #default="{ row }">{{ envLabel[row.env] ?? row.env }}</template></el-table-column>
      <el-table-column label="金额" min-width="100"><template #default="{ row }">{{ money(row.amount) }}</template></el-table-column>
      <el-table-column label="支付" width="90"><template #default="{ row }"><el-tag :type="row.paymentStatus === 1 ? 'success' : 'info'">{{ row.paymentStatus === 1 ? '已支付' : '未支付' }}</el-tag></template></el-table-column>
      <el-table-column label="发货通知" width="115"><template #default="{ row }"><el-tag :type="row.paymentStatus === 0 ? 'info' : row.notifyStatus === 1 ? 'success' : row.notifyStatus === 2 ? 'danger' : 'warning'">{{ notifyLabel(row) }}</el-tag></template></el-table-column>
      <el-table-column prop="notifyAttempts" label="领取次数" width="95" />
      <el-table-column label="创建时间" min-width="175"><template #default="{ row }">{{ displayTime(row.createdAt) }}</template></el-table-column>
      <el-table-column label="支付时间" min-width="175"><template #default="{ row }">{{ displayTime(row.paidAt) }}</template></el-table-column>
      <el-table-column label="操作" width="85" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="openDetail(row)">详情</el-button></template></el-table-column>
    </el-table>
    <div class="order-pagination">
      <el-button :disabled="cursors.length <= 1 || loading" @click="previous">上一页</el-button>
      <span>第 {{ cursors.length }} 页</span>
      <el-button :disabled="!nextCursor || loading" @click="next">下一页</el-button>
    </div>
    <el-drawer v-model="drawer" title="订单详情" size="min(680px, 100%)" @close="closeDetail">
      <div v-loading="detailLoading">
        <el-descriptions v-if="detail" border :column="1">
          <el-descriptions-item label="游戏订单号">{{ detail.gameOrderId }}</el-descriptions-item>
          <el-descriptions-item label="平台订单号">{{ detail.platformOrderNo || '—' }}</el-descriptions-item>
          <el-descriptions-item label="渠道 / 环境">{{ detail.appid }} / {{ envLabel[detail.env] }}</el-descriptions-item>
          <el-descriptions-item label="金额">{{ money(detail.amount) }}</el-descriptions-item>
          <el-descriptions-item label="支付状态">{{ detail.paymentStatus === 1 ? '已支付' : '未支付' }}</el-descriptions-item>
          <el-descriptions-item label="发货通知">{{ notifyLabel(detail) }}</el-descriptions-item>
          <el-descriptions-item label="任务领取次数">{{ detail.notifyAttempts }}</el-descriptions-item>
          <el-descriptions-item label="创建时间">{{ displayTime(detail.createdAt) }}</el-descriptions-item>
          <el-descriptions-item label="支付时间">{{ displayTime(detail.paidAt) }}</el-descriptions-item>
          <el-descriptions-item label="最近领取时间">{{ displayTime(detail.lastNotifyAt) }}</el-descriptions-item>
          <el-descriptions-item label="通知成功时间">{{ displayTime(detail.notifySuccessAt) }}</el-descriptions-item>
          <el-descriptions-item label="下次调度 / 租约时间">{{ displayTime(detail.nextNotifyAt) }}</el-descriptions-item>
          <el-descriptions-item label="最近通知错误">{{ detail.lastNotifyError || '—' }}</el-descriptions-item>
          <el-descriptions-item label="透传参数"><pre class="order-attach">{{ detail.attach }}</pre></el-descriptions-item>
        </el-descriptions>
        <el-button v-if="detail && canRetry(detail)" class="order-retry" type="warning" :loading="retrying" @click="retry">重试发货通知</el-button>
      </div>
    </el-drawer>
  </main>
</template>

<style scoped>
.order-hint { color: var(--el-text-color-secondary); font-size: 13px; }
.order-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(145px, 1fr)); gap: 12px; margin: 16px 0; }
.order-stat { display: flex; flex-direction: column; gap: 8px; border: 1px solid var(--el-border-color-light); border-radius: 8px; padding: 14px; }
.order-stat span { font-size: 13px; color: var(--el-text-color-secondary); }
.order-stat strong { font-size: 21px; overflow-wrap: anywhere; }
.order-pagination { display: flex; align-items: center; justify-content: flex-end; gap: 12px; margin-top: 16px; }
.order-attach { white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; }
.order-retry { margin-top: 20px; }
</style>
