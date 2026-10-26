<script setup lang="ts">
// 发现新版本弹窗：展示当前/最新版本、发布时间、更新说明，支持跳转下载或发布页
import { computed } from 'vue'
import { Browser } from '@wailsio/runtime'
import { useUpdate } from '@/composables/useUpdate'
import { Download } from '@element-plus/icons-vue'
import { i18n } from '@/i18n'

const { updateInfo, showUpdateModal } = useUpdate()

const info = computed(() => updateInfo.value)

function close() {
  showUpdateModal.value = false
}

// 立即下载：优先 .exe 附件，其次任意附件，再退回发布页
function download() {
  const url = info.value?.downloadUrl || info.value?.releaseUrl
  if (url) Browser.OpenURL(url)
}

function openReleasePage() {
  if (info.value?.releaseUrl) Browser.OpenURL(info.value.releaseUrl)
}
</script>

<template>
  <el-dialog
    :model-value="showUpdateModal && !!info"
    @update:model-value="close"
    width="460px"
    class="update-dialog"
    :show-close="true"
    :title="i18n('update.title')"
  >
    <div v-if="info" class="update-body">
      <div class="update-versions">
        <span class="ver-old">v{{ info.currentVersion }}</span>
        <span class="ver-arrow">→</span>
        <span class="ver-new">v{{ info.latestVersion }}</span>
        <span v-if="info.prerelease" class="ver-beta">{{ i18n('update.prerelease') }}</span>
      </div>
      <p v-if="info.publishedAt" class="update-date">{{ i18n('update.publishedAt', { date: info.publishedAt }) }}</p>

      <div v-if="info.releaseNotes" class="update-notes">
        <p class="notes-title">{{ i18n('update.notes') }}</p>
        <pre class="notes-body">{{ info.releaseNotes }}</pre>
      </div>

      <div class="update-actions">
        <el-button @click="close">{{ i18n('update.later') }}</el-button>
        <el-button type="primary" :icon="Download" @click="download">{{ i18n('update.download') }}</el-button>
      </div>
      <a href="javascript:void(0)" class="update-page" @click="openReleasePage">{{ i18n('update.viewPage') }}</a>
    </div>
  </el-dialog>
</template>

<style scoped lang="scss">
.update-body {
  text-align: center;
}
.update-versions {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  margin-bottom: 4px;
}
.ver-old,
.ver-new {
  padding: 3px 12px;
  border-radius: 10px;
  font-size: 13px;
  font-weight: 700;
  font-family: Consolas, 'SFMono-Regular', Menlo, monospace;
}
.ver-old {
  background: var(--panel-bg, rgba(0, 0, 0, 0.04));
  color: var(--panel-text-secondary);
  border: 1px solid var(--panel-border);
}
.ver-new {
  background: rgba(79, 158, 255, 0.12);
  color: var(--el-color-primary);
  border: 1px solid rgba(79, 158, 255, 0.35);
}
.ver-arrow {
  color: var(--panel-text-secondary);
}
.ver-beta {
  padding: 2px 8px;
  border-radius: 8px;
  background: rgba(217, 119, 6, 0.1);
  color: #b45309;
  font-size: 11px;
  font-weight: 700;
}
.update-date {
  margin: 0 0 12px;
  font-size: 12px;
  color: var(--panel-text-secondary);
}
.update-notes {
  margin: 0 0 16px;
  text-align: left;
  border: 1px solid var(--panel-border);
  border-radius: 10px;
  overflow: hidden;
}
.notes-title {
  margin: 0;
  padding: 8px 12px;
  font-size: 12.5px;
  font-weight: 700;
  color: var(--panel-text);
  border-bottom: 1px solid var(--panel-border);
}
.notes-body {
  margin: 0;
  padding: 10px 12px;
  max-height: 200px;
  overflow-y: auto;
  font-size: 12.5px;
  line-height: 1.7;
  color: var(--panel-text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
  font-family: inherit;
}
.update-actions {
  display: flex;
  gap: 10px;
  justify-content: center;
}
.update-actions .el-button {
  flex: 1;
}
.update-page {
  display: inline-block;
  margin-top: 12px;
  font-size: 12px;
  color: var(--el-color-primary);
  text-decoration: none;
}
.update-page:hover {
  text-decoration: underline;
}
</style>
