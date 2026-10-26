// 统计 Pinia store：按会话 id 维护实时快照 + 速率折线历史数据点
import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { StatsSnapshot } from '@/types'

// 每个会话保留的最近速率采样点数量
const MAX_POINTS = 120

export const useStatsStore = defineStore('stats', () => {
  // 各会话最新快照
  const snapshots = ref<Record<string, StatsSnapshot>>({})
  // 各会话速率历史点：id -> number[]（条/秒）
  const rateHistory = ref<Record<string, number[]>>({})

  // 接收区是否暂停刷新（仅 UI 不刷新，后端仍计数）
  const recvPaused = ref<Record<string, boolean>>({})

  // 写入快照并更新历史点（服务端不发包，ratePerSec 恒 0，曲线自动回落到接收速率）
  function update(snap: StatsSnapshot) {
    snapshots.value[snap.sessionID] = snap
    const arr = rateHistory.value[snap.sessionID] ?? []
    arr.push(snap.ratePerSec || snap.recvRatePerSec)
    if (arr.length > MAX_POINTS) arr.shift()
    rateHistory.value[snap.sessionID] = arr
  }

  // 取某会话的快照
  function get(id: string): StatsSnapshot | null {
    return snapshots.value[id] ?? null
  }

  // 取某会话的速率折线数据
  function getRateHistory(id: string): number[] {
    return rateHistory.value[id] ?? []
  }

  // 清空某会话的统计
  function clear(id: string) {
    delete snapshots.value[id]
    delete rateHistory.value[id]
  }

  // 接收区暂停/恢复
  function setRecvPaused(id: string, paused: boolean) {
    recvPaused.value[id] = paused
  }
  function isRecvPaused(id: string): boolean {
    return recvPaused.value[id] ?? false
  }

  return { snapshots, rateHistory, update, get, getRateHistory, clear, setRecvPaused, isRecvPaused }
})
