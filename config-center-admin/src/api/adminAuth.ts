// 管理密钥只保存在当前页面内存，刷新或退出后重新输入。
import { ref } from 'vue'

let secret = ''
export const adminAuthenticated = ref(false)

export function getAdminSecret(): string { return secret }

export function setAdminSecret(value: string): void {
  secret = value
}

export function unlockAdmin(): void {
  adminAuthenticated.value = true
}

export function clearAdminSecret(): void {
  secret = ''
  adminAuthenticated.value = false
}
