<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import en from 'element-plus/es/locale/lang/en'
import MainView from '@/components/layout/MainView.vue'
import UpdateDialog from '@/components/dialogs/UpdateDialog.vue'
import { useSettingsStore } from '@/stores/settings'
import { checkForUpdate } from '@/composables/useUpdate'
import { i18n } from '@/i18n'
import * as wails from '@/utils/wails'

// 应用启动状态：false 时显示加载占位，避免 webview 首帧空白抖动
const ready = ref(false)
const settings = useSettingsStore()

// Element Plus 组件（日期、分页、确认框按钮等）语言跟随界面语言
const elLocale = computed(() => (settings.language === 'en' ? en : zhCn))

// 启动时拉取全局设置并应用主题
onMounted(async () => {
  try {
    await settings.load()
    settings.applyTheme()
  } catch (e) {
    // 设置加载失败也不阻塞主界面，使用默认主题
    console.warn('加载设置失败：', e)
  } finally {
    ready.value = true
  }
  // 启动后延时静默检测更新：发现新版本时自动弹出更新窗口，失败不打扰用户
  setTimeout(() => {
    checkForUpdate(true)
  }, 2500)
  // 事件通道自检：前端 ping → 后端 pong → 写入运行日志，用于验证 Wails Events 双向通道
  wails.addFrontendLog(
    `运行时诊断：window.runtime=${typeof (window as any).runtime}，window.go=${typeof (window as any).go}`
  )
  const diagID = `diag-${Date.now()}`
  const offPong = wails.onEvent(`pong:${diagID}`, () => {
    offPong()
    wails.addFrontendLog('事件通道自检通过（Wails Events 双向正常）')
  })
  try {
    wails.ping(diagID)
  } catch {
    wails.addFrontendLog('[error] 事件通道自检失败：Ping 调用异常')
  }
})
</script>

<template>
  <el-config-provider :locale="elLocale">
    <div class="app-root" :class="settings.themeClass">
      <div v-if="!ready" class="app-loading">{{ i18n('brand.loading') }}</div>
      <MainView v-else />
    </div>
    <!-- 发现新版本弹窗（由更新状态单例控制显隐） -->
    <UpdateDialog />
  </el-config-provider>
</template>

<style scoped lang="scss">
.app-root {
  width: 100%;
  height: 100%;
}
.app-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
  color: var(--el-text-color-secondary, #c0c4cc);
  font-size: 14px;
}
</style>
