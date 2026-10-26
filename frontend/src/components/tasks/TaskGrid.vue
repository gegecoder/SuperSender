<script setup lang="ts">
// 任务卡片可拖拽布局外壳（基于开源栅格库 grid-layout-plus）：
// - 顶部为固定按钮栏（非栅格项，不参与拖拽）：编辑布局 / 保存布局 / 重置 / 取消
// - 编辑模式：:is-draggable="true" :is-resizable="true"，drag-grip 可拖拽，右下角手柄可缩放
// - 非编辑模式：:is-draggable="false" :is-resizable="false"，GridItem 仍由 grid-layout-plus
//   渲染 inline style（transform/width/height/position:absolute），几何与编辑模式完全一致，
//   所见即所得，无需自行用 CSS Grid 计算几何
// - 编辑中产生草稿布局，点「保存」才持久化到 localStorage，点「取消」回退到上次保存
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Edit, Check, RefreshLeft, Close } from '@element-plus/icons-vue'
import { GridLayout, GridItem } from 'grid-layout-plus'
import type { Layout } from 'grid-layout-plus'
import { useTaskLayout } from '@/composables/useTaskLayout'
import { i18n } from '@/i18n'

const props = defineProps<{
  /** 布局持久化标识，一般为 协议_模式（如 udp_client） */
  layoutKey: string
  /** 默认布局（含每张卡的 x/y/w/h 与最小尺寸约束） */
  defaults: Layout
}>()

const COL_NUM = 12
const ROW_HEIGHT = 40
const MARGIN = 14

const { layout, persist, resetLayout } = useTaskLayout(props.layoutKey, props.defaults)

// 是否处于编辑模式（仅此时可拖拽/缩放）
const editing = ref(false)
// 编辑中的草稿布局（保存前不影响已保存的 layout）
const draft = ref<Layout>([])

// 把 layoutKey（如 udp_client）转为本地化任务类型名（如 UDP 客户端 / UDP Client）
const taskLabel = computed(() => {
  const [proto, mode] = props.layoutKey.split('_')
  return i18n(`proto.${proto}${mode === 'server' ? 'Server' : 'Client'}`)
})

/* ---------- 编辑 / 保存 / 取消 ---------- */
function enterEdit() {
  draft.value = layout.value.map((it) => ({ ...it }))
  editing.value = true
}

function onSave() {
  layout.value = draft.value.map((it) => ({ ...it }))
  persist(layout.value)
  editing.value = false
  ElMessage.success(i18n('task.layoutSaved'))
}

function onCancel() {
  editing.value = false
  draft.value = []
}

function onReset() {
  resetLayout()
  if (editing.value) {
    draft.value = layout.value.map((it) => ({ ...it }))
  }
  ElMessage.success(i18n('task.layoutResetDone'))
}

function onLayoutUpdated(next: Layout | undefined) {
  if (!next || !editing.value) return
  draft.value = next
  // 编辑布局时实时打印当前 layout 信息（调试用）
  console.log('[TaskGrid] layoutKey:', props.layoutKey, 'current layout:', JSON.stringify(next.map((it) => ({ i: it.i, x: it.x, y: it.y, w: it.w, h: it.h }))))
}

watch(
  () => props.layoutKey,
  () => {
    editing.value = false
    draft.value = []
  }
)
</script>

<template>
  <div class="task-grid-wrap">
    <!-- 顶部固定按钮栏 -->
    <div class="layout-toolbar">
      <div class="toolbar-hint">
        <el-icon v-if="editing" class="hint-icon">
          <Edit />
        </el-icon>
        <template v-if="editing">{{ i18n('task.editLayoutHint') }}</template>
        <template v-else>{{ taskLabel }}</template>
      </div>
      <div class="toolbar-actions">
        <el-button v-if="!editing" size="small" type="primary" plain :icon="Edit" :title="i18n('task.editLayoutTitle')" @click="enterEdit">{{ i18n('task.editLayout') }}</el-button>
        <template v-else>
          <el-button size="small" type="primary" :icon="Check" @click="onSave">{{ i18n('task.saveLayout') }}</el-button>
          <el-button size="small" :icon="RefreshLeft" @click="onReset">{{ i18n('task.reset') }}</el-button>
          <el-button size="small" :icon="Close" @click="onCancel">{{ i18n('common.cancel') }}</el-button>
        </template>
      </div>
    </div>

    <!-- 下方区域：编辑/非编辑模式统一用 grid-layout-plus 渲染，GridItem inline style 保证几何一致 -->
    <div class="grid-area" :class="{ 'grid-area--editing': editing }">
      <div class="grid-measure">
        <GridLayout :layout="editing ? draft : layout" :col-num="COL_NUM" :row-height="ROW_HEIGHT"
          :margin="[MARGIN, MARGIN]" :is-draggable="editing" :is-resizable="editing" :vertical-compact="true"
          :use-css-transforms="true" class="task-grid" @layout-updated="onLayoutUpdated">
          <GridItem v-for="item in (editing ? draft : layout)" :key="item.i" :i="item.i" :x="item.x" :y="item.y"
            :w="item.w" :h="item.h" :min-w="item.minW ?? 3" :min-h="item.minH ?? 3" drag-allow-from=".drag-grip"
            class="task-grid-cell" :class="{ 'task-grid-cell--editing': editing }">
            <slot :name="String(item.i)" />
            <!-- 顶部居中拖拽抓手（仅编辑模式显示） -->
            <div v-if="editing" class="drag-grip" :title="i18n('task.dragGrip')">⠿</div>
          </GridItem>
        </GridLayout>
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.task-grid-wrap {
  display: flex;
  flex-direction: column;
  width: 100%;

  --vgl-resizer-border-color: var(--accent);
  --vgl-resizer-size: 8px;
  --vgl-resizer-border-width: 2px;
}

/* 顶部固定按钮栏 */
.layout-toolbar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 8px 8px 8px;
  margin: 10px 10px 0px 15px;
  background: white;
  box-shadow: var(--shadow-sm);
  transition: box-shadow 0.2s ease;
  border: 1px solid var(--panel-border);
  border-radius: 10px;
}

.toolbar-hint {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--panel-text-secondary);

  .hint-icon {
    color: var(--accent);
    font-size: 14px;
  }
}

.toolbar-actions {
  display: flex;
  gap: 8px;
}

/* 下方栅格区域 */
.grid-area {
  flex: 0 0 auto;
  margin: 0px 0px 0px 0px;
  min-width: 0;
  position: relative;
}

/* 编辑模式虚线网格背景：12 列 × 54px 行，作为拖拽对齐参考 */
.grid-area--editing::before {
  content: '';
  position: absolute;
  inset: 0;
  background-image:
    linear-gradient(to right, var(--panel-border-light) 1px, transparent 1px),
    linear-gradient(to bottom, var(--panel-border-light) 1px, transparent 1px);
  background-size: calc((100% + 14px) / 12) 54px;
  background-position: 0 0;
  pointer-events: none;
  z-index: 0;
  border-radius: 8px;
}

.grid-area--editing .task-grid {
  position: relative;
  z-index: 1;
}

.task-grid {
  width: 100%;
}

/* 栅格整体左上偏移 14px：消除库默认 margin 顶部和左侧留白，让第一行/第一列从 0 开始 */
.grid-measure {
  width: 100%;
  margin: 0px 0px 0px 0px;
}

/* 卡片单元格内 panel-card 充满栅格项（GridItem inline style 决定大小） */
:deep(.task-grid-cell) {
  &.vgl-item--dragging {
    z-index: 20;

    .panel-card {
      box-shadow: 0 12px 32px rgba(15, 23, 42, 0.18);
    }
  }

  >.panel-card {
    height: 100%;
    margin-bottom: 0;
    display: flex;
    flex-direction: column;
    overflow-y: auto;
    box-sizing: border-box;
  }
}

/* 顶部居中拖拽抓手：位于卡片内部上边缘中央（往下几 px，避免显示在卡片外）。
   panel-card padding 16px，grip 高 14px + top 2px = 16px 刚好不遮挡 panel-title */
:deep(.drag-grip) {
  position: absolute;
  top: 2px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 15;
  width: 40px;
  height: 14px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 7px;
  background: var(--accent);
  color: #fff;
  font-size: 11px;
  letter-spacing: -1px;
  line-height: 1;
  cursor: grab;
  user-select: none;
  box-shadow: 0 2px 6px rgba(15, 23, 42, 0.25);
  transition: background 0.15s ease, box-shadow 0.15s ease;

  &:hover {
    background: var(--accent-hover, #3d8be8);
    box-shadow: 0 3px 10px rgba(79, 158, 255, 0.45);
  }

  &:active {
    cursor: grabbing;
  }
}

/* 拖拽落点占位符 */
:deep(.vgl-item--placeholder) {
  background: var(--accent-soft);
  border: 1.5px dashed var(--accent);
  border-radius: 12px;
  opacity: 0.7;
  transition: none !important;
}
</style>
