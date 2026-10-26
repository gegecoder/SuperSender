<script setup lang="ts">
// 新建会话：单屏完成 —— 协议 / 模式 / 名称一次选好，不再分步切换。
// 协议与模式在创建时确定，创建后各任务界面不再提供切换。
import { ref, watch } from 'vue'
import { Download, Upload } from '@element-plus/icons-vue'
import { useSessionStore } from '@/stores/session'
import { TASK_MODES, getTaskMode, type TaskModeId } from '@/components/tasks/taskModes'
import { i18n } from '@/i18n'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [v: boolean] }>()

const session = useSessionStore()

const protoChoice = ref<TaskModeId>('syslog')
const mode = ref<'client' | 'server'>('client')
const name = ref('')
const creating = ref(false)

// 打开时重置选择，并按已有会话序号自动生成默认名称
watch(
  () => props.visible,
  (v) => {
    if (!v) return
    protoChoice.value = 'syslog'
    mode.value = 'client'
    name.value = defaultName()
  }
)

// 默认名称：取现有「会话 N」的最大序号 +1，避免重复（前缀随界面语言变化）
function defaultName(): string {
  const prefix = i18n('newSession.defaultNamePrefix')
  const re = new RegExp(`^${prefix}\\s*(\\d+)$`)
  let max = 0
  for (const s of session.list) {
    const m = re.exec(s.name)
    if (m) max = Math.max(max, parseInt(m[1], 10))
  }
  return `${prefix} ${max + 1}`
}

async function submit() {
  if (creating.value) return
  creating.value = true
  try {
    const def = getTaskMode(protoChoice.value)
    await session.create(name.value.trim() || undefined, {
      taskMode: def.id,
      protocol: def.protocol(mode.value)
    })
    emit('update:visible', false)
  } finally {
    creating.value = false
  }
}

// 供模板遍历的协议选项
const protoOptions = TASK_MODES
</script>

<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    width="760px"
    :title="i18n('newSession.title')"
    align-center
    :close-on-click-modal="false"
  >
    <div class="ns-body">
      <!-- 选择协议 -->
      <div class="ns-section">
        <div class="ns-label">{{ i18n('newSession.selectProtocol') }}</div>
        <div class="proto-grid">
          <div
            v-for="m in protoOptions"
            :key="m.id"
            class="opt-card"
            :class="{ selected: protoChoice === m.id }"
            @click="protoChoice = m.id"
          >
            <div class="opt-top">
              <span class="opt-icon"><el-icon><component :is="m.icon" /></el-icon></span>
              <span class="opt-label">{{ m.label }}</span>
            </div>
            <span class="opt-desc">{{ i18n(m.descKey) }}</span>
          </div>
        </div>
      </div>

      <!-- 选择模式 -->
      <div class="ns-section">
        <div class="ns-label">{{ i18n('newSession.selectMode') }}</div>
        <div class="mode-grid">
          <div class="opt-card" :class="{ selected: mode === 'client' }" @click="mode = 'client'">
            <div class="opt-top">
              <span class="opt-icon"><el-icon><Upload /></el-icon></span>
              <span class="opt-label">{{ i18n('newSession.client') }}</span>
            </div>
            <span class="opt-desc">{{ i18n(protoChoice === 'syslog' ? 'newSession.clientDescSyslog' : 'newSession.clientDescPacket') }}</span>
          </div>
          <div class="opt-card" :class="{ selected: mode === 'server' }" @click="mode = 'server'">
            <div class="opt-top">
              <span class="opt-icon"><el-icon><Download /></el-icon></span>
              <span class="opt-label">{{ i18n('newSession.server') }}</span>
            </div>
            <span class="opt-desc">{{ i18n(protoChoice === 'syslog' ? 'newSession.serverDescSyslog' : 'newSession.serverDescPacket') }}</span>
          </div>
        </div>
      </div>

      <!-- 会话名称 -->
      <div class="ns-section">
        <div class="ns-label">{{ i18n('newSession.name') }}</div>
        <el-input v-model="name" :placeholder="i18n('newSession.namePlaceholder')" maxlength="50" clearable size="large" @keyup.enter="submit" />
      </div>
    </div>

    <template #footer>
      <div class="ns-footer">
        <el-button @click="emit('update:visible', false)">{{ i18n('common.cancel') }}</el-button>
        <el-button type="primary" :loading="creating" @click="submit">{{ i18n('newSession.create') }}</el-button>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.ns-body {
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 4px 6px 2px;
}
.ns-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.ns-label {
  font-size: 13px;
  font-weight: 700;
  color: var(--panel-text);
  &::before {
    content: '';
    display: inline-block;
    width: 3px;
    height: 12px;
    margin-right: 8px;
    border-radius: 2px;
    background: var(--el-color-primary);
    vertical-align: -1px;
  }
}

/* 选项卡片：上行「图标+名称」、下行「说明」，统一高度 */
.proto-grid,
.mode-grid {
  display: flex;
  gap: 14px;
}
.opt-card {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
  box-sizing: border-box;
  height: 90px;
  padding: 12px 16px;
  border: 1px solid var(--panel-border);
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
  background: var(--panel-bg);
  &:hover {
    border-color: var(--el-color-primary);
    box-shadow: 0 6px 18px rgba(79, 158, 255, 0.18);
    transform: translateY(-2px);
  }
  &.selected {
    border-color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
    box-shadow: 0 0 0 1px var(--el-color-primary) inset;
  }
}
.opt-top {
  display: flex;
  align-items: center;
  gap: 10px;
}
/* 图标固定尺寸徽章：使用 SVG 图标（不依赖系统 emoji 字体），视觉大小一致 */
.opt-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  border-radius: 8px;
  background: rgba(79, 158, 255, 0.12);
  color: var(--el-color-primary);
  font-size: 17px;
  line-height: 1;
}
.opt-label {
  font-size: 15px;
  font-weight: 700;
  color: var(--panel-text);
}
.opt-desc {
  font-size: 12px;
  color: var(--panel-text-secondary);
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 底部按钮 */
.ns-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
