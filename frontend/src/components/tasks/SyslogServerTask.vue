<script setup lang="ts">
// Syslog 服务端任务：自包含启动配置（监听）/ Syslog 配置（监听端口）/ 接收日志 / 实时统计。
// 模式在"新建会话"时确定，本组件仅负责 Syslog 服务端（实际走 UDP 协议监听）。
import { computed, ref, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { useSessionStore } from '@/stores/session'
import { useStatsStore } from '@/stores/stats'
import * as wails from '@/utils/wails'
import { i18n } from '@/i18n'
import { Delete, DocumentCopy, Download, Refresh, Search, VideoPause, VideoPlay } from '@element-plus/icons-vue'
import { ENCODING_OPTIONS } from '@/types'
import type { Layout } from 'grid-layout-plus'
import type { NICInfo, ReceiveEvent, SessionConfig } from '@/types'
import TaskGrid from './TaskGrid.vue'

const session = useSessionStore()
const stats = useStatsStore()

// 卡片默认布局（12 列栅格）：左列监听配置 / 右列接收日志 + 统计
const defaultLayout: Layout = [
  { i: 'start', x: 0, y: 0, w: 5, h: 3, minW: 5, minH: 3 },
  { i: 'syslog', x: 0, y: 3, w: 5, h: 4, minW: 5, minH: 4 },
  { i: 'recv', x: 5, y: 0, w: 7, h: 13, minW: 7, minH: 6 },
  { i: 'stats', x: 0, y: 7, w: 5, h: 6, minW: 5, minH: 6 }
]

const cfg = computed<SessionConfig | null>(() => session.activeConfig)
const running = computed(() => session.activeStatus === 'running' || session.activeStatus === 'listening')

// 接收编码（服务端仅接收）
const recvEncoding = computed({
  get: () => cfg.value?.recvEncoding ?? 'utf-8',
  set: (v: any) => {
    if (cfg.value) cfg.value.recvEncoding = v
  }
})

// === 端口占用检测：输入防抖后实际探测，被占用时界面友好提示 ===
const portInUse = ref(false)
const portOccupant = ref('')
let portProbeTimer: ReturnType<typeof setTimeout> | null = null
let portProbeSeq = 0 // 探测序号：仅应用最新一次探测结果，丢弃迟到响应，防止提示卡住
watch(
  () => cfg.value?.targetPort,
  (port) => {
    if (portProbeTimer) clearTimeout(portProbeTimer)
    const seq = ++portProbeSeq
    portProbeTimer = setTimeout(async () => {
      if (!port || running.value) {
        portInUse.value = false
        portOccupant.value = ''
        return
      }
      try {
        const inUse = await wails.checkPortInUse('udp', port)
        if (seq !== portProbeSeq) return // 过期响应，丢弃
        portInUse.value = inUse
        portOccupant.value = inUse ? await wails.getPortOccupant(port).catch(() => '') : ''
      } catch {
        if (seq === portProbeSeq) portInUse.value = false
      }
    }, 300)
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  if (portProbeTimer) clearTimeout(portProbeTimer)
})

// === 网卡列表（服务端绑定 IP 下拉） ===
const nics = ref<NICInfo[]>([])
onMounted(async () => {
  try { nics.value = await wails.getNICs() } catch { /* 加载失败时仅显示默认项 */ }
})
// 默认绑定全部网卡
watch(() => cfg.value?.sourceIP, (v) => {
  if (cfg.value && !v) cfg.value.sourceIP = '0.0.0.0'
}, { immediate: true })

// === 启停（监听） ===
async function start() {
  if (!cfg.value) return
  if (running.value) {
    ElMessage.warning(i18n('task.errRunning'))
    return
  }
  // 启动前校验：端口有效 + 实时探测端口占用
  const port = cfg.value.targetPort
  if (!port || port < 1 || port > 65535) {
    ElMessage.error(i18n('server.errPort'))
    return
  }
  try {
    if (await wails.checkPortInUse('udp', port)) {
      const who = portOccupant.value ? i18n('server.portOccupant', { name: portOccupant.value }) : ''
      ElMessage.error(i18n('server.portOccupied', { port, who }))
      return
    }
  } catch { /* 探测失败时交由后端启动报错兜底 */ }
  const ok = await session.saveConfig(cfg.value.id, { ...cfg.value })
  if (!ok) {
    ElMessage.error(i18n('task.errSaveConfig'))
    return
  }
  const started = await session.start(cfg.value.id)
  if (!started) {
    ElMessage.error(i18n('task.errStart'))
  } else {
    portInUse.value = false
    portOccupant.value = ''
    ElMessage.success(i18n('server.started'))
  }
}

async function stop() {
  if (!cfg.value) return
  await session.stop(cfg.value.id)
  ElMessage.info(i18n('server.stopped'))
}

// === Syslog 配置 ===
// 确保 syslog 配置存在（保持配置结构完整）
function ensureSyslogEnabled() {
  if (cfg.value && !cfg.value.syslog) {
    cfg.value.syslog = {
      enabled: true,
      advanced: false,
      mode: 'rfc3164',
      customPrefix: '',
      facility: 16,
      severity: 6,
      hostname: 'supersender',
      tag: 'app'
    }
  }
}
watch(() => cfg.value?.id, ensureSyslogEnabled, { immediate: true })

// === 接收日志 ===
const MAX_LOCAL = 5000
const events = ref<ReceiveEvent[]>([])

const logFilePath = ref('')
const logEnabled = ref(false)

const recvBoxRef = ref<HTMLDivElement>()
async function autoScroll() {
  await nextTick()
  if (recvBoxRef.value) recvBoxRef.value.scrollTop = recvBoxRef.value.scrollHeight
}

// === 查询浮层：关键字检索接收日志，命中高亮，可打开详情 ===
const queryVisible = ref(false)
const queryKeyword = ref('')
// 大小写不敏感匹配文本内容；空关键字返回空列表
const queryResults = computed(() => {
  const kw = queryKeyword.value.trim().toLowerCase()
  if (!kw) return []
  return events.value.filter(ev => ev.data.toLowerCase().includes(kw))
})
// 按关键字切分文本为片段数组，命中段标记 hit 用于高亮渲染（避免 v-html 注入）
function highlightParts(text: string): { text: string; hit: boolean }[] {
  const kw = queryKeyword.value.trim()
  if (!kw || !text) return [{ text, hit: false }]
  const parts: { text: string; hit: boolean }[] = []
  const lower = text.toLowerCase()
  const kwLower = kw.toLowerCase()
  let i = 0
  while (i < text.length) {
    const idx = lower.indexOf(kwLower, i)
    if (idx === -1) {
      parts.push({ text: text.slice(i), hit: false })
      break
    }
    if (idx > i) parts.push({ text: text.slice(i, idx), hit: false })
    parts.push({ text: text.slice(idx, idx + kw.length), hit: true })
    i = idx + kw.length
  }
  return parts
}
// 结果行仅展示命中区域摘录：结果行尾部会被 CSS 省略，前短后长截取让命中关键字大致落在可视区中间；完整内容经 title 提示展示
const EXCERPT_BEFORE = 24
const EXCERPT_AFTER = 56
function excerptText(text: string): string {
  const kw = queryKeyword.value.trim()
  if (!kw || !text) return text
  const idx = text.toLowerCase().indexOf(kw.toLowerCase())
  if (idx === -1) return text
  const start = Math.max(0, idx - EXCERPT_BEFORE)
  const end = Math.min(text.length, idx + kw.length + EXCERPT_AFTER)
  return (start > 0 ? '…' : '') + text.slice(start, end) + (end < text.length ? '…' : '')
}
function clearQuery() {
  queryKeyword.value = ''
}
// 导出命中结果：仅导出当前关键字命中的日志行，格式与接收区导出一致
async function onExportQuery() {
  const id = session.activeId
  const kw = queryKeyword.value.trim()
  if (!id || !kw || queryResults.value.length === 0) return
  const path = await wails.saveFileDialog(i18n('query.logExportTitle'), `recv-query-${id}.log`)
  if (!path) return
  const res = await wails.exportReceiveFiltered(id, path, kw)
  if (res.success) ElMessage.success(i18n('recv.exported', { path: res.message }))
  else ElMessage.error(i18n('recv.exportFailed', { reason: res.message }))
}

// === 报文详情浮层：点击接收行查看完整内容 ===
const detail = ref<ReceiveEvent | null>(null)
function openDetail(ev: ReceiveEvent) {
  detail.value = ev
}
async function copyDetail() {
  if (!detail.value) return
  try {
    await navigator.clipboard.writeText(detail.value.data)
    ElMessage.success(i18n('query.copiedLog'))
  } catch {
    ElMessage.error(i18n('query.copyFailed'))
  }
}
// hexData 两两分组、每行 16 字节展示
function hexFormatted(ev: ReceiveEvent): string {
  if (!ev.hexData) return ''
  const bytes = ev.hexData.match(/.{2}/g) ?? []
  const lines: string[] = []
  for (let i = 0; i < bytes.length; i += 16) {
    lines.push(bytes.slice(i, i + 16).join(' '))
  }
  return lines.join('\n')
}

let unsub: (() => void) | null = null
let unsubErr: (() => void) | null = null
let unsubStatus: (() => void) | null = null

// 接收事件批量缓冲：高频接收（如 1000 条/秒）时每 200ms 批量刷入一次，
// 避免逐条 push/shift/滚动造成的渲染风暴（渲染频率恒定 ≤5 次/秒）
const pendingRecv: ReceiveEvent[] = []
let flushTimer: ReturnType<typeof setTimeout> | null = null
function scheduleFlush() {
  if (flushTimer) return
  flushTimer = setTimeout(() => {
    flushTimer = null
    if (!pendingRecv.length) return
    const arr = events.value.concat(pendingRecv)
    pendingRecv.length = 0
    events.value = arr.length > MAX_LOCAL ? arr.slice(arr.length - MAX_LOCAL) : arr
    autoScroll()
  }, 200)
}

function subscribe() {
  unsubscribe()
  const id = session.activeId
  if (!id) return
  unsub = wails.onReceive(id, (evt) => {
    pendingRecv.push(evt)
    scheduleFlush()
  })
  unsubErr = wails.onError(id, (msg) => {
    ElMessage.error(`[${id}] ${msg}`)
  })
  unsubStatus = wails.onStatus(id, (status) => {
    session.setStatus(id, status as any)
  })
}

function unsubscribe() {
  if (unsub) { unsub(); unsub = null }
  if (unsubErr) { unsubErr(); unsubErr = null }
  if (unsubStatus) { unsubStatus(); unsubStatus = null }
  if (flushTimer) { clearTimeout(flushTimer); flushTimer = null }
  pendingRecv.length = 0
}

watch(() => session.activeId, async (id) => {
  if (!id) return
  try {
    events.value = await wails.getReceiveBuffer(id)
    autoScroll()
  } catch {
    events.value = []
  }
  // 恢复实时落盘勾选：落盘句柄由后端持有，切走再切回时本地状态需向后端同步
  try {
    const st = await wails.getReceiveLogStatus(id)
    logEnabled.value = st.enabled
    logFilePath.value = st.path
  } catch { /* 查询失败保持默认关闭 */ }
  subscribe()
}, { immediate: true })

onBeforeUnmount(unsubscribe)

async function onClearRecv() {
  events.value = []
  wails.clearReceive(session.activeId)
}

async function onExport() {
  const id = session.activeId
  const path = await wails.saveFileDialog(i18n('recv.logExportTitle'), `recv-${id}.log`)
  if (!path) return
  const res = await wails.exportReceive(id, path)
  if (res.success) ElMessage.success(i18n('recv.exported', { path: res.message }))
  else ElMessage.error(i18n('recv.exportFailed', { reason: res.message }))
}

async function onPickLogPath() {
  const id = session.activeId
  const path = await wails.saveFileDialog(i18n('recv.logFileTitle'), `recv-${id}.log`)
  if (!path) return
  logFilePath.value = path
}

async function onToggleLog(v: boolean | string | number) {
  const id = session.activeId
  const enable = Boolean(v)
  if (enable) {
    if (!logFilePath.value) {
      await onPickLogPath()
      if (!logFilePath.value) {
        logEnabled.value = false
        return
      }
    }
    const res = await wails.enableReceiveLog(id, logFilePath.value)
    if (res.success) {
      logEnabled.value = true
      ElMessage.success(i18n('recv.logEnabled', { path: res.message }))
    } else {
      logEnabled.value = false
      ElMessage.error(i18n('recv.logEnableFailed', { reason: res.message }))
    }
  } else {
    wails.disableReceiveLog(id)
    logEnabled.value = false
    ElMessage.info(i18n('recv.logDisabled'))
  }
}

function ts(ev: ReceiveEvent): string {
  try {
    return new Date(ev.timestamp).toLocaleTimeString('zh-CN', { hour12: false }) +
      '.' + String(new Date(ev.timestamp).getMilliseconds()).padStart(3, '0')
  } catch { return ev.timestamp }
}

// 详情浮层用完整时间格式：yyyy-MM-dd HH:mm:ss.SSS
function tsFull(ev: ReceiveEvent): string {
  try {
    const d = new Date(ev.timestamp)
    const p = (n: number, l = 2) => String(n).padStart(l, '0')
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ` +
      `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`
  } catch { return ev.timestamp }
}

// === 实时统计 ===
const snap = computed(() => stats.get(session.activeId))

const ratePath = computed(() => {
  const arr = stats.getRateHistory(session.activeId)
  if (arr.length < 2) return ''
  const w = 280
  const h = 60
  const max = Math.max(10, ...arr)
  const step = w / Math.max(1, arr.length - 1)
  return arr
    .map((v, i) => `${i === 0 ? 'M' : 'L'} ${(i * step).toFixed(1)} ${(h - (v / max) * h).toFixed(1)}`)
    .join(' ')
})

function fmtBytes(b: number | undefined): string {
  if (!b) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let v = b
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++ }
  return `${v.toFixed(1)} ${u[i]}`
}

function fmtDuration(sec: number | undefined): string {
  if (!sec) return `0${i18n('task.secs')}`
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  if (d > 0) return `${d}d ${h}h ${m}m ${s}s`
  if (h > 0) return `${h}h ${m}m ${s}s`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

function onClearStats() {
  wails.clearStats(session.activeId)
  stats.clear(session.activeId)
}
</script>

<template>
  <TaskGrid v-if="cfg" layout-key="syslog_server" :defaults="defaultLayout">
    <template #start>
        <!-- 启动配置（监听） -->
        <div class="panel-card">
          <div class="panel-title">{{ i18n('task.startupConfig') }}</div>
          <div class="tiny-hint" style="margin-bottom: 4px">{{ i18n('server.hintSyslog') }}</div>
          <div class="send-actions">
            <el-button type="primary" :icon="VideoPlay" :disabled="running" @click="start">{{ i18n('server.startListen') }}</el-button>
            <el-button type="danger" :icon="VideoPause" :disabled="!running" @click="stop">{{ i18n('server.stopListen') }}</el-button>
          </div>
        </div>
    </template>

    <template #syslog>
        <!-- Syslog 配置 -->
        <div class="panel-card">
          <div class="panel-title">{{ i18n('syslog.title') }}</div>
          <el-form size="small" label-width="80px" :show-message="false" :disabled="running">
            <el-form-item :label="i18n('server.bindIP')">
              <el-select v-model="cfg.sourceIP" style="width: 100%">
                <el-option v-for="n in nics" :key="n.ip" :label="`${n.name} (${n.ip})`" :value="n.ip" />
              </el-select>
            </el-form-item>
            <el-form-item :label="i18n('server.bindPort')">
              <el-input-number v-model="cfg.targetPort" :min="1" :max="65535" controls-position="right" style="width: 100%" />
            </el-form-item>
            <div v-if="portInUse" class="port-warn">
              {{ i18n('server.portWarn', { port: cfg.targetPort, name: portOccupant ? ` ${portOccupant}` : '' }) }}
            </div>
            <el-form-item :label="i18n('task.receiveEncoding')">
              <el-select v-model="recvEncoding" style="width: 120px">
                <el-option v-for="o in ENCODING_OPTIONS" :key="o.value" :label="o.label" :value="o.value" />
              </el-select>
            </el-form-item>
          </el-form>
        </div>
    </template>

    <template #recv>
      <!-- 接收日志 -->
      <div class="panel-card recv-card">
        <div class="panel-title">{{ i18n('recv.logTitle') }}</div>
        <div class="recv-toolbar">
          <el-button size="small" :icon="Delete" @click="onClearRecv">{{ i18n('recv.clear') }}</el-button>
          <el-button size="small" :icon="Download" @click="onExport">{{ i18n('recv.export') }}</el-button>
          <el-button size="small" :icon="Search" @click="queryVisible = true">{{ i18n('recv.query') }}</el-button>
          <div class="log-toggle">
            <el-checkbox :model-value="logEnabled" @change="onToggleLog">{{ i18n('recv.realtimeSave') }}</el-checkbox>
            <span v-if="logFilePath" class="tiny-hint log-path">{{ logFilePath }}</span>
            <el-button v-else size="small" @click="onPickLogPath">{{ i18n('recv.pickFile') }}</el-button>
          </div>
        </div>
        <div ref="recvBoxRef" class="recv-box">
          <div
            v-for="(ev, i) in events"
            :key="i"
            class="recv-line"
            :class="{ alt: i % 2 === 1 }"
            :title="i18n('recv.clickDetail')"
            @click="openDetail(ev)"
          >
            <span class="recv-ts">{{ ts(ev) }}</span>
            <span class="recv-src" :title="ev.source">{{ ev.source }}</span>
            <span class="recv-data" :title="ev.data">{{ ev.data }}</span>
          </div>
          <div v-if="events.length === 0" class="empty-hint">{{ i18n('recv.emptyLog') }}</div>
        </div>
      </div>

      <!-- 查询浮层：关键字检索 + 高亮命中 + 详情入口 -->
      <el-dialog v-model="queryVisible" :title="i18n('query.logTitle')" width="760px" top="8vh" append-to-body>
        <div class="query-bar">
          <el-input
            v-model="queryKeyword"
            :placeholder="i18n('query.placeholder')"
            clearable
            size="small"
            @clear="clearQuery"
          >
            <template #append>{{ i18n('query.hits', { n: queryResults.length }) }}</template>
          </el-input>
          <el-button size="small" :disabled="queryResults.length === 0" @click="onExportQuery">{{ i18n('query.exportHits') }}</el-button>
        </div>
        <div class="query-results">
          <div
            v-for="(ev, i) in queryResults"
            :key="i"
            class="query-line"
            :class="{ alt: i % 2 === 1 }"
          >
            <span class="recv-ts">{{ ts(ev) }}</span>
            <span class="recv-src" :title="ev.source">{{ ev.source }}</span>
            <span class="recv-data query-data" :title="ev.data">
              <template v-for="(p, j) in highlightParts(excerptText(ev.data))" :key="j"><mark v-if="p.hit" class="hit">{{ p.text }}</mark><template v-else>{{ p.text }}</template></template>
            </span>
            <el-button size="small" class="query-detail-btn" @click="openDetail(ev)">{{ i18n('query.detail') }}</el-button>
          </div>
          <div v-if="queryKeyword.trim() && queryResults.length === 0" class="empty-hint">{{ i18n('query.noMatch') }}</div>
          <div v-if="!queryKeyword.trim()" class="empty-hint">{{ i18n('query.emptyHint') }}</div>
        </div>
      </el-dialog>

      <!-- 报文详情浮层 -->
      <el-dialog
        :model-value="!!detail"
        @update:model-value="detail = null"
        :title="i18n('query.logDetailTitle')"
        width="680px"
        top="10vh"
        append-to-body
      >
        <template v-if="detail">
          <div class="detail-meta">
            <div class="meta-item"><span class="k">{{ i18n('query.time') }}</span><span class="v">{{ tsFull(detail) }}</span></div>
            <div class="meta-item"><span class="k">{{ i18n('query.source') }}</span><span class="v">{{ detail.source }}</span></div>
            <div class="meta-item"><span class="k">{{ i18n('query.length') }}</span><span class="v">{{ i18n('query.bytes', { n: detail.hexData ? detail.hexData.length / 2 : 0 }) }}</span></div>
            <el-button size="small" :icon="DocumentCopy" @click="copyDetail">{{ i18n('query.copyContent') }}</el-button>
          </div>
          <div class="detail-title">{{ i18n('query.textContent') }}</div>
          <pre class="detail-pre">{{ detail.data || i18n('query.emptyContent') }}</pre>
          <div class="detail-title">{{ i18n('query.hexContent') }}</div>
          <pre class="detail-pre">{{ hexFormatted(detail) || i18n('query.emptyContent') }}</pre>
        </template>
      </el-dialog>
    </template>

    <template #stats>
      <!-- 实时统计 -->
      <div class="panel-card">
        <div class="panel-title">{{ i18n('task.realtimeStats') }}</div>
        <div class="stats-grid">
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('server.statReceived') }}</div>
            <div class="stat-value">{{ snap?.received ?? 0 }}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statTotalBytes') }}</div>
            <div class="stat-value">{{ fmtBytes(snap?.totalBytesRecv) }}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statRate') }}</div>
            <div class="stat-value">{{ (snap?.recvRatePerSec ?? 0).toFixed(1) }}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statByteRate') }}</div>
            <div class="stat-value">{{ fmtBytes(snap?.recvBytesPerSec) }}/s</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statUptime') }}</div>
            <div class="stat-value">{{ fmtDuration(snap?.durationSec) }}</div>
          </div>
          <div class="stat-cell stat-client">
            <div class="stat-label">{{ i18n('server.statClients') }}</div>
            <div class="stat-value">{{ snap?.clientCount ?? 0 }}</div>
          </div>
        </div>
        <div class="chart-wrap">
          <svg viewBox="0 0 280 60" preserveAspectRatio="none" class="rate-chart">
            <path :d="ratePath" stroke="currentColor" fill="none" stroke-width="1.5" />
          </svg>
          <span class="chart-title">{{ i18n('task.rateHistory') }}</span>
        </div>
        <div class="stats-actions">
          <el-button size="small" :icon="Refresh" @click="onClearStats">{{ i18n('task.clearStats') }}</el-button>
        </div>
      </div>
    </template>
  </TaskGrid>
</template>

<style scoped lang="scss">
/* 启动配置 */
.send-actions {
  margin-top: 8px;
  display: flex;
  gap: 8px;
}
:deep(.el-form-item__content) {
  flex-wrap: nowrap;
}

/* 接收日志 */
.recv-card {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}
.recv-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
  flex-shrink: 0;
}
.log-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: 1;
  min-width: 0;
  margin-left: auto;
  justify-content: flex-end;
  .log-path {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
}
.recv-box {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  border: 1px solid var(--panel-border);
  border-radius: 4px;
  background: var(--code-bg);
  padding: 4px 8px;
  font-family: "Cascadia Code", Consolas, "Courier New", monospace;
  font-size: 12px;
  line-height: 1.5;
}
.recv-line {
  display: flex;
  gap: 8px;
  padding: 2px 0;
  cursor: pointer;
  transition: background 0.12s ease;
  &:hover { background: var(--accent-soft); }
  &.alt { background: var(--bg-recv-line-alt); }
  .recv-ts { color: var(--recv-ts); flex: 0 0 92px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .recv-src { color: var(--recv-src); flex: 0 0 150px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; min-width: 0; }
  .recv-data {
    color: var(--panel-text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
    min-width: 0;
  }
}

/* 报文详情浮层 */
.detail-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 24px;
  margin-bottom: 4px;
  .meta-item {
    display: flex;
    align-items: center;
    gap: 6px;
    .k { font-size: 12px; color: var(--panel-text-secondary); }
    .v { font-size: 12.5px; color: var(--panel-text); font-family: Consolas, monospace; }
  }
  > .el-button { margin-left: auto; }
}
.detail-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--panel-text-secondary);
  margin: 12px 0 4px;
}
.detail-pre {
  margin: 0;
  padding: 10px 12px;
  max-height: 220px;
  overflow-y: auto;
  background: var(--panel-bg-secondary);
  border: 1px solid var(--panel-border);
  border-radius: 6px;
  font-family: Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--panel-text);
}
.empty-hint {
  padding: 16px;
  text-align: center;
  color: var(--panel-text-secondary);
}

/* 查询浮层 */
.query-bar {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
  .el-input { flex: 1; }
}
.query-results {
  max-height: 52vh;
  overflow-y: auto;
  border: 1px solid var(--panel-border);
  border-radius: 6px;
  padding: 4px 8px;
  background: var(--panel-bg-secondary);
}
.query-line {
  display: flex;
  gap: 8px;
  padding: 2px 0;
  align-items: center;
  &:hover { background: var(--accent-soft); }
  .recv-ts { color: var(--recv-ts); flex: 0 0 92px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .recv-src { color: var(--recv-src); flex: 0 0 140px; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; min-width: 0; }
  .query-data {
    color: var(--panel-text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
    min-width: 0;
  }
  .query-detail-btn { flex: 0 0 auto; }
}
/* 命中关键字高亮 */
mark.hit {
  background: var(--el-color-warning-light-5);
  color: var(--el-color-warning-dark-2);
  border-radius: 2px;
  padding: 0 1px;
}

/* 实时统计 */
.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 6px;
  margin-bottom: 8px;
}
.stat-cell {
  background: rgba(79, 158, 255, 0.08);
  border-radius: 4px;
  padding: 6px 8px;
  .stat-label {
    font-size: 11px;
    color: var(--panel-text-secondary);
  }
  .stat-value {
    font-size: 16px;
    font-weight: 600;
    color: var(--panel-text);
    margin-top: 2px;
    font-family: "Cascadia Code", Consolas, monospace;
  }
}
.stat-cell.stat-client {
  background: rgba(34, 197, 94, 0.12);
  .stat-value { color: var(--status-running); }
}
.chart-wrap {
  position: relative;
  height: 60px;
  background: var(--code-bg);
  border: 1px solid var(--panel-border);
  border-radius: 4px;
  overflow: hidden;
}
.rate-chart {
  width: 100%;
  height: 100%;
  display: block;
  color: var(--accent);
}
.chart-title {
  position: absolute;
  top: 4px;
  left: 6px;
  font-size: 10px;
  color: var(--panel-text-secondary);
}
.stats-actions {
  margin-top: 6px;
  text-align: right;
}
.empty-hint {
  padding: 32px 16px;
  text-align: center;
  color: var(--panel-text-secondary);
  font-size: 12px;
}
/* 端口占用提示 */
.port-warn {
  margin: -8px 0 2px 80px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--status-error);
}
</style>
