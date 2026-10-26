// 会话 Pinia store：管理会话列表、当前活跃会话、配置 CRUD、启停、单发
import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import * as wails from '@/utils/wails'
import type { SessionConfig, SessionInfo, SessionStatus } from '@/types'
import { createDefaultSessionConfig } from '@/types'

export const useSessionStore = defineStore('session', () => {
  // 会话摘要列表（侧边栏用）
  const list = ref<SessionInfo[]>([])
  // 当前激活的会话 id（Tab 选中）
  const activeId = ref<string>('')
  // 各会话配置缓存：id -> SessionConfig
  const configMap = ref<Record<string, SessionConfig>>({})
  // 各会话状态缓存：id -> SessionStatus
  const statusMap = ref<Record<string, SessionStatus>>({})

  // 当前激活会话的配置
  const activeConfig = computed<SessionConfig | null>(() => {
    if (!activeId.value) return null
    return configMap.value[activeId.value] ?? null
  })

  // 当前激活会话的状态
  const activeStatus = computed<SessionStatus>(() => {
    return statusMap.value[activeId.value] ?? 'idle'
  })

  // 当前激活会话摘要
  const activeInfo = computed<SessionInfo | null>(() => {
    return list.value.find(s => s.id === activeId.value) ?? null
  })

  // === 全局状态订阅：为所有会话订阅引擎状态事件（status:{id}），
  // 保证切走某客户端会话后，其达到按条数/按时间停止条件自动停止时，侧边栏状态点也能实时变灰。
  // （任务组件仅订阅激活会话的事件，离开期间事件会丢失，需 store 层兜底）
  const statusUnsubs = new Map<string, () => void>()
  function syncStatusSubscriptions(ids: string[]) {
    const idSet = new Set(ids)
    // 清理已删除会话的订阅
    for (const [id, off] of [...statusUnsubs]) {
      if (!idSet.has(id)) {
        off()
        statusUnsubs.delete(id)
      }
    }
    // 为缺失的会话补订阅
    for (const id of ids) {
      if (!statusUnsubs.has(id)) {
        statusUnsubs.set(id, wails.onStatus(id, (status) => setStatus(id, status as SessionStatus)))
      }
    }
  }

  // 拉取会话列表
  async function fetchList() {
    try {
      list.value = await wails.listSessions()
      // 同步状态缓存
      for (const s of list.value) {
        statusMap.value[s.id] = s.status
      }
      // 维护全局状态订阅（新增补订、删除退订）
      syncStatusSubscriptions(list.value.map((s) => s.id))
      // 默认激活第一个会话（仅当无激活或激活已失效时）
      if ((!activeId.value || !list.value.find(s => s.id === activeId.value)) && list.value.length > 0) {
        activeId.value = list.value[0].id
        await fetchConfig(activeId.value)
      }
    } catch (e) {
      console.error('拉取会话列表失败：', e)
    }
  }

  // 拉取单个会话配置
  async function fetchConfig(id: string) {
    try {
      const cfg = await wails.getSessionConfig(id)
      configMap.value[id] = cfg
    } catch (e) {
      console.error('拉取会话配置失败：', e)
    }
  }

  // 新建会话：name 可指定；overrides 指定任务模式与协议（由新建会话向导决定，创建后不可再切换）
  async function create(
    name?: string,
    overrides?: { taskMode?: string; protocol?: string }
  ): Promise<string | null> {
    const seq = (list.value.length + 1).toString().padStart(2, '0')
    const cfg = createDefaultSessionConfig(name || `会话 ${seq}`)
    if (overrides?.taskMode) cfg.taskMode = overrides.taskMode as SessionConfig['taskMode']
    if (overrides?.protocol) cfg.protocol = overrides.protocol as SessionConfig['protocol']
    // Syslog 客户端默认日志正文（区别于 UDP/TCP 的通用默认报文）
    if (overrides?.taskMode === 'syslog' && overrides.protocol !== 'udp_server') {
      cfg.payloadText = 'Hello SuperSender'
    }
    try {
      const created = await wails.createSession(cfg)
      configMap.value[created.id] = created
      await fetchList()
      activeId.value = created.id
      return created.id
    } catch (e: any) {
      console.error('新建会话失败：', e)
      ElMessage.error('新建会话失败：' + (e?.message ?? e))
      return null
    }
  }

  // 重命名会话（Enter 或失焦保存）
  async function rename(id: string, name: string): Promise<boolean> {
    const cfg = configMap.value[id]
    if (!cfg) return false
    const trimmed = name.trim()
    if (!trimmed || trimmed === cfg.name) return false
    cfg.name = trimmed
    return await saveConfig(id, { ...cfg })
  }

  // 删除会话
  async function remove(id: string) {
    try {
      await wails.deleteSession(id)
      delete configMap.value[id]
      delete statusMap.value[id]
      await fetchList()
      // 若删除的是激活会话，则切到第一个
      if (activeId.value === id) {
        activeId.value = list.value[0]?.id ?? ''
      }
    } catch (e) {
      console.error('删除会话失败：', e)
    }
  }

  // 拖拽排序：本地立即重排（乐观更新，不重新拉取避免闪动），并持久化到后端
  async function reorder(ids: string[]) {
    const map = new Map(list.value.map(s => [s.id, s]))
    const next: SessionInfo[] = []
    for (const id of ids) {
      const s = map.get(id)
      if (s) {
        next.push(s)
        map.delete(id)
      }
    }
    // 兜底：拖拽结果中遗漏的项保持在末尾
    for (const s of map.values()) next.push(s)
    list.value = next
    try {
      const res = await wails.reorderSessions(next.map(s => s.id))
      if (!res?.success) console.warn('会话顺序保存失败：', res?.message)
    } catch (e) {
      console.error('会话顺序保存异常：', e)
    }
  }

  // 停止全部会话
  async function stopAll() {
    try {
      await wails.stopAllSessions()
      await fetchList()
    } catch (e) {
      console.error('停止全部会话失败：', e)
    }
  }

  // 保存配置（运行中禁止修改）
  async function saveConfig(id: string, cfg: SessionConfig): Promise<boolean> {
    try {
      const res = await wails.updateSessionConfig(cfg)
      if (res.success) {
        configMap.value[id] = { ...cfg }
        await fetchList()
        return true
      } else {
        console.warn('保存配置失败：', res.message)
        return false
      }
    } catch (e) {
      console.error('保存配置异常：', e)
      return false
    }
  }

  // 启动发包
  async function start(id: string): Promise<boolean> {
    try {
      const res = await wails.startSending(id)
      if (res.success) {
        statusMap.value[id] = 'running'
        return true
      } else {
        console.warn('启动失败：', res.message)
        return false
      }
    } catch (e) {
      console.error('启动异常：', e)
      return false
    }
  }

  // 停止发包
  async function stop(id: string) {
    try {
      await wails.stopSending(id)
      statusMap.value[id] = 'idle'
    } catch (e) {
      console.error('停止异常：', e)
    }
  }

  // 单次发送
  async function sendOnce(id: string, payload: string): Promise<boolean> {
    try {
      const res = await wails.sendOnce(id, payload)
      if (!res.success) {
        console.warn('单次发送失败：', res.message)
      }
      return res.success
    } catch (e) {
      console.error('单次发送异常：', e)
      return false
    }
  }

  // 暂停接收区刷新
  function pauseReceive(id: string) { wails.pauseReceive(id) }
  function resumeReceive(id: string) { wails.resumeReceive(id) }

  // 清空接收区
  function clearReceive(id: string) { wails.clearReceive(id) }

  // 清空计数器
  function clearStats(id: string) { wails.clearStats(id) }

  // 设置状态（事件回调触发）
  function setStatus(id: string, status: SessionStatus) {
    statusMap.value[id] = status
    const item = list.value.find(s => s.id === id)
    if (item) item.status = status
  }

  // 选择激活会话
  function setActive(id: string) {
    activeId.value = id
    if (id && !configMap.value[id]) {
      fetchConfig(id)
    }
  }

  return {
    list, activeId, activeConfig, activeStatus, activeInfo,
    configMap, statusMap,
    fetchList, fetchConfig, create, rename, remove, reorder, stopAll, saveConfig,
    start, stop, sendOnce, pauseReceive, resumeReceive, clearReceive, clearStats,
    setStatus, setActive
  }
})
