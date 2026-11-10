<script setup lang="ts">
// 回调订阅者管理页：维护接入信息、启停状态和投递进度。
import { onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  createSubscriber,
  deleteSubscriber,
  listSubscribers,
  setSubscriberEnabled,
  updateSubscriber
} from '../../api/subscriberApi'
import { apiErrorMessage } from '../../api/http'
import type { Subscriber } from '../../api/subscriberApi'

const subscribers = ref<Subscriber[]>([])
const loadingSubscribers = ref(false)
const showSubscriber = ref(false)
const showEditSubscriber = ref(false)
const creatingSubscriber = ref(false)
const savingSubscriber = ref(false)
const togglingSubscriberId = ref<number | null>(null)
const deletingSubscriberId = ref<number | null>(null)
const newSubscriber = reactive({ code: '', callbackUrl: '', secret: '' })
const editForm = reactive({ id: 0, code: '', callbackUrl: '', secret: '' })

function isValidSubscriberSecret(secret: string): boolean {
  const length = new TextEncoder().encode(secret).length
  return length >= 32 && length <= 255 && secret === secret.trim()
}

async function loadSubscribers() {
  loadingSubscribers.value = true
  try {
    subscribers.value = (await listSubscribers()).data
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '读取接入平台失败'))
  } finally {
    loadingSubscribers.value = false
  }
}

async function addSubscriber() {
  if (!newSubscriber.code.trim() || !newSubscriber.callbackUrl.trim() || !newSubscriber.secret.trim()) {
    ElMessage.warning('请填写接入方编码、回调地址和签名密钥')
    return
  }
  if (newSubscriber.code !== newSubscriber.code.trim() || !isValidSubscriberSecret(newSubscriber.secret)) {
    ElMessage.warning('接入方编码不能包含首尾空格，签名密钥须为 32–255 字节且不能包含首尾空格')
    return
  }
  creatingSubscriber.value = true
  try {
    await createSubscriber(newSubscriber)
    showSubscriber.value = false
    Object.assign(newSubscriber, { code: '', callbackUrl: '', secret: '' })
    await loadSubscribers()
    ElMessage.success('接入平台创建成功')
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '创建接入平台失败'))
  } finally {
    creatingSubscriber.value = false
  }
}

function openEditSubscriber(row: Subscriber) {
  Object.assign(editForm, { id: row.id, code: row.code, callbackUrl: row.callbackUrl, secret: '' })
  showEditSubscriber.value = true
}

async function saveSubscriber() {
  if (!editForm.callbackUrl.trim()) {
    ElMessage.warning('请填写 Webhook 回调地址')
    return
  }
  if (editForm.secret && (!isValidSubscriberSecret(editForm.secret))) {
    ElMessage.warning('签名密钥须为 32–255 字节且不能包含首尾空格')
    return
  }
  savingSubscriber.value = true
  try {
    await updateSubscriber(editForm.id, {
      callbackUrl: editForm.callbackUrl,
      secret: editForm.secret
    })
    showEditSubscriber.value = false
    Object.assign(editForm, { id: 0, code: '', callbackUrl: '', secret: '' })
    await loadSubscribers()
    ElMessage.success('订阅信息已更新')
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '更新订阅失败'))
  } finally {
    savingSubscriber.value = false
  }
}

async function toggleSubscriber(row: Subscriber) {
  const enabled = !row.enabled
  if (!enabled) {
    try {
      await ElMessageBox.confirm(
        `停用「${row.code}」后，配置中心将停止新的 Webhook 通知，并拒绝该接入端的配置同步请求。已经领取的投递任务可能继续完成。`,
        '确认停用',
        { type: 'warning', confirmButtonText: '停用', cancelButtonText: '取消' }
      )
    } catch {
      return
    }
  }
  togglingSubscriberId.value = row.id
  try {
    await setSubscriberEnabled(row.id, enabled)
    await loadSubscribers()
    ElMessage.success(enabled ? '订阅已启用' : '订阅已停用')
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '修改订阅状态失败'))
  } finally {
    togglingSubscriberId.value = null
  }
}

async function removeSubscriber(row: Subscriber) {
  try {
    await ElMessageBox.confirm(
      `确定删除接入方「${row.code}」？删除后无法恢复，其投递进度也会一并清理。`,
      '删除同步订阅',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
  } catch {
    return
  }
  deletingSubscriberId.value = row.id
  try {
    await deleteSubscriber(row.id)
    await loadSubscribers()
    ElMessage.success('订阅已删除')
  } catch (error) {
    ElMessage.error(apiErrorMessage(error, '删除订阅失败'))
  } finally {
    deletingSubscriberId.value = null
  }
}

onMounted(() => void loadSubscribers())
</script>

<template>
  <main class="content-panel">
    <div class="panel-title">
      <div>
        <h2>接入平台与同步状态</h2>
        <p class="muted">每个接入方使用独立密钥，统一认证 Webhook 通知、配置拉取和本地只读查询。</p>
      </div>
      <div class="panel-actions">
        <el-button @click="loadSubscribers">刷新</el-button>
        <el-button type="primary" @click="showSubscriber = true">新增接入平台</el-button>
      </div>
    </div>

    <el-table v-loading="loadingSubscribers" :data="subscribers" stripe>
      <el-table-column prop="code" label="接入方" min-width="130" />
      <el-table-column prop="callbackUrl" label="回调 URL" min-width="240" show-overflow-tooltip />
      <el-table-column prop="acknowledgedRevision" label="已确认版本" width="110" />
      <el-table-column prop="failures" label="连续失败" width="90" />
      <el-table-column label="状态" width="95">
        <template #default="{ row }">
          <el-tag :type="!row.enabled ? 'info' : row.failures ? 'danger' : 'success'">
            {{ !row.enabled ? '已停用' : row.failures ? '异常' : '正常' }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="lastError" label="最近错误" min-width="150" show-overflow-tooltip />
      <el-table-column label="操作" width="190" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="openEditSubscriber(row)">编辑</el-button>
          <el-button
            link
            :type="row.enabled ? 'warning' : 'success'"
            size="small"
            :loading="togglingSubscriberId === row.id"
            @click="toggleSubscriber(row)"
          >
            {{ row.enabled ? '停用' : '启用' }}
          </el-button>
          <el-button
            link
            type="danger"
            size="small"
            :loading="deletingSubscriberId === row.id"
            @click="removeSubscriber(row)"
          >
            删除
          </el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="showSubscriber" title="新增接入平台" width="530px">
      <el-form label-position="top" class="admin-config-form" autocomplete="off">
        <el-form-item label="唯一编码">
          <el-input v-model="newSubscriber.code" autocomplete="off" />
        </el-form-item>
        <el-form-item label="Webhook 回调地址">
          <el-input v-model="newSubscriber.callbackUrl" autocomplete="off" />
        </el-form-item>
        <el-form-item label="接入端签名密钥（32–255 字节）">
          <el-input v-model="newSubscriber.secret" type="password" show-password autocomplete="new-password" />
        </el-form-item>
        <p class="muted">签名密钥必须在所有接入端之间保持唯一；同一接入端的配置拉取、本地查询和 Webhook 共用此密钥。服务端会校验回调地址安全性。</p>
      </el-form>
      <template #footer>
        <el-button @click="showSubscriber = false">取消</el-button>
        <el-button type="primary" :loading="creatingSubscriber" @click="addSubscriber">创建</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="showEditSubscriber" title="编辑同步订阅" width="530px">
      <el-form label-position="top" class="admin-config-form" autocomplete="off">
        <el-form-item label="唯一编码">
          <el-input v-model="editForm.code" disabled />
        </el-form-item>
        <el-form-item label="Webhook 回调地址">
          <el-input v-model="editForm.callbackUrl" autocomplete="off" />
        </el-form-item>
        <el-form-item label="更新接入端签名密钥">
          <el-input
            v-model="editForm.secret"
            type="password"
            show-password
            autocomplete="new-password"
            placeholder="留空表示不修改当前密钥"
          />
        </el-form-item>
        <p class="muted">接入方编码创建后不可修改；密钥不会回显；每个订阅者的密钥必须唯一，留空即可保留原密钥。轮换密钥后，必须同步更新对应接入端的 subscriber.secret 并重启接入端。</p>
      </el-form>
      <template #footer>
        <el-button @click="showEditSubscriber = false">取消</el-button>
        <el-button type="primary" :loading="savingSubscriber" @click="saveSubscriber">保存</el-button>
      </template>
    </el-dialog>
  </main>
</template>
