import { ref } from 'vue'
import { nodeApi } from '../api/nodeApi'
import type { NodeSummary } from '../features/nodes/types'

// 各业务页面共用当前游戏，切换页面不再丢失工作上下文。
export const games = ref<NodeSummary[]>([])
export const currentGameId = ref<number | null>(null)
export const gamesLoading = ref(false)

export async function loadGames() {
  gamesLoading.value = true
  try {
    const result = await nodeApi.getTreeChildren(0)
    games.value = result.data
    if (!games.value.some(game => game.id === currentGameId.value)) {
      currentGameId.value = games.value[0]?.id ?? null
    }
  } finally {
    gamesLoading.value = false
  }
}

export function currentGameName() {
  return games.value.find(game => game.id === currentGameId.value)?.name ?? '请选择游戏'
}
