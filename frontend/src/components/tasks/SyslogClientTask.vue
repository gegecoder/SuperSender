<script setup lang="ts">
// Syslog 客户端任务：自包含启动配置 / Syslog 配置（目标+高级配置）/ 日志来源 / 实时统计。
// 模式在"新建会话"时确定，本组件仅负责 Syslog 客户端（实际走 UDP 协议发送）。
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { Delete, FolderOpened, Plus, Refresh, VideoPause, VideoPlay } from '@element-plus/icons-vue'
import VueMonacoEditor from '@guolao/vue-monaco-editor'
import { useSessionStore } from '@/stores/session'
import { useStatsStore } from '@/stores/stats'
import { useTemplateStore } from '@/stores/template'
import * as wails from '@/utils/wails'
import { i18n } from '@/i18n'
import { ENCODING_OPTIONS, READ_STRATEGY_OPTIONS } from '@/types'
import type { Layout } from 'grid-layout-plus'
import type { SessionConfig, SessionStatus, SyslogOption, Target, Template } from '@/types'
import TaskGrid from './TaskGrid.vue'

const session = useSessionStore()
const stats = useStatsStore()
const templates = useTemplateStore()

// 卡片默认布局（12 列栅格）：左列启动/Syslog/日志来源 / 右列实时统计
const defaultLayout: Layout = [
  { i: 'start', x: 0, y: 0, w: 6, h: 6, minW: 6, minH: 6 },
  { i: 'syslog', x: 6, y: 0, w: 6, h: 6, minW: 6, minH: 6 },
  { i: 'source', x: 0, y: 6, w: 6, h: 8, minW: 6, minH: 8 },
  { i: 'stats', x: 6, y: 6, w: 6, h: 8, minW: 6, minH: 6 }
]

const cfg = computed<SessionConfig | null>(() => session.activeConfig)
const running = computed(() => session.activeStatus === 'running' || session.activeStatus === 'listening')

// 模板分类分组：从模板列表动态推导（含 custom），支持自定义分类
const templateCategories = computed(() => {
  const set = new Set<string>(['custom'])
  for (const t of templates.list) if (t.category) set.add(t.category)
  return ['custom', ...Array.from(set).filter(c => c !== 'custom')]
})

// 发送编码（Syslog 客户端只发不收）
const sendEncoding = computed({
  get: () => cfg.value?.sendEncoding ?? 'utf-8',
  set: (v: any) => {
    if (cfg.value) cfg.value.sendEncoding = v
  }
})

// === 多目标发送（参考 UDP 客户端实现） ===
// 启用时默认把"目标 IP + 目标端口"作为表格第一条记录，该行跟随主目标配置、不可编辑删除
const enableMulti = computed({
  get: () => cfg.value?.enableMulti ?? false,
  set: (v: boolean) => {
    if (!cfg.value) return
    cfg.value.enableMulti = v
    if (v) {
      const main: Target = { ip: cfg.value.targetIP || '127.0.0.1', port: cfg.value.targetPort || 514 }
      if (cfg.value.targets.length === 0) cfg.value.targets.push(main)
      else cfg.value.targets[0] = main
    }
  }
})

// 主目标变化时同步第一行
watch(() => [cfg.value?.targetIP, cfg.value?.targetPort], () => {
  if (cfg.value?.enableMulti && cfg.value.targets.length > 0) {
    cfg.value.targets[0].ip = cfg.value.targetIP
    cfg.value.targets[0].port = cfg.value.targetPort
  }
})

function addTarget() {
  if (!cfg.value) return
  cfg.value.targets.push({ ip: '127.0.0.1', port: 514 } as Target)
}
function removeTarget(i: number) {
  if (!cfg.value) return
  cfg.value.targets.splice(i, 1)
}

// === 启停（发包） ===
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

// === Syslog 配置 ===
// Facility / Severity 选项（后端枚举）
const facilities = ref<SyslogOption[]>([])
const severities = ref<SyslogOption[]>([])

onMounted(async () => {
  try {
    const [fac, sev] = await Promise.all([wails.facilityOptions(), wails.severityOptions()])
    facilities.value = fac
    severities.value = sev
  } catch (e) {
    console.warn('加载 Syslog 选项失败：', e)
  }
})

// === 引擎状态订阅 ===
// 关键：按条数/时长自动停止时引擎会推送 status:<id> 事件，
// 若不订阅，前端状态将停留在 running，表现为"发完了还没停止"。
let unsubErr: (() => void) | null = null
let unsubStatus: (() => void) | null = null

function subscribeEngine() {
  unsubscribeEngine()
  const id = session.activeId
  if (!id) return
  unsubErr = wails.onError(id, (msg) => {
    ElMessage.error(`[${id}] ${msg}`)
  })
  unsubStatus = wails.onStatus(id, (status) => {
    session.setStatus(id, status as SessionStatus)
  })
}

function unsubscribeEngine() {
  if (unsubErr) { unsubErr(); unsubErr = null }
  if (unsubStatus) { unsubStatus(); unsubStatus = null }
}

watch(() => session.activeId, (id) => {
  if (id) {
    subscribeEngine()
    // 切回会话时主动同步一次后端真实状态：离开期间引擎自动停止（按条数/时长）的状态事件已丢失，
    // 仅靠事件订阅会让按钮残留"启动中"
    session.fetchList()
  } else {
    unsubscribeEngine()
  }
}, { immediate: true })

// 确保 syslog 配置已启用（Syslog 客户端语义上必须封装，UI 无 enabled 开关）
function ensureSyslogEnabled() {
  if (!cfg.value) return
  if (!cfg.value.syslog) {
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
  } else {
    // 工厂默认 enabled:false，Syslog 客户端必须强制开启，否则后端 WrapIfNeeded 不封装
    cfg.value.syslog.enabled = true
    if (!cfg.value.syslog.mode) cfg.value.syslog.mode = 'rfc3164' // 旧会话兜底
  }
}
watch(() => cfg.value?.id, ensureSyslogEnabled, { immediate: true })

// 简化引用
const syslog = computed(() => cfg.value?.syslog ?? null)

// 日志正文
const message = ref('Hello SuperSender')

// Monaco 编辑器配置（与 UDP 客户端报文配置一致的开源编辑组件）
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
  // 运行中禁止编辑日志正文
  readOnly: running.value
}))

// === 预览（防抖调用后端生成最终报文） ===
const preview = ref('')
let previewTimer: ReturnType<typeof setTimeout> | null = null

async function refreshPreview(src?: string) {
  if (!syslog.value) return
  const content = src ?? message.value
  try {
    preview.value = await wails.buildSyslog({ ...syslog.value }, content)
  } catch (e) {
    preview.value = ''
    console.warn('生成预览失败：', e)
  }
}

function debouncePreview(src?: string) {
  if (previewTimer) clearTimeout(previewTimer)
  previewTimer = setTimeout(() => refreshPreview(src), 300)
}

// 双向同步：日志正文 ⇆ payloadText（后端发送统一使用 payloadText）
watch(message, (v) => {
  if (cfg.value && cfg.value.payloadText !== v) {
    cfg.value.payloadText = v
  }
  debouncePreview(v)
})

watch(
  () => cfg.value?.payloadText,
  (v) => {
    if (v !== undefined && v !== message.value) {
      message.value = v
      debouncePreview(v)
    }
  }
)

// 会话切换/首次挂载时同步日志正文，保证"编辑器显示 = 实际发送"：
// 以会话已有正文为准回填编辑器，空正文则回填默认值，并立即刷新一次预览
watch(() => cfg.value?.id, () => {
  if (!cfg.value) return
  if (cfg.value.payloadText) {
    if (cfg.value.payloadText !== message.value) message.value = cfg.value.payloadText
  } else {
    cfg.value.payloadText = message.value
  }
  debouncePreview()
}, { immediate: true })

// 高级配置变化影响报文头，重新生成预览
watch(
  () => [syslog.value?.advanced, syslog.value?.mode, syslog.value?.customPrefix, syslog.value?.facility, syslog.value?.severity, syslog.value?.hostname, syslog.value?.tag],
  () => debouncePreview()
)

onBeforeUnmount(() => {
  if (previewTimer) clearTimeout(previewTimer)
  unsubscribeEngine()
})

// === 套用模板 / 文件样本（对齐 UDP 客户端报文配置） ===
const templateID = ref('')

// 下拉选项展示：模板名 +（厂商/产品/型号/版本 元数据，若有）
function templateOptionLabel(t: Template): string {
  const meta = [t.vendor, t.product, t.model, t.version].filter(Boolean).join(' / ')
  return meta ? `${t.name}（${meta}）` : t.name
}

// 套用选中模板：选择即套用，填充日志正文并刷新预览（message watch 自动同步 payloadText）
function applySelectedTemplate() {
  if (!templateID.value) return
  const t = templates.getById(templateID.value)
  if (!t) return
  if (cfg.value) cfg.value.templateID = t.id
  message.value = t.content
  debouncePreview(t.content)
}

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
  <TaskGrid v-if="cfg && syslog" layout-key="syslog_client" :defaults="defaultLayout">
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
    <template #syslog>
        <!-- Syslog 配置 -->
        <div class="panel-card">
          <div class="panel-title">{{ i18n('syslog.title') }}</div>
          <el-form size="small" label-width="80px" :show-message="false" :disabled="running">
            <el-form-item :label="i18n('task.targetIP')">
              <el-input v-model="cfg.targetIP" :placeholder="i18n('syslog.targetIPPlaceholder')" clearable :disabled="running" />
            </el-form-item>
            <el-form-item :label="i18n('task.targetPort')">
              <el-input-number v-model="cfg.targetPort" :min="1" :max="65535" controls-position="right" style="width: 100%" />
            </el-form-item>
            <el-form-item :label="i18n('task.multiTarget')">
              <el-switch v-model="enableMulti" />
              <span class="tiny-hint" style="margin-left: 8px">{{ i18n('syslog.multiHint') }}</span>
            </el-form-item>
            <div v-if="enableMulti" class="multi-targets">
              <div class="multi-targets-header">
                <span>{{ i18n('task.targetsTitle', { n: cfg.targets.length }) }}</span>
                <el-button size="small" :icon="Plus" @click="addTarget">{{ i18n('common.add') }}</el-button>
              </div>
              <el-table :data="cfg.targets" size="small" max-height="240" border>
                <el-table-column type="index" label="#" width="42" />
                <el-table-column :label="i18n('task.colIP')">
                  <template #default="{ row, $index }">
                    <el-input v-model="row.ip" size="small" placeholder="127.0.0.1" :disabled="$index === 0" />
                  </template>
                </el-table-column>
                <el-table-column :label="i18n('task.colPort')" width="130">
                  <template #default="{ row, $index }">
                    <el-input-number v-model="row.port" :min="1" :max="65535" size="small" controls-position="right" style="width: 100%" :disabled="$index === 0" />
                  </template>
                </el-table-column>
                <el-table-column :label="i18n('task.colActions')" width="80" align="center">
                  <template #default="{ $index }">
                    <el-button v-if="$index > 0" size="small" :icon="Delete" @click="removeTarget($index)" />
                  </template>
                </el-table-column>
              </el-table>
              <div class="tiny-hint" style="margin-top: 4px">{{ i18n('syslog.mainTargetHint') }}</div>
            </div>
            <el-form-item :label="i18n('task.sendEncoding')">
              <el-select v-model="sendEncoding" style="width: 120px">
                <el-option v-for="o in ENCODING_OPTIONS" :key="o.value" :label="o.label" :value="o.value" />
              </el-select>
            </el-form-item>

            <!-- 高级配置：默认不启用，直接发送日志正文原文；启用后按所选模式拼接报文头 -->
            <el-form-item :label="i18n('syslog.advanced')">
              <el-switch v-model="syslog.advanced" />
              <span class="tiny-hint" style="margin-left: 8px">{{ i18n(syslog.advanced ? 'syslog.advancedOn' : 'syslog.advancedOff') }}</span>
            </el-form-item>
            <template v-if="syslog.advanced">
              <el-form-item :label="i18n('syslog.mode')">
                <el-select v-model="syslog.mode" style="width: 220px">
                  <el-option label="RFC3164" value="rfc3164" />
                  <el-option :label="i18n('syslog.custom')" value="custom" />
                </el-select>
              </el-form-item>
              <el-form-item v-if="syslog.mode === 'custom'" :label="i18n('syslog.customPrefix')">
                <el-input v-model="syslog.customPrefix" :placeholder="i18n('syslog.customPrefixPlaceholder')" style="width: 420px" />
              </el-form-item>
              <template v-else>
                <el-form-item label="Facility">
                  <el-select v-model="syslog.facility" style="width: 220px">
                    <el-option v-for="f in facilities" :key="f.value" :label="f.label" :value="f.value" />
                  </el-select>
                </el-form-item>
                <el-form-item label="Severity">
                  <el-select v-model="syslog.severity" style="width: 220px">
                    <el-option v-for="s in severities" :key="s.value" :label="s.label" :value="s.value" />
                  </el-select>
                </el-form-item>
                <el-form-item label="Hostname">
                  <el-input v-model="syslog.hostname" placeholder="supersender" />
                </el-form-item>
                <el-form-item label="Tag">
                  <el-input v-model="syslog.tag" placeholder="app" />
                </el-form-item>
              </template>
            </template>
          </el-form>
        </div>
    </template>

    <template #source>
        <!-- 日志来源配置（对齐 UDP 客户端报文配置） -->
        <div class="panel-card">
          <div class="panel-title">{{ i18n('syslog.sourceTitle') }}</div>
          <div class="payload-toolbar">
            <el-select v-model="templateID" :placeholder="i18n('task.templatePlaceholder')" size="small" clearable filterable style="width: 260px" @change="applySelectedTemplate">
              <el-option-group v-for="cat in templateCategories" :key="cat" :label="cat === 'custom' ? i18n('template.customCategory') : cat">
                <el-option v-for="t in templates.byCategory(cat)" :key="t.id" :label="templateOptionLabel(t)" :value="t.id" />
              </el-option-group>
            </el-select>
            <el-button size="small" :icon="FolderOpened" @click="loadFile">{{ i18n('task.loadFile') }}</el-button>
          </div>

          <div class="editor-wrap" :class="{ 'editor-disabled': running }">
            <VueMonacoEditor
              v-model:value="message"
              language="plaintext"
              :theme="editorTheme"
              :options="editorOptions"
              height="240px"
            />
          </div>

          <el-form size="small" label-width="80px" :show-message="false" :disabled="running" style="margin-top: 8px">
            <el-form-item :label="i18n('task.readStrategy')">
              <el-radio-group v-model="cfg.readStrategy">
                <el-radio v-for="o in READ_STRATEGY_OPTIONS" :key="o.value" :label="o.value">{{ i18n(o.labelKey) }}</el-radio>
              </el-radio-group>
            </el-form-item>
            <el-form-item :label="i18n('task.paramEnabled')">
              <el-switch v-model="cfg.paramEnabled" />
              <span class="tiny-hint" style="margin-left: 8px">{{ i18n('task.paramHint') }}</span>
            </el-form-item>
            <el-form-item :label="i18n('syslog.preview')">
              <div class="preview">{{ preview }}</div>
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
/* 多目标 */
.multi-targets {
  margin: 0 0 18px;
}
.multi-targets-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 6px;
  font-size: 12px;
  color: var(--panel-text-secondary);
}

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

/* 报文来源：模板工具栏与编辑器（对齐 UDP 客户端） */
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
.preview {
  width: 100%;
  min-height: 42px;
  max-height: 154px;
  overflow-y: auto;
  padding: 6px 8px;
  border: 1px solid var(--panel-border);
  border-radius: 4px;
  background: var(--code-bg);
  font-family: "Cascadia Code", Consolas, "Courier New", monospace;
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--panel-text);
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
