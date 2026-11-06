import { ElMessageBox } from 'element-plus'

// 各配置编辑器统一注册未保存状态，供页面导航、切换游戏和刷新保护使用。
const checkers = new Map<symbol, () => boolean>()

export function registerUnsavedChecker(checker: () => boolean): () => void {
  const id = Symbol('unsaved-config')
  checkers.set(id, checker)
  return () => { checkers.delete(id) }
}

export function hasUnsavedChanges(): boolean {
  return [...checkers.values()].some(checker => checker())
}

export async function confirmDiscardChanges(): Promise<boolean> {
  if (!hasUnsavedChanges()) return true
  try {
    await ElMessageBox.confirm('当前有尚未保存的配置，确定放弃修改吗？', '未保存的修改', {
      type: 'warning', confirmButtonText: '放弃修改', cancelButtonText: '继续编辑'
    })
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
