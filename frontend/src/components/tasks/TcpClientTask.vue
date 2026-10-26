<script setup lang="ts">
// TCP 客户端任务：自包含启动配置 / 基础配置（目标地址）/ 报文来源 / 实时统计。
// 模式在"新建会话"时确定，本组件仅负责 TCP 客户端。
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { FolderOpened, Refresh, VideoPause, VideoPlay } from '@element-plus/icons-vue'
import VueMonacoEditor from '@guolao/vue-monaco-editor'
import { useSessionStore } from '@/stores/session'
import { useStatsStore } from '@/stores/stats'
import { useTemplateStore } from '@/stores/template'
import * as wails from '@/utils/wails'
import { i18n } from '@/i18n'
import {
  ENCODING_OPTIONS,
  READ_STRATEGY_OPTIONS,
  TCP_FRAME_MODE_OPTIONS,
  TCP_DELIMITER_OPTIONS,
  TCP_HEADER_BYTES_OPTIONS,
  TCP_BYTE_ORDER_OPTIONS,
  createDefaultTCPFrameConfig
} from '@/types'
import type { SessionConfig, TCPFrameMode } from '@/types'
import type { Layout } from 'grid-layout-plus'
import TaskGrid from './TaskGrid.vue'

const session = useSessionStore()
const stats = useStatsStore()
const templates = useTemplateStore()

// 卡片默认布局（12 列栅格）：左列启动/会话/TCP 协议卡 / 右列报文配置 + 统计
const defaultLayout: Layout = [
  { i: 'start', x: 0, y: 0, w: 6, h: 5, minW: 6, minH: 5 },
  { i: 'session', x: 0, y: 5, w: 6, h: 6, minW: 6, minH: 6 },
  { i: 'tcp', x: 0, y: 11, w: 6, h: 5, minW: 6, minH: 3 },
  { i: 'payload', x: 6, y: 0, w: 6, h: 8, minW: 6, minH: 8 },
  { i: 'stats', x: 6, y: 8, w: 6, h: 8, minW: 6, minH: 8 }
]

const cfg = computed<SessionConfig | null>(() => session.activeConfig)
const running = computed(() => session.activeStatus === 'running' || session.activeStatus === 'listening')

// 模板分类分组：从模板列表动态推导（含 custom），支持自定义分类
const templateCategories = computed(() => {
  const set = new Set<string>(['custom'])
  for (const t of templates.list) if (t.category) set.add(t.category)
  return ['custom', ...Array.from(set).filter(c => c !== 'custom')]
})

// === 启停 ===
// 启动前表单校验：目标地址 / 端口 / 停止条件
function validateForm(): boolean {
  if (!cfg.value) return false
  if (!cfg.value.targetIP || !cfg.value.targetIP.trim()) {
    ElMessage.error(i18n('task.errIPRequired'))
    return false
  }
  if (!/^(\d{1,3}(\.\d{1,3}){3}|[0-9a-fA-F:]+)$/.test(cfg.value.targetIP.trim())) {
    ElMessage.error(i18n('task.errIPInvalid', { ip: cfg.value.targetIP }))
    return false
  }
  if (!cfg.value.targetPort || cfg.value.targetPort < 1 || cfg.value.targetPort > 65535) {
    ElMessage.error(i18n('task.errPort'))
    return false
  }
  if (cfg.value.ratePerSec < 1) {
    ElMessage.error(i18n('task.errRate'))
    return false
  }
  if (cfg.value.stopCondition === 'count' && (!cfg.value.stopCount || cfg.value.stopCount < 1)) {
    ElMessage.error(i18n('task.errCount'))
    return false
  }
  if (cfg.value.stopCondition === 'duration' && (!cfg.value.stopDurationSec || cfg.value.stopDurationSec < 1)) {
    ElMessage.error(i18n('task.errDuration'))
    return false
  }
  return true
}

async function start() {
  if (!cfg.value) return
  if (running.value) {
    ElMessage.warning(i18n('task.errRunning'))
    return
  }
  if (!validateForm()) return
  const ok = await session.saveConfig(cfg.value.id, { ...cfg.value })
  if (!ok) {
    ElMessage.error(i18n('task.errSaveConfig'))
    return
  }
  const started = await session.start(cfg.value.id)
  if (!started) {
    ElMessage.error(i18n('task.errStart'))
  } else {
    ElMessage.success(i18n('task.started'))
  }
}

async function stop() {
  if (!cfg.value) return
  await session.stop(cfg.value.id)
  ElMessage.info(i18n('task.stopped'))
}

// === 启动配置：发包模式 / 条数 / 时长 / 速率 ===
const sendMode = computed<'count' | 'duration'>({
  get: () => (cfg.value?.stopCondition === 'count' ? 'count' : 'duration'),
  set: (v) => {
    if (!cfg.value) return
    cfg.value.mode = 'rate'
    cfg.value.stopCondition = v
  }
})

const stopDays = ref(0)
const stopHours = ref(0)
const stopMins = ref(0)
const stopSecs = ref(0)

function syncFromTotal() {
  const total = cfg.value?.stopDurationSec ?? 0
  stopDays.value = Math.floor(total / 86400)
  stopHours.value = Math.floor((total % 86400) / 3600)
  stopMins.value = Math.floor((total % 3600) / 60)
  stopSecs.value = total % 60
}

function syncToTotal() {
  if (!cfg.value) return
  cfg.value.stopDurationSec =
    stopDays.value * 86400 + stopHours.value * 3600 + stopMins.value * 60 + stopSecs.value
}

watch(() => cfg.value?.id, syncFromTotal, { immediate: true })
watch([stopDays, stopHours, stopMins, stopSecs], syncToTotal)

// === 报文配置 ===
const editorTheme = computed(() => (document.documentElement.classList.contains('dark') ? 'vs-dark' : 'vs'))
const editorOptions = computed(() => ({
  minimap: { enabled: false },
  fontSize: 13,
  lineNumbers: 'on' as 'on' | 'off',
  scrollBeyondLastLine: false,
  wordWrap: 'on' as 'on' | 'off',
  tabSize: 4,
  automaticLayout: true,
  // 显式滚动条配置：内容超出时出现垂直滚动条，滚轮事件由编辑器消费
  scrollbar: {
    vertical: 'auto' as 'auto' | 'visible' | 'hidden',
    horizontal: 'auto' as 'auto' | 'visible' | 'hidden',
    alwaysConsumeMouseWheel: true
  },
  renderWhitespace: 'selection' as 'selection' | 'none' | 'boundary' | 'trailing' | 'all',
  // 运行中禁止编辑报文内容
  readOnly: running.value
}))

// 加载本地文件：内容直接填入编辑器（不绑定文件路径，后续可自由编辑）
async function loadFile() {
  if (!cfg.value) return
  const path = await wails.openFileDialog(i18n('task.payloadFileTitle'))
  if (!path) return
  try {
    cfg.value.payloadText = await wails.readTextFile(path)
    ElMessage.success(i18n('task.payloadLoaded', { path }))
  } catch (e: any) {
    ElMessage.error(i18n('task.payloadLoadFailed', { reason: e?.message ?? e }))
  }
}

function applyTemplate(id: string) {
  if (!cfg.value || !id) return
  const t = templates.getById(id)
  if (t) {
    cfg.value.payloadText = t.content
    cfg.value.templateID = id
  }
}

const paramEnabled = computed({
  get: () => cfg.value?.paramEnabled ?? false,
  set: (v: boolean) => {
    if (cfg.value) cfg.value.paramEnabled = v
  }
})

const readStrategy = computed({
  get: () => cfg.value?.readStrategy ?? 'sequential',
  set: (v: any) => {
    if (cfg.value) cfg.value.readStrategy = v
  }
})

const sendEncoding = computed({
  get: () => cfg.value?.sendEncoding ?? 'utf-8',
  set: (v: any) => {
    if (cfg.value) cfg.value.sendEncoding = v
  }
})

// === TCP 协议化（分帧）配置 ===
// 历史会话可能缺少 tcp 字段，激活时补齐默认值（默认原始裸流，行为与旧版本一致）
watch(
  () => cfg.value?.id,
  () => {
    if (cfg.value && !cfg.value.tcp) cfg.value.tcp = createDefaultTCPFrameConfig()
  },
  { immediate: true }
)

const DELIM_CUSTOM = '__custom__'

const tcpMode = computed<TCPFrameMode>({
  get: () => cfg.value?.tcp?.mode ?? 'raw',
  set: (v) => {
    if (!cfg.value) return
    if (!cfg.value.tcp) cfg.value.tcp = createDefaultTCPFrameConfig()
    cfg.value.tcp.mode = v
    // 切换模式时补齐缺省参数，避免后端因参数非法退化为裸流
    if (v === 'delimiter' && !cfg.value.tcp.delimiter) cfg.value.tcp.delimiter = '\\r\\n'
    if (v === 'fixed' && (!cfg.value.tcp.frameLength || cfg.value.tcp.frameLength < 1)) {
      cfg.value.tcp.frameLength = 64
    }
  }
})

const tcpModeHint = computed(() => {
  const key = TCP_FRAME_MODE_OPTIONS.find((o) => o.value === tcpMode.value)?.descKey
  return key ? i18n(key) : ''
})

const delimPreset = computed<string>({
  get: () => {
    const d = cfg.value?.tcp?.delimiter ?? ''
    return TCP_DELIMITER_OPTIONS.some((o) => o.value === d && o.value !== DELIM_CUSTOM) ? d : DELIM_CUSTOM
  },
  set: (v) => {
    if (!cfg.value) return
    if (!cfg.value.tcp) cfg.value.tcp = createDefaultTCPFrameConfig()
    if (v !== DELIM_CUSTOM) {
      cfg.value.tcp.delimiter = v
    } else {
      // 切到自定义时清空预设内容，交给用户输入转义文本
      cfg.value.tcp.delimiter = ''
    }
  }
})

const tcpDelimiter = computed<string>({
  get: () => cfg.value?.tcp?.delimiter ?? '',
  set: (v) => {
    if (!cfg.value) return
    if (!cfg.value.tcp) cfg.value.tcp = createDefaultTCPFrameConfig()
    cfg.value.tcp.delimiter = v
  }
})

const tcpFrameLength = computed<number>({
  get: () => cfg.value?.tcp?.frameLength ?? 64,
  set: (v) => {
    if (cfg.value?.tcp) cfg.value.tcp.frameLength = v
  }
})

const tcpHeaderBytes = computed<number>({
  get: () => cfg.value?.tcp?.headerBytes ?? 4,
  set: (v) => {
    if (cfg.value?.tcp) cfg.value.tcp.headerBytes = v
  }
})

const tcpByteOrder = computed<'big' | 'little'>({
  get: () => cfg.value?.tcp?.byteOrder ?? 'big',
  set: (v) => {
    if (cfg.value?.tcp) cfg.value.tcp.byteOrder = v
  }
})

const tcpLengthBasis = computed<'body' | 'self'>({
  get: () => (cfg.value?.tcp?.lengthIncludesSelf ? 'self' : 'body'),
  set: (v) => {
    if (cfg.value?.tcp) cfg.value.tcp.lengthIncludesSelf = v === 'self'
  }
})

// === 事件订阅（错误提示 + 状态同步；接收报文卡片已移除，不再订阅接收流） ===
let unsubErr: (() => void) | null = null
let unsubStatus: (() => void) | null = null

function subscribe() {
  unsubscribe()
  const id = session.activeId
  if (!id) return
  unsubErr = wails.onError(id, (msg) => {
    ElMessage.error(`[${id}] ${msg}`)
  })
  unsubStatus = wails.onStatus(id, (status) => {
    session.setStatus(id, status as any)
  })
}

function unsubscribe() {
  if (unsubErr) { unsubErr(); unsubErr = null }
  if (unsubStatus) { unsubStatus(); unsubStatus = null }
}

watch(() => session.activeId, (id) => {
  subscribe()
  // 切回会话时主动同步一次后端真实状态：离开期间引擎自动停止（按条数/时长）的状态事件已丢失，
  // 仅靠事件订阅会让按钮残留"启动中"
  if (id) session.fetchList()
}, { immediate: true })

onBeforeUnmount(unsubscribe)

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
  <TaskGrid v-if="cfg" layout-key="tcp_client" :defaults="defaultLayout">
    <template #start>
        <!-- 启动配置 -->
        <div class="panel-card">
          <div class="panel-title">{{ i18n('task.startupConfig') }}</div>
          <el-form size="small" label-width="80px" :show-message="false" :disabled="running">
            <el-form-item :label="i18n('task.sendMode')">
              <el-radio-group v-model="sendMode" :disabled="running">
                <el-radio label="count">{{ i18n('task.byCount') }}</el-radio>
                <el-radio label="duration">{{ i18n('task.byDuration') }}</el-radio>
              </el-radio-group>
            </el-form-item>
            <el-form-item v-if="sendMode === 'count'" :label="i18n('task.totalCount')">
              <el-input-number v-model="cfg.stopCount" :min="1" :max="1000000000" :disabled="running" controls-position="right" />
              <span style="margin-left: 6px">{{ i18n('task.countUnit') }}</span>
              <span class="tiny-hint" style="margin-left: 8px">{{ i18n('task.autoStop') }}</span>
            </el-form-item>
            <el-form-item v-if="sendMode === 'duration'" :label="i18n('task.duration')">
              <el-input-number v-model="stopDays" :min="0" :max="30" :disabled="running" controls-position="right" size="small" style="width: 68px" />
              <span class="unit">{{ i18n('task.days') }}</span>
              <el-input-number v-model="stopHours" :min="0" :max="23" :disabled="running" controls-position="right" size="small" style="width: 68px" />
              <span class="unit">{{ i18n('task.hours') }}</span>
              <el-input-number v-model="stopMins" :min="0" :max="59" :disabled="running" controls-position="right" size="small" style="width: 68px" />
              <span class="unit">{{ i18n('task.mins') }}</span>
              <el-input-number v-model="stopSecs" :min="0" :max="59" :disabled="running" controls-position="right" size="small" style="width: 68px" />
              <span class="unit">{{ i18n('task.secs') }}</span>
            </el-form-item>
            <el-form-item :label="i18n('task.rate')">
              <el-input-number v-model="cfg.ratePerSec" :min="1" :disabled="running" controls-position="right" />
              <span style="margin-left: 6px">{{ i18n('task.perSec') }}</span>
            </el-form-item>
          </el-form>
          <div class="send-actions">
            <el-button type="primary" :icon="VideoPlay" :disabled="running" @click="start">{{ i18n('task.start') }}</el-button>
            <el-button type="danger" :icon="VideoPause" :disabled="!running" @click="stop">{{ i18n('task.stop') }}</el-button>
          </div>
        </div>
    </template>

    <template #session>
        <!-- 会话配置 -->
        <div class="panel-card">
          <div class="panel-title">{{ i18n('task.sessionConfig') }}</div>
          <el-form size="small" label-width="80px" :show-message="false" :disabled="running">
            <el-form-item :label="i18n('task.sessionName')">
              <el-input v-model="cfg.name" :placeholder="i18n('task.sessionNamePlaceholder')" />
            </el-form-item>
            <el-form-item :label="i18n('task.targetIP')">
              <el-input v-model="cfg.targetIP" placeholder="127.0.0.1" />
            </el-form-item>
            <el-form-item :label="i18n('task.targetPort')">
              <el-input-number v-model="cfg.targetPort" :min="1" :max="65535" controls-position="right" style="width: 100%" />
            </el-form-item>
            <el-form-item :label="i18n('task.sendEncoding')">
              <el-select v-model="sendEncoding" style="width: 120px">
                <el-option v-for="o in ENCODING_OPTIONS" :key="o.value" :label="o.label" :value="o.value" />
              </el-select>
            </el-form-item>
          </el-form>
        </div>
    </template>

    <template #tcp>
        <!-- TCP 协议化（分帧） -->
        <div class="panel-card">
          <div class="panel-title">{{ i18n('tcpFrame.title') }}</div>
          <el-form size="small" label-width="80px" :show-message="false" :disabled="running">
            <el-form-item :label="i18n('tcpFrame.mode')">
              <el-radio-group v-model="tcpMode">
                <el-radio v-for="o in TCP_FRAME_MODE_OPTIONS" :key="o.value" :label="o.value">{{ i18n(o.labelKey) }}</el-radio>
              </el-radio-group>
            </el-form-item>
            <div class="tiny-hint tcp-mode-hint">{{ tcpModeHint }}</div>
            <el-form-item v-if="tcpMode === 'delimiter'" :label="i18n('tcpFrame.delimiter')">
              <el-select v-model="delimPreset" style="width: 160px">
                <el-option v-for="o in TCP_DELIMITER_OPTIONS" :key="o.value" :label="i18n(o.labelKey)" :value="o.value" />
              </el-select>
              <el-input
                v-if="delimPreset === DELIM_CUSTOM"
                v-model="tcpDelimiter"
                :placeholder="i18n('tcpFrame.delimiterPlaceholder')"
                style="width: 200px; margin-left: 8px"
              />
            </el-form-item>
            <el-form-item v-if="tcpMode === 'fixed'" :label="i18n('tcpFrame.frameLength')">
              <el-input-number v-model="tcpFrameLength" :min="1" :max="1048576" controls-position="right" />
              <span style="margin-left: 6px">{{ i18n('tcpFrame.bytesPerFrame') }}</span>
            </el-form-item>
            <template v-if="tcpMode === 'length'">
              <el-form-item :label="i18n('tcpFrame.lengthHeader')">
                <el-radio-group v-model="tcpHeaderBytes">
                  <el-radio v-for="o in TCP_HEADER_BYTES_OPTIONS" :key="o.value" :label="o.value">{{ i18n(o.labelKey) }}</el-radio>
                </el-radio-group>
              </el-form-item>
              <el-form-item :label="i18n('tcpFrame.byteOrder')">
                <el-radio-group v-model="tcpByteOrder">
                  <el-radio v-for="o in TCP_BYTE_ORDER_OPTIONS" :key="o.value" :label="o.value">{{ i18n(o.labelKey) }}</el-radio>
                </el-radio-group>
              </el-form-item>
              <el-form-item :label="i18n('tcpFrame.lengthBasis')">
                <el-radio-group v-model="tcpLengthBasis">
                  <el-radio label="body">{{ i18n('tcpFrame.basisBody') }}</el-radio>
                  <el-radio label="self">{{ i18n('tcpFrame.basisSelf') }}</el-radio>
                </el-radio-group>
              </el-form-item>
            </template>
          </el-form>
        </div>
    </template>

    <template #payload>
        <!-- 报文配置 -->
        <div class="panel-card">
          <div class="panel-title">{{ i18n('task.payloadConfig') }}</div>
          <div class="payload-toolbar">
            <el-select v-model="cfg.templateID" :placeholder="i18n('task.templatePlaceholder')" size="small" clearable filterable @change="applyTemplate" style="width: 260px">
              <el-option-group v-for="cat in templateCategories" :key="cat" :label="cat">
                <el-option v-for="t in templates.byCategory(cat)" :key="t.id" :label="t.name" :value="t.id" />
              </el-option-group>
            </el-select>
            <el-button size="small" :icon="FolderOpened" @click="loadFile">{{ i18n('task.loadFile') }}</el-button>
          </div>

          <div class="editor-wrap" :class="{ 'editor-disabled': running }">
            <VueMonacoEditor
              v-model:value="cfg.payloadText"
              language="plaintext"
              :theme="editorTheme"
              :options="editorOptions"
              height="240px"
            />
          </div>

          <el-form size="small" label-width="80px" :show-message="false" :disabled="running" style="margin-top: 8px">
            <el-form-item :label="i18n('task.readStrategy')">
              <el-radio-group v-model="readStrategy">
                <el-radio v-for="o in READ_STRATEGY_OPTIONS" :key="o.value" :label="o.value">{{ i18n(o.labelKey) }}</el-radio>
              </el-radio-group>
            </el-form-item>
            <el-form-item :label="i18n('task.paramEnabled')">
              <el-switch v-model="paramEnabled" />
              <span class="tiny-hint" style="margin-left: 8px">
                {{ i18n('task.paramHint') }}
              </span>
            </el-form-item>
          </el-form>
        </div>
    </template>

    <template #stats>
      <!-- 实时统计 -->
      <div class="panel-card">
        <div class="panel-title">{{ i18n('task.realtimeStats') }}</div>
        <div class="stats-grid">
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statTotalSent') }}</div>
            <div class="stat-value">{{ snap?.totalSent ?? 0 }}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statSessionSent') }}</div>
            <div class="stat-value">{{ snap?.sessionSent ?? 0 }}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statReceived') }}</div>
            <div class="stat-value">{{ snap?.received ?? 0 }}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statTotalBytes') }}</div>
            <div class="stat-value">{{ fmtBytes(snap?.totalBytesSent) }}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statRate') }}</div>
            <div class="stat-value">{{ (snap?.ratePerSec ?? 0).toFixed(1) }}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statByteRate') }}</div>
            <div class="stat-value">{{ fmtBytes(snap?.bytesPerSec) }}/s</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">{{ i18n('task.statUptime') }}</div>
            <div class="stat-value">{{ fmtDuration(snap?.durationSec) }}</div>
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
.unit {
  margin: 0 4px 0 2px;
  color: var(--panel-text-secondary);
  font-size: 12px;
}
:deep(.el-form-item__content) {
  flex-wrap: nowrap;
}

/* TCP 协议分帧模式提示与表单项对齐 */
.tcp-mode-hint {
  margin: -4px 0 6px 80px;
}

/* 报文来源 */
.payload-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin-bottom: 6px;
}
.editor-wrap {
  border: 1px solid var(--panel-border);
  border-radius: 4px;
  overflow: hidden;
  height: 240px;
  /* grid 卡片内为 flex 列布局，禁止压缩编辑器高度，否则 Monaco 测量退化、滚动条不出现 */
  flex: none;
}
.editor-wrap.editor-disabled {
  /* 运行中只读：视觉置灰对齐禁用态（背景变淡灰 + 内容弱化 + 禁止手势），仍可滚动查看 */
  background: var(--panel-bg-secondary);
  opacity: 0.55;
  cursor: not-allowed;
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
</style>
