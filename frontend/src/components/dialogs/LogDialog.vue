<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'
import { ElMessage } from 'element-plus'
import { Delete, CopyDocument, FolderOpened } from '@element-plus/icons-vue'
import * as wails from '@/utils/wails'
import { i18n } from '@/i18n'
import type { LogEntry } from '@/types'

// 运行日志浮层：控制台风格日志查看，支持自动刷新、清空、复制、打开日志目录
const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [v: boolean] }>()

const logs = ref<LogEntry[]>([])
const autoScroll = ref(true)
const autoRefresh = ref(true)
// 当前日志持久化目录（后端提供）
const logDir = ref('')
let timer: ReturnType<typeof setInterval> | null = null
const listEl = ref<HTMLElement | null>(null)

// 拉取当前日志目录
async function refreshDir() {
  try {
    logDir.value = await wails.getLogDir()
  } catch {
    logDir.value = ''
  }
}

// 在系统文件管理器中打开日志目录
async function onOpenDir() {
  try {
    const res = await wails.openLogDir()
    if (!res.success) ElMessage.error(i18n('logs.openDirFailed'))
  } catch {
    ElMessage.error(i18n('logs.openDirFailed'))
  }
}

async function refresh() {
  try {
    logs.value = await wails.getRecentLogs()
    if (autoScroll.value) {
      await nextTick()
      if (listEl.value) listEl.value.scrollTop = listEl.value.scrollHeight
    }
  } catch (e) {
    console.warn('获取日志失败：', e)
  }
}

watch(
  () => props.visible,
  (v) => {
    if (v) {
      refreshDir()
      refresh()
      if (autoRefresh.value) startTimer()
    } else {
      stopTimer()
    }
  }
)

watch(autoRefresh, (v) => {
  if (v && props.visible) startTimer()
  else stopTimer()
})

function startTimer() {
  if (timer) return
  timer = setInterval(refresh, 1000)
}
function stopTimer() {
  if (timer) {
    clearInterval(timer)
    timer = null
  }
}

async function onClear() {
  try {
    await wails.clearLogs()
    logs.value = []
  } catch (e) {
    ElMessage.error(i18n('logs.clearFailed', { reason: String(e) }))
  }
}

async function onCopyAll() {
  const text = logs.value.map((l) => `[${l.time}] [${l.level.toUpperCase()}] ${l.message}`).join('\n')
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(i18n('logs.copied'))
  } catch {
    ElMessage.error(i18n('logs.copyFailed'))
  }
}

onBeforeUnmount(stopTimer)
</script>

<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    :title="i18n('logs.title')"
    width="780px"
    top="6vh"
  >
    <div class="log-dialog-body">
      <!-- 工具栏 -->
      <div class="log-toolbar">
        <div class="log-toolbar-left">
          <span class="log-dot"></span>
          <span class="log-count">{{ i18n('logs.count', { n: logs.length }) }}</span>
          <el-checkbox v-model="autoScroll" size="small">{{ i18n('logs.autoScroll') }}</el-checkbox>
          <el-checkbox v-model="autoRefresh" size="small">{{ i18n('logs.autoRefresh') }}</el-checkbox>
        </div>
        <div>
          <el-button size="small" :icon="FolderOpened" @click="onOpenDir">{{ i18n('logs.openDir') }}</el-button>
          <el-button size="small" :icon="CopyDocument" @click="onCopyAll">{{ i18n('logs.copyAll') }}</el-button>
          <el-button size="small" type="danger" plain :icon="Delete" @click="onClear">{{ i18n('logs.clear') }}</el-button>
        </div>
      </div>
      <!-- 日志目录 -->
      <div class="log-dir-row">
        <el-icon><FolderOpened /></el-icon>
        <span class="log-dir-path" :title="logDir">{{ i18n('logs.dirTitle', { path: logDir || '—' }) }}</span>
      </div>
      <!-- 日志列表 -->
      <div ref="listEl" class="log-list">
        <div v-if="logs.length === 0" class="log-empty">{{ i18n('logs.empty') }}</div>
        <div
          v-for="(l, i) in logs"
          :key="i"
          class="log-row"
          :class="l.level"
        >
          <span class="log-time">{{ l.time }}</span>
          <span class="log-level">{{ l.level.toUpperCase() }}</span>
          <span class="log-msg">{{ l.message }}</span>
        </div>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped lang="scss">
.log-dialog-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.log-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  .log-toolbar-left {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .log-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #67c23a;
    box-shadow: 0 0 6px rgba(103, 194, 58, 0.8);
  }
  .log-count {
    font-size: 12px;
    color: var(--panel-text-secondary, #909399);
  }
}
/* 日志目录展示 */
.log-dir-row {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  font-size: 12px;
  color: var(--panel-text-secondary, #909399);
  .log-dir-path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: 'Cascadia Code', Consolas, 'SF Mono', monospace;
  }
}
/* 控制台风格日志区 */
.log-list {
  height: 460px;
  overflow-y: auto;
  background: #0d1117;
  border: 1px solid var(--panel-border, #30363d);
  border-radius: 8px;
  padding: 8px 0;
  font-family: 'Cascadia Code', Consolas, 'SF Mono', monospace;
  font-size: 12.5px;
  line-height: 1.7;
}
.log-row {
  display: flex;
  gap: 10px;
  padding: 0 12px;
  align-items: baseline;
  &:hover {
    background: rgba(177, 186, 196, 0.08);
  }
  .log-time {
    flex-shrink: 0;
    color: #7d8590;
  }
  .log-level {
    flex-shrink: 0;
    width: 44px;
    font-weight: 700;
    font-size: 11px;
  }
  .log-msg {
    word-break: break-all;
    white-space: pre-wrap;
    color: #c9d1d9;
  }
  &.info .log-level { color: #58a6ff; }
  &.warn {
    .log-level { color: #e3b341; }
    .log-msg { color: #e3b341; }
  }
  &.error {
    .log-level { color: #f85149; }
    .log-msg { color: #f85149; }
  }
}
.log-empty {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #7d8590;
  font-size: 13px;
}
/* 滚动条 */
.log-list::-webkit-scrollbar {
  width: 8px;
}
.log-list::-webkit-scrollbar-thumb {
  background: #30363d;
  border-radius: 4px;
}
.log-list::-webkit-scrollbar-track {
  background: transparent;
}
</style>
