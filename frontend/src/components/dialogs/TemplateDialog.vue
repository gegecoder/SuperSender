<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document, DocumentCopy, Search } from '@element-plus/icons-vue'
import * as wails from '@/utils/wails'
import { useTemplateStore } from '@/stores/template'
import { i18n } from '@/i18n'
import type { Template } from '@/types'

const props = defineProps<{ visible: boolean }>()
const emit = defineEmits<{ 'update:visible': [v: boolean] }>()

const store = useTemplateStore()
const editing = ref<Template | null>(null)

// 列表检索关键词：按 分类/名称/厂商/产品/型号/版本 模糊过滤（不区分大小写）
const keyword = ref('')
const filteredList = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return store.list
  return store.list.filter(t =>
    [t.name, t.category, t.vendor, t.product, t.model, t.version].some(
      v => (v || '').toLowerCase().includes(kw)
    )
  )
})

// 分类选项从模板列表动态推导（含 custom 内置分类），支持手动输入新分类
const categoryOptions = computed(() => {
  const set = new Set<string>(['custom'])
  for (const t of store.list) {
    if (t.category) set.add(t.category)
  }
  return ['custom', ...Array.from(set).filter(c => c !== 'custom').sort((a, b) => a.localeCompare(b))]
})

function categoryLabel(cat: string) {
  return cat === 'custom' ? i18n('template.customCategory') : cat
}

const showVarHelp = ref(false)
// 变量说明表格数据（与后端 template.Render 支持的变量一致），desc/example 使用 i18n key
const VAR_DOCS = [
  { name: '${seq}', descKey: 'template.varSeqDesc', exampleKey: 'template.varSeqExample' },
  { name: '${random_ip}', descKey: 'template.varRandomIpDesc', exampleKey: 'template.varRandomIpExample' },
  { name: '${random_port}', descKey: 'template.varRandomPortDesc', exampleKey: 'template.varRandomPortExample' },
  { name: '${random_int(min,max)}', descKey: 'template.varRandomIntDesc', exampleKey: 'template.varRandomIntExample' },
  { name: '${random_hex(len)}', descKey: 'template.varRandomHexDesc', exampleKey: 'template.varRandomHexExample' },
  { name: '${random_str(len)}', descKey: 'template.varRandomStrDesc', exampleKey: 'template.varRandomStrExample' },
  { name: '${random_time}', descKey: 'template.varRandomTimeDesc', exampleKey: 'template.varRandomTimeExample' },
  { name: '${date}', descKey: 'template.varDateDesc', exampleKey: 'template.varDateExample' },
  { name: '${datetime}', descKey: 'template.varDatetimeDesc', exampleKey: 'template.varDatetimeExample' },
  { name: '${timestamp}', descKey: 'template.varTimestampDesc', exampleKey: 'template.varTimestampExample' },
  { name: '${timestamp_ms}', descKey: 'template.varTimestampMsDesc', exampleKey: 'template.varTimestampMsExample' },
  { name: '${uuid}', descKey: 'template.varUuidDesc', exampleKey: 'template.varUuidExample' }
]

// 变量说明表格数据（desc/example 渲染时翻译）
const varDocs = computed(() =>
  VAR_DOCS.map(v => ({ name: v.name, desc: i18n(v.descKey), example: i18n(v.exampleKey) }))
)

function open() {
  store.fetchList()
  editing.value = null
}

function startNew() {
  editing.value = {
    id: '',
    name: '',
    category: 'custom',
    content: '',
    builtin: false,
    vendor: '',
    product: '',
    model: '',
    version: '',
    createdAt: '',
    updatedAt: ''
  }
}

function edit(t: Template) {
  // 内置模板允许复制编辑（保存为新 id）
  if (t.builtin) {
    editing.value = { ...t, id: '', name: t.name + i18n('template.copySuffix'), builtin: false }
  } else {
    editing.value = { ...t }
  }
}

async function save() {
  if (!editing.value) return
  if (!editing.value.name || !editing.value.content) {
    ElMessage.warning(i18n('template.required'))
    return
  }
  const saved = await store.save(editing.value)
  if (saved) {
    ElMessage.success(i18n('template.saved'))
    editing.value = null
  }
}

async function remove(id: string) {
  try {
    await ElMessageBox.confirm(i18n('template.deleteConfirm'), i18n('common.tip'), { type: 'warning' })
    const ok = await store.remove(id)
    if (ok) ElMessage.success(i18n('template.deleted'))
  } catch {}
}

// 复制模板报文到剪贴板（优先 Clipboard API，失败回退 execCommand）
async function copyPayload(t: Template) {
  if (!t.content) return
  try {
    await navigator.clipboard.writeText(t.content)
  } catch {
    const ta = document.createElement('textarea')
    ta.value = t.content
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
  ElMessage.success(i18n('template.copySuccess', { name: t.name }))
}

function close() {
  emit('update:visible', false)
}

// 导出模板：系统保存对话框选定路径 → 后端序列化写入 JSON 文件
async function onExport() {
  const path = await wails.saveFileDialog(i18n('template.exportTitle'), 'templates-export.json')
  if (!path) return
  const res = await store.exportAll(path)
  if (res.success) ElMessage.success(i18n('template.exported'))
  else ElMessage.error(i18n('template.exportFailed'))
}

// 导入模板：系统打开对话框选定 JSON 文件 → 后端合并导入（ID 冲突时生成新 ID）
async function onImport() {
  const path = await wails.openFileDialog(i18n('template.importTitle'))
  if (!path) return
  const res = await store.importAll(path)
  if (res.success) ElMessage.success(i18n('template.imported'))
  else ElMessage.error(i18n('template.importFailed'))
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    @update:model-value="emit('update:visible', $event)"
    :title="i18n('template.title')"
    width="920px"
    top="6vh"
    @open="open"
  >
    <div class="tmpl-layout">
      <!-- 左：模板列表 -->
      <div class="tmpl-list">
        <div class="tmpl-list-head">
          <span class="tmpl-list-title">{{ i18n('template.allTemplates') }}</span>
          <div class="tmpl-list-actions">
            <el-button size="small" @click="showVarHelp = true">{{ i18n('template.varHelp') }}</el-button>
            <el-button type="primary" size="small" @click="startNew">{{ i18n('template.new') }}</el-button>
          </div>
        </div>
        <div class="tmpl-search-row">
          <el-input
            v-model="keyword"
            size="small"
            clearable
            class="tmpl-search"
            :placeholder="i18n('template.searchPlaceholder')"
            :prefix-icon="Search"
          />
          <el-button size="small" @click="onImport">{{ i18n('template.import') }}</el-button>
          <el-button size="small" @click="onExport">{{ i18n('template.export') }}</el-button>
        </div>
        <div class="tmpl-items">
          <div
            v-for="tpl in filteredList"
            :key="tpl.id"
            class="tmpl-card"
            :class="{ selected: editing && editing.id === tpl.id }"
            @click="edit(tpl)"
          >
            <div class="tmpl-card-top">
              <span v-if="tpl.builtin" class="builtin-tag">{{ i18n('template.builtin') }}</span>
              <span class="tmpl-name" :title="tpl.name">{{ tpl.name }}</span>
              <el-tooltip :content="i18n('template.copyPayload')" placement="top" :show-after="300" :hide-after="0">
                <el-button
                  text
                  size="small"
                  class="copy-btn"
                  :icon="DocumentCopy"
                  @click.stop="copyPayload(tpl)"
                />
              </el-tooltip>
            </div>
            <div class="tmpl-card-bottom">
              <span class="tmpl-cat">{{ tpl.category }}</span>
              <span class="tmpl-size">{{ i18n('template.chars', { n: tpl.content.length }) }}</span>
              <el-button v-if="!tpl.builtin" text size="small" type="danger" class="del-btn" @click.stop="remove(tpl.id)">{{ i18n('template.delete') }}</el-button>
            </div>
          </div>
          <div v-if="!filteredList.length" class="tmpl-no-match">{{ i18n('template.noMatch') }}</div>
        </div>
      </div>

      <!-- 右：编辑 / 预览 -->
      <div class="tmpl-editor">
        <template v-if="editing">
          <div class="editor-head">
            <span class="editor-title">{{ i18n(editing.id ? 'template.editTitle' : 'template.newTitle') }}</span>
            <div>
              <el-button size="small" @click="copyPayload(editing)" :icon="DocumentCopy">{{ i18n('template.copy') }}</el-button>
              <el-button size="small" @click="editing = null">{{ i18n('common.cancel') }}</el-button>
              <el-button type="primary" size="small" @click="save">{{ i18n('common.save') }}</el-button>
            </div>
          </div>
          <el-form size="small" label-width="60px" class="editor-form">
            <el-form-item :label="i18n('template.name')">
              <el-input v-model="editing.name" :placeholder="i18n('template.namePlaceholder')" />
            </el-form-item>
            <el-form-item :label="i18n('template.category')">
              <el-select
                v-model="editing.category"
                style="width: 100%"
                filterable
                allow-create
                default-first-option
                :placeholder="i18n('template.categoryPlaceholder')"
              >
                <el-option v-for="cat in categoryOptions" :key="cat" :label="categoryLabel(cat)" :value="cat" />
              </el-select>
            </el-form-item>
            <el-row :gutter="8">
              <el-col :span="12">
                <el-form-item :label="i18n('template.vendor')">
                  <el-input v-model="editing.vendor" :placeholder="i18n('template.vendorPlaceholder')" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="i18n('template.product')">
                  <el-input v-model="editing.product" :placeholder="i18n('template.productPlaceholder')" />
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="8">
              <el-col :span="12">
                <el-form-item :label="i18n('template.model')">
                  <el-input v-model="editing.model" :placeholder="i18n('template.modelPlaceholder')" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="i18n('template.version')">
                  <el-input v-model="editing.version" :placeholder="i18n('template.versionPlaceholder')" />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item :label="i18n('template.content')">
              <el-input
                v-model="editing.content"
                type="textarea"
                :rows="14"
                class="content-area"
                :placeholder="i18n('template.contentPlaceholder')"
              />
            </el-form-item>
          </el-form>
        </template>
        <div v-else class="tmpl-empty">
          <div class="empty-icon"><el-icon><Document /></el-icon></div>
          <div>{{ i18n('template.emptyHint') }}</div>
          <div class="empty-tip">{{ i18n('template.emptyTip') }}</div>
        </div>
      </div>
    </div>

    <!-- 变量说明弹窗 -->
    <el-dialog
      v-model="showVarHelp"
      :title="i18n('template.varTitle')"
      width="800px"
      append-to-body
    >
      <div class="var-help-tip">
        {{ i18n('template.varTip') }}
      </div>
      <el-table :data="varDocs" size="small" border>
        <el-table-column prop="name" :label="i18n('template.varColName')" width="200">
          <template #default="{ row }">
            <code class="var-code">{{ row.name }}</code>
          </template>
        </el-table-column>
        <el-table-column prop="desc" :label="i18n('template.varColDesc')" min-width="240" />
        <el-table-column prop="example" :label="i18n('template.varColExample')" min-width="180">
          <template #default="{ row }">
            <span class="var-example">{{ row.example }}</span>
          </template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="showVarHelp = false">{{ i18n('common.close') }}</el-button>
      </template>
    </el-dialog>
  </el-dialog>
</template>

<style scoped lang="scss">
.tmpl-layout {
  display: flex;
  gap: 14px;
  height: 560px;
}

/* 左侧列表 */
.tmpl-list {
  width: 280px;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--panel-border);
  padding-right: 12px;
}
.tmpl-list-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
  .tmpl-list-title {
    font-size: 12px;
    font-weight: 600;
    color: var(--panel-text-secondary);
    letter-spacing: 0.5px;
  }
  .tmpl-list-actions {
    display: flex;
    gap: 6px;
  }
}
/* 检索 + 导入导出工具行 */
.tmpl-search-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
  .tmpl-search {
    flex: 1;
    min-width: 0;
  }
}
.tmpl-no-match {
  padding: 18px 0;
  text-align: center;
  font-size: 12px;
  color: var(--panel-text-secondary);
}

/* 变量说明弹窗 */
.var-help-tip {
  font-size: 12.5px;
  color: var(--panel-text-secondary);
  margin-bottom: 10px;
  line-height: 1.6;
}
.var-code {
  font-family: Consolas, 'SFMono-Regular', Menlo, monospace;
  font-size: 12px;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  padding: 1px 6px;
  border-radius: 4px;
}
.var-example {
  font-family: Consolas, 'SFMono-Regular', Menlo, monospace;
  font-size: 12px;
  color: var(--panel-text);
  word-break: break-all;
}
.tmpl-items {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-right: 2px;
}
.tmpl-card {
  padding: 8px 10px;
  border: 1px solid var(--panel-border);
  border-radius: 8px;
  cursor: pointer;
  transition: border-color 0.15s ease, box-shadow 0.15s ease, background 0.15s ease;
  &:hover {
    border-color: var(--el-color-primary-light-5);
    box-shadow: 0 2px 8px rgba(79, 158, 255, 0.1);
    .copy-btn, .del-btn { opacity: 1; }
  }
  &.selected {
    border-color: var(--el-color-primary);
    background: var(--el-color-primary-light-9);
  }
  .copy-btn, .del-btn { opacity: 0; transition: opacity 0.15s ease; }
}
.tmpl-card-top {
  display: flex;
  align-items: center;
  gap: 6px;
}
.tmpl-name {
  flex: 1;
  font-size: 13px;
  font-weight: 600;
  color: var(--panel-text);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tmpl-card-bottom {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 3px;
}
.tmpl-cat {
  font-size: 11px;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  padding: 0 6px;
  border-radius: 3px;
}
.tmpl-size {
  flex: 1;
  font-size: 11px;
  color: var(--panel-text-secondary);
}
.builtin-tag {
  background: #67c23a;
  color: #fff;
  padding: 0 5px;
  border-radius: 3px;
  font-size: 10px;
  flex-shrink: 0;
}

/* 右侧编辑区 */
.tmpl-editor {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.editor-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  .editor-title {
    font-size: 13px;
    font-weight: 600;
    color: var(--panel-text);
  }
}
.editor-form {
  flex: 1;
  overflow-y: auto;
  :deep(.el-textarea__inner) {
    font-family: Consolas, 'SFMono-Regular', Menlo, monospace;
    font-size: 12.5px;
    line-height: 1.6;
  }
}
.tmpl-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 6px;
  color: var(--panel-text-secondary);
  font-size: 13px;
  .empty-icon { font-size: 34px; margin-bottom: 4px; color: var(--el-color-primary); }
  .empty-tip { font-size: 12px; opacity: 0.7; }
}
</style>
