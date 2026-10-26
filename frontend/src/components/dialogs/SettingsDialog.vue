<script setup lang="ts">
// 全局设置：左右结构 —— 左侧设置分类导航，右侧独占展示当前分类配置；
// 点击左侧 tab 切换右侧内容（v-show 独占展示，不混在一起）。
// 新增分类只需在 categories 中登记一项并追加一个 <section>，便于后续拓展。
import { ref, watch } from 'vue'
import { Brush, FolderOpened } from '@element-plus/icons-vue'
import { useSettingsStore } from '@/stores/settings'
import { ElMessage } from 'element-plus'
import { i18n, SUPPORTED_LOCALES } from '@/i18n'
import * as wails from '@/utils/wails'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [v: boolean] }>()

const settings = useSettingsStore()

// === 设置分类（新增分类：此处加一项 + 模板追加同 id 的 section 即可） ===
const categories = [
  { id: 'appearance', labelKey: 'settings.appearance', icon: Brush },
  { id: 'logs', labelKey: 'settings.logs', icon: FolderOpened }
] as const

type CategoryId = (typeof categories)[number]['id']
const activeCat = ref<CategoryId>('appearance')

// 默认日志目录（后端提供，用于提示与"恢复默认"说明）
const defaultLogDir = ref('')

// 点击左侧分类：切换右侧展示
function selectCat(id: CategoryId) {
  activeCat.value = id
}

// 每次打开弹窗重置到首个分类，并按需拉取默认日志目录
watch(
  () => props.visible,
  async (v) => {
    if (!v) return
    activeCat.value = categories[0].id
    if (defaultLogDir.value) return
    try {
      defaultLogDir.value = await wails.getDefaultLogDir()
    } catch {
      defaultLogDir.value = ''
    }
  }
)

// 选择日志目录（取消时保持不变）
async function onBrowseLogDir() {
  try {
    const dir = await wails.pickDirectory(i18n('settings.logDir'))
    if (dir) settings.logDir = dir
  } catch (e) {
    console.warn('选择日志目录失败：', e)
  }
}

// 恢复默认目录（留空 = 后端默认目录）
function onResetLogDir() {
  settings.logDir = ''
}

async function onSave() {
  settings.applyTheme()
  const ok = await settings.save()
  if (ok) ElMessage.success(i18n('settings.saved'))
  else ElMessage.error(i18n('settings.saveFailed'))
  emit('update:visible', false)
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    :title="i18n('settings.title')"
    width="680px"
    class="settings-dialog"
  >
    <div class="settings-body">
      <!-- 左侧：设置分类导航 -->
      <nav class="settings-nav">
        <button
          v-for="c in categories"
          :key="c.id"
          type="button"
          class="nav-item"
          :class="{ active: activeCat === c.id }"
          @click="selectCat(c.id)"
        >
          <el-icon class="nav-icon"><component :is="c.icon" /></el-icon>
          <span>{{ i18n(c.labelKey) }}</span>
        </button>
      </nav>

      <!-- 右侧：当前分类配置（独占展示） -->
      <div class="settings-content">
        <section v-show="activeCat === 'appearance'" class="setting-section">
          <div class="section-title">{{ i18n('settings.appearance') }}</div>
          <el-form size="small" label-width="90px">
            <el-form-item :label="i18n('settings.theme')">
              <el-radio-group :model-value="settings.theme" @update:model-value="settings.theme = $event">
                <el-radio label="light">{{ i18n('settings.light') }}</el-radio>
                <el-radio label="dark">{{ i18n('settings.dark') }}</el-radio>
              </el-radio-group>
            </el-form-item>
            <el-form-item :label="i18n('settings.language')">
              <el-radio-group v-model="settings.language">
                <el-radio v-for="o in SUPPORTED_LOCALES" :key="o.value" :label="o.value">{{ o.label }}</el-radio>
              </el-radio-group>
            </el-form-item>
          </el-form>
        </section>

        <section v-show="activeCat === 'logs'" class="setting-section">
          <div class="section-title">{{ i18n('settings.logs') }}</div>
          <el-form size="small" label-width="90px">
            <el-form-item :label="i18n('settings.logDir')">
              <div class="logdir-row">
                <el-input
                  v-model="settings.logDir"
                  clearable
                  :placeholder="i18n('settings.logDirPlaceholder')"
                />
                <el-button :icon="FolderOpened" @click="onBrowseLogDir">{{ i18n('settings.browse') }}</el-button>
                <el-button @click="onResetLogDir">{{ i18n('settings.resetDefault') }}</el-button>
              </div>
              <div class="logdir-hint">{{ i18n('settings.logDirDefault', { path: defaultLogDir || '—' }) }}</div>
            </el-form-item>
          </el-form>
        </section>
      </div>
    </div>

    <template #footer>
      <el-button @click="emit('update:visible', false)">{{ i18n('common.cancel') }}</el-button>
      <el-button type="primary" @click="onSave">{{ i18n('common.save') }}</el-button>
    </template>
  </el-dialog>
</template>

<style scoped lang="scss">
.settings-body {
  display: flex;
  height: 420px;
  margin: -10px -0; // 贴满 dialog body
}

/* 左侧分类导航 */
.settings-nav {
  width: 148px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 8px 10px 8px 0;
  border-right: 1px solid var(--panel-border);
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  height: 38px;
  padding: 0 12px;
  border: none;
  border-radius: 8px;
  background: transparent;
  color: var(--panel-text-secondary);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
  .nav-icon {
    font-size: 15px;
  }
  &:hover {
    background: var(--panel-bg-hover);
    color: var(--panel-text);
  }
  &.active {
    background: var(--accent-soft);
    color: var(--accent);
    font-weight: 600;
  }
}

/* 右侧配置区 */
.settings-content {
  flex: 1;
  min-width: 0;
  padding: 4px 8px 16px 22px;
}
.setting-section {
  // 单分类独占展示，不需要 section 间分隔线
}
.section-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--panel-text);
  margin-bottom: 14px;
  padding-left: 2px;
}

/* 日志目录：输入框 + 浏览/恢复默认按钮 */
.logdir-row {
  display: flex;
  gap: 8px;
  width: 100%;
  :deep(.el-input) {
    flex: 1;
  }
}
.logdir-hint {
  width: 100%;
  margin-top: 6px;
  font-size: 12px;
  color: var(--panel-text-secondary);
  word-break: break-all;
}
</style>
