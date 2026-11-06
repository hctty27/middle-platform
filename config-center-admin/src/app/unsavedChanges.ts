import { ElMessageBox } from 'element-plus'

// 各配置编辑器统一注册未保存状态，供页面导航、切换游戏和刷新保护使用。
const checkers = new Map<symbol, { check: () => boolean; discard?: () => void }>()

export function registerUnsavedChecker(checker: () => boolean, discard?: () => void): () => void {
  const id = Symbol('unsaved-config')
  checkers.set(id, { check: checker, discard })
  return () => { checkers.delete(id) }
}

export function hasUnsavedChanges(): boolean {
  return [...checkers.values()].some(entry => entry.check())
}

export async function confirmDiscardChanges(): Promise<boolean> {
  if (!hasUnsavedChanges()) return true
  try {
    await ElMessageBox.confirm('当前有尚未保存的配置，确定放弃修改吗？', '未保存的修改', {
      type: 'warning', confirmButtonText: '放弃修改', cancelButtonText: '继续编辑'
    })
    for (const entry of checkers.values()) if (entry.check()) entry.discard?.()
    return true
  } catch {
    return false
  }
}

export function preventUnloadWithUnsavedChanges(event: BeforeUnloadEvent): void {
  if (!hasUnsavedChanges()) return
  event.preventDefault()
  event.returnValue = ''
}
