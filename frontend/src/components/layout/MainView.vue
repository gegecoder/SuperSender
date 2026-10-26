<script setup lang="ts">
import { onMounted, ref, watch, computed } from 'vue'
import { Plus, Document, Setting, InfoFilled, Tickets } from '@element-plus/icons-vue'
import SessionSidebar from './SessionSidebar.vue'
import TemplateDialog from '@/components/dialogs/TemplateDialog.vue'
import SettingsDialog from '@/components/dialogs/SettingsDialog.vue'
import AboutDialog from '@/components/dialogs/AboutDialog.vue'
import LogDialog from '@/components/dialogs/LogDialog.vue'
import NewSessionDialog from '@/components/dialogs/NewSessionDialog.vue'
import { useSessionStore } from '@/stores/session'
import { useStatsStore } from '@/stores/stats'
import { useTemplateStore } from '@/stores/template'
import * as wails from '@/utils/wails'
import { i18n } from '@/i18n'
import {
  getTaskComponent,
  taskModeFromProtocol,
  subModeFromProtocol,
  type TaskModeId
} from '@/components/tasks/taskModes'

const session = useSessionStore()
const stats = useStatsStore()
const tmpl = useTemplateStore()

const showTemplates = ref(false)
const showSettings = ref(false)
const showAbout = ref(false)
const showLogs = ref(false)
const showNewSession = ref(false)

// 当前会话的任务模式（创建时已确定并持久化；旧数据按协议派生兜底）
const currentTaskMode = computed<TaskModeId>(() => {
  const cfg = session.activeConfig
  if (!cfg) return 'udp'
  const saved = cfg.taskMode as TaskModeId
  if (saved === 'udp' || saved === 'tcp' || saved === 'syslog') return saved
  return taskModeFromProtocol(cfg.protocol)
})

// 当前任务组件：由 协议×模式（新建会话时确定）映射到独立任务组件
const taskComponent = computed(() => {
  const cfg = session.activeConfig
  if (!cfg) return null
  return getTaskComponent(currentTaskMode.value, subModeFromProtocol(cfg.protocol))
})

// 统计事件订阅：监听激活会话变化，订阅 stats:<id> 推送
let unsubStats: (() => void) | null = null

function subscribeStats(id: string) {
  if (unsubStats) { unsubStats(); unsubStats = null }
  if (!id) return
  unsubStats = wails.onStats(id, (snap) => {
    stats.update(snap)
  })
}

watch(() => session.activeId, (id) => {
  if (id) subscribeStats(id)
}, { immediate: false })

onMounted(async () => {
  await session.fetchList()
  // 模板列表 + Syslog 选项预拉取，方便下拉立即填充
  try {
    await tmpl.fetchList()
    await tmpl.fetchSyslogOptions()
  } catch (e) {
    console.warn('预拉取模板失败：', e)
  }
  if (session.activeId) subscribeStats(session.activeId)
})
</script>

<template>
  <div class="main-view">
    <!-- 顶部品牌栏 + 按钮栏 -->
    <header class="top-bar">
      <div class="top-bar-left">
        <div class="brand">
          <img class="brand-icon" src="/icon.png" alt="SuperSender" />
          <span class="brand-name">SuperSender</span>
          <span class="brand-sub">{{ i18n('brand.brandSub') }}</span>
        </div>
      </div>

      <div class="top-bar-actions">
        <el-button text class="top-action-btn" :icon="Document" @click="showTemplates = true">
          {{ i18n('topbar.templates') }}
        </el-button>
        <span class="action-divider"></span>
        <el-button text class="top-action-btn" :icon="Setting" @click="showSettings = true">
          {{ i18n('topbar.settings') }}
        </el-button>
        <el-button text class="top-action-btn" :icon="Tickets" @click="showLogs = true">
          {{ i18n('topbar.logs') }}
        </el-button>
        <el-button text class="top-action-btn" :icon="InfoFilled" @click="showAbout = true">
          {{ i18n('topbar.about') }}
        </el-button>
      </div>
    </header>

    <div class="main-body">
      <SessionSidebar @create="showNewSession = true" />
      <div class="main-workspace">
        <div v-if="session.activeId && taskComponent" class="workspace">
          <!-- 任务根组件（由新建会话确定的 协议×模式 映射） -->
          <component :is="taskComponent" />
        </div>
        <div v-else class="welcome">
          <img class="welcome-icon" src="/icon.png" alt="SuperSender" />
          <div class="welcome-title">{{ i18n('brand.welcomeTitle') }}</div>
          <div class="welcome-sub">{{ i18n('brand.welcomeSub') }}</div>
          <el-button type="primary" size="large" :icon="Plus" @click="showNewSession = true" class="welcome-btn">
            {{ i18n('sidebar.newSession') }}
          </el-button>
        </div>
      </div>
    </div>

    <TemplateDialog v-model:visible="showTemplates" />
    <SettingsDialog v-model:visible="showSettings" />
    <AboutDialog v-model:visible="showAbout" />
    <LogDialog v-model:visible="showLogs" />
    <NewSessionDialog v-model:visible="showNewSession" />
  </div>
</template>

<style scoped lang="scss">
.main-view {
  display: flex;
  flex-direction: column;
  width: 100%;
  height: 100%;
  background: var(--panel-bg-secondary);
}

/* 顶部栏 */
.top-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  height: 52px;
  padding: 0 16px;
  background: var(--topbar-bg);
  border-bottom: 1px solid var(--topbar-border);
  box-shadow: var(--shadow-sm);
  flex-shrink: 0;
  z-index: 10;
}
.top-bar-left {
  display: flex;
  align-items: center;
}
.brand {
  display: flex;
  align-items: baseline;
  gap: 8px;
  .brand-icon {
    width: 22px;
    height: 22px;
    align-self: center;
    border-radius: 6px;
    object-fit: contain;
  }
  .brand-name {
    font-size: 16px;
    font-weight: 700;
    color: var(--panel-text);
    letter-spacing: 0.5px;
  }
  .brand-sub {
    font-size: 11px;
    color: var(--panel-text-secondary);
  }
}
.top-bar-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
/* 顶部操作按钮：无边框轻量风格，圆角悬浮块，图标+文字节奏舒适 */
:deep(.top-action-btn) {
  height: 32px;
  padding: 0 12px;
  border-radius: 8px;
  font-size: 13px;
  color: var(--panel-text-secondary);
  gap: 6px;
  transition: background 0.15s ease, color 0.15s ease;
  &:hover {
    background: var(--panel-bg-hover);
    color: var(--accent);
  }
  // 弹窗关闭后焦点会归还触发按钮，鼠标场景下不保留选中底色；
  // 仅键盘导航触发的 :focus-visible 显示焦点态（无障碍）
  &:focus:not(:focus-visible),
  &.is-focus:not(:focus-visible) {
    background: transparent;
    color: var(--panel-text-secondary);
    outline: none;
  }
  &:focus-visible {
    background: var(--panel-bg-hover);
    color: var(--accent);
    outline: 2px solid var(--el-color-primary-light-5, #a0cfff);
    outline-offset: -1px;
  }
  &:active {
    background: var(--accent-soft);
  }
  .el-icon {
    font-size: 15px;
    margin-right: 1px;
  }
}
/* 内容操作（模板）与系统操作（设置/日志/关于）分组分隔 */
.action-divider {
  width: 1px;
  height: 16px;
  margin: 0 6px;
  background: var(--panel-border);
}

/* 主体：侧边栏 + 工作区 */
.main-body {
  flex: 1;
  display: flex;
  min-height: 0;
}
.main-workspace {
  flex: 1;
  overflow: hidden;
  padding: 0px 0px 0px 0px;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.workspace {
  flex: 1;
  min-height: 0;
  // 卡片可自由拖拽摆放，内容总高可变，工作区整体纵向滚动
  overflow-y: auto;
}

/* 欢迎页 */
.welcome {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--panel-text-secondary);
  gap: 10px;
  .welcome-icon {
    width: 64px;
    height: 64px;
    opacity: 0.85;
    border-radius: 16px;
    object-fit: contain;
    box-shadow: 0 10px 24px rgba(79, 158, 255, 0.25);
  }
  .welcome-title { font-size: 26px; font-weight: 700; color: var(--panel-text); }
  .welcome-sub { font-size: 13px; }
  .welcome-btn { margin-top: 16px; }
}
</style>
