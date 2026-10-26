// 全局设置 Pinia store：主题 / 界面语言
import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import * as wails from '@/utils/wails'
import { setLocale, resolveLocale, getLocale, type Locale } from '@/i18n'
import type { Settings } from '@/types'

export const useSettingsStore = defineStore('settings', () => {
  const theme = ref<'dark' | 'light'>('light')
  // 界面语言：跟随全局 i18n 响应式状态，切换后界面即时刷新
  const language = ref<Locale>(getLocale())
  // 日志目录（空 = 使用后端默认目录）
  const logDir = ref('')

  // 语言变更立即同步到 i18n 运行时
  watch(language, (v) => setLocale(v))

  // 用于挂到根节点 class 上
  const themeClass = computed(() => `theme-${theme.value}`)

  async function load() {
    try {
      const s: Settings = await wails.getSettings()
      theme.value = (s.theme === 'dark' ? 'dark' : 'light') as 'dark' | 'light'
      language.value = resolveLocale(s.language)
      logDir.value = s.logDir || ''
      setLocale(language.value)
    } catch (e) {
      console.warn('加载设置失败，使用默认值：', e)
    }
  }

  async function save(): Promise<boolean> {
    try {
      const res = await wails.saveSettings({
        theme: theme.value,
        language: language.value,
        logDir: logDir.value
      })
      // 后端保存的是规范化后的绝对路径，回写保持一致
      const s: Settings = await wails.getSettings()
      logDir.value = s.logDir || ''
      return res.success
    } catch (e) {
      console.error('保存设置失败：', e)
      return false
    }
  }

  // 应用主题到 html 节点（Element Plus 深色变量靠 html.dark 类生效）
  function applyTheme() {
    const root = document.documentElement
    if (theme.value === 'dark') {
      root.classList.add('dark')
    } else {
      root.classList.remove('dark')
    }
  }

  // 切换主题并立即生效
  async function toggleTheme() {
    theme.value = theme.value === 'dark' ? 'light' : 'dark'
    applyTheme()
    await save()
  }

  return { theme, language, logDir, themeClass, load, save, applyTheme, toggleTheme }
})
