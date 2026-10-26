<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import Sortable from 'sortablejs'
import { useSessionStore } from '@/stores/session'
import { i18n } from '@/i18n'
import type { Protocol, SessionInfo } from '@/types'

const emit = defineEmits<{ create: [] }>()

const store = useSessionStore()

// 会话列表变化时，为尚未加载配置的会话拉取配置，
// 使侧边栏能基于实时配置（而非后端静态摘要）显示服务信息
watch(() => store.list, (l) => {
  for (const s of l) {
    if (!store.configMap[s.id]) store.fetchConfig(s.id)
  }
}, { immediate: true })

// === 会话名称内联重命名：点击名称进入编辑，Enter 或失焦保存 ===
const editingId = ref('')
const editName = ref('')
const editInputRef = ref<{ focus: () => void } | null>(null)

async function startRename(s: SessionInfo) {
  editingId.value = s.id
  editName.value = s.name
  await nextTick()
  editInputRef.value?.focus()
}

async function saveRename() {
  const id = editingId.value
  if (!id) return
  const name = editName.value
  editingId.value = ''
  await store.rename(id, name)
}

function cancelRename() {
  editingId.value = ''
}

// 协议简称显示（含 Syslog 任务模式识别）
const PROTOCOL_KEY: Record<Protocol, string> = {
  tcp_client: 'proto.tcpClient',
  tcp_server: 'proto.tcpServer',
  udp_client: 'proto.udpClient',
  udp_server: 'proto.udpServer'
}

function protocolLabel(p: Protocol, taskMode?: string): string {
  if (taskMode === 'syslog') {
    return p.endsWith('_server') ? i18n('proto.syslogServer') : i18n('proto.syslogClient')
  }
  return PROTOCOL_KEY[p] ? i18n(PROTOCOL_KEY[p]) : p
}

// 按当前选择情况动态生成服务信息（协议模式 · 目标/监听地址）
function metaOf(s: SessionInfo): string {
  const cfg = store.configMap[s.id]
  const proto = cfg?.protocol ?? s.protocol
  const label = protocolLabel(proto, cfg?.taskMode)
  // 无实时配置时退回后端摘要
  if (!cfg) return `${label} · ${s.target}`

  if (proto === 'tcp_server' || proto === 'udp_server') {
    const bindIP = cfg.sourceIP?.trim() || '0.0.0.0'
    return `${label} · ${i18n('sidebar.listening')} ${bindIP}:${cfg.targetPort}`
  }
  if (cfg.enableMulti && cfg.targets.length > 0) {
    return `${label} · ${i18n('sidebar.targets', { n: cfg.targets.length })}`
  }
  return `${label} · ${cfg.targetIP}:${cfg.targetPort}`
}

// 状态点：优先用实时状态缓存
function statusOf(s: SessionInfo): string {
  return store.statusMap[s.id] ?? s.status
}

async function onDelete(id: string, name: string) {
  try {
    await ElMessageBox.confirm(i18n('sidebar.deleteConfirm', { name }), i18n('common.tip'), { type: 'warning' })
    await store.remove(id)
  } catch {}
}

// 会话总数
const total = computed(() => store.list.length)

// === 拖拽排序（sortablejs：开源、无框架依赖、Vue3 直接可用） ===
// 仅左侧 ⠿ 手柄可发起拖拽，避免与单击切换、双击改名、删除按钮冲突
const listRef = ref<HTMLElement>()
let sortable: Sortable | null = null

onMounted(() => {
  if (!listRef.value) return
  sortable = Sortable.create(listRef.value, {
    handle: '.drag-handle',
    draggable: '.session-item',
    animation: 180,
    ghostClass: 'session-item--ghost',
    chosenClass: 'session-item--chosen',
    dragClass: 'session-item--drag',
    forceFallback: false,
    onEnd: (evt) => {
      const { oldIndex, newIndex } = evt
      if (oldIndex === undefined || newIndex === undefined || oldIndex === newIndex) return
      // Sortable 已移动 DOM；这里按同样的位置变更重排数据，Vue 会以 key 校正回一致状态
      const ids = store.list.map(s => s.id)
      const [moved] = ids.splice(oldIndex, 1)
      ids.splice(newIndex, 0, moved)
      store.reorder(ids)
    }
  })
})

onBeforeUnmount(() => {
  sortable?.destroy()
  sortable = null
})
</script>

<template>
  <div class="sidebar">
    <div class="sidebar-header">
      <span class="sidebar-title">{{ i18n('sidebar.title') }}</span>
      <span class="sidebar-count">{{ total }}</span>
      <el-tooltip :content="i18n('sidebar.newSession')" placement="bottom" :show-after="300" :hide-after="0">
        <el-button class="new-session-btn" text circle size="small" :icon="Plus" @click="emit('create')" />
      </el-tooltip>
    </div>
    <div ref="listRef" class="sidebar-list">
      <div
        v-for="s in store.list"
        :key="s.id"
        class="session-item"
        :class="{ active: s.id === store.activeId }"
        @click="store.setActive(s.id)"
      >
        <span class="drag-handle" :title="i18n('sidebar.dragTip')" @click.stop>⠿</span>
        <span class="status-dot" :class="statusOf(s)"></span>
        <div class="session-info">
          <el-tooltip v-if="editingId !== s.id" :content="s.name" placement="right" :show-after="400" :hide-after="0">
            <div class="session-name" :title="i18n('sidebar.renameTip')" @dblclick.stop="startRename(s)">{{ s.name }}</div>
          </el-tooltip>
          <el-input
            v-else
            ref="editInputRef"
            v-model="editName"
            size="small"
            class="rename-input"
            maxlength="50"
            @keyup.enter="saveRename"
            @blur="saveRename"
            @keydown.esc="cancelRename"
            @click.stop
          />
          <el-tooltip :content="metaOf(s)" placement="right" :show-after="400" :hide-after="0">
            <div class="session-meta">{{ metaOf(s) }}</div>
          </el-tooltip>
        </div>
        <el-button
          text
          size="small"
          class="session-del"
          @click.stop="onDelete(s.id, s.name)"
        >×</el-button>
      </div>
      <div v-if="store.list.length === 0" class="empty-hint">
        {{ i18n('sidebar.empty') }}
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.sidebar {
  width: 240px;
  height: 100%;
  background: var(--sidebar-bg);
  border-right: 1px solid var(--panel-border);
  display: flex;
  flex-direction: column;
  color: var(--panel-text);
  flex-shrink: 0;
}
.sidebar-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 16px 10px;
  .new-session-btn {
    margin-left: auto;
    color: var(--el-color-primary);
    &:hover { transform: scale(1.1); }
  }
  .sidebar-count {
    margin-left: 2px;
  }
  .sidebar-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--panel-text-secondary);
    text-transform: uppercase;
    letter-spacing: 0.8px;
  }
  .sidebar-count {
    font-size: 11px;
    color: var(--panel-text-secondary);
    background: var(--panel-bg-hover);
    padding: 1px 8px;
    border-radius: 10px;
  }
}
.sidebar-list {
  flex: 1;
  overflow-y: auto;
  padding: 4px 8px 12px;
}
.session-item {
  display: flex;
  align-items: center;
  padding: 10px 12px;
  cursor: pointer;
  gap: 8px;
  border-radius: 8px;
  margin-bottom: 2px;
  transition: background 0.15s ease;
  &:hover { background: var(--panel-bg-hover); }
  &.active {
    background: var(--accent-soft);
    box-shadow: inset 3px 0 0 var(--accent);
  }
  .drag-handle {
    flex-shrink: 0;
    width: 10px;
    font-size: 13px;
    line-height: 1;
    color: var(--panel-text-secondary);
    opacity: 0.25;
    cursor: grab;
    user-select: none;
    transition: opacity 0.15s ease, color 0.15s ease;
    &:hover { opacity: 1; color: var(--accent); }
    &:active { cursor: grabbing; }
  }
  &:hover .drag-handle { opacity: 0.7; }
  &.session-item--ghost {
    opacity: 0.45;
    background: var(--accent-soft);
    outline: 1px dashed var(--accent);
    outline-offset: -1px;
  }
  &.session-item--chosen {
    background: var(--accent-soft);
  }
  .session-info { flex: 1; min-width: 0; }
  .session-name {
    font-size: 13px;
    color: var(--panel-text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-weight: 500;
    cursor: text;
  }
  .rename-input {
    margin: -2px 0;
    :deep(.el-input__wrapper) {
      padding: 0 6px;
    }
    :deep(.el-input__inner) {
      font-size: 12px;
      height: 22px;
    }
  }
  .session-meta {
    font-size: 11px;
    color: var(--panel-text-secondary);
    margin-top: 3px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .session-del {
    opacity: 0;
    transition: opacity 0.15s ease;
    &:hover { color: var(--status-error); }
  }
  &:hover .session-del { opacity: 0.6; }
}
.empty-hint {
  padding: 32px 16px;
  text-align: center;
  color: var(--panel-text-secondary);
  font-size: 12px;
  line-height: 1.6;
}
</style>
