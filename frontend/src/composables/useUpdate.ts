// 版本更新检测组合式函数：关于弹窗手动检测、启动时静默检测共用同一份模块级状态
import { computed, ref } from 'vue'
import { CheckForUpdate } from '@/utils/wails'
import { i18n } from '@/i18n'
import appConfig from '@app-config'

export interface UpdateInfo {
  currentVersion: string
  latestVersion: string
  hasUpdate: boolean
  prerelease: boolean
  releaseName: string
  releaseNotes: string
  publishedAt: string
  releaseUrl: string
  downloadUrl: string
  msg: string
}

// 模块级单例状态
const appVersion = ref(appConfig.version)
const updateInfo = ref<UpdateInfo | null>(null)
const checking = ref(false)
const showUpdateModal = ref(false)
// idle / checking / latest / available / error
const checkStatus = ref<'idle' | 'checking' | 'latest' | 'available' | 'error'>('idle')
// 检测失败原因（原始信息，展示时拼进本地化文案）
const checkError = ref('')

// 状态文案随界面语言实时变化
const checkMessage = computed(() => {
  switch (checkStatus.value) {
    case 'checking':
      return i18n('update.checking')
    case 'latest':
      return i18n('update.latest')
    case 'available':
      return i18n('update.found', { version: updateInfo.value?.latestVersion ?? '' })
    case 'error':
      return checkError.value
        ? i18n('update.failedDetail', { reason: checkError.value })
        : i18n('update.failed')
    default:
      return ''
  }
})

// 检测更新：silent=true 用于启动时静默检测，失败不提示；
// 发现新版本时自动弹出更新窗口
export async function checkForUpdate(silent = false): Promise<UpdateInfo | null> {
  if (checking.value) return updateInfo.value
  checking.value = true
  checkStatus.value = 'checking'
  checkError.value = ''
  try {
    const res = (await CheckForUpdate()) as unknown as UpdateInfo
    updateInfo.value = res
    if (res && res.msg === 'ok') {
      if (res.hasUpdate) {
        checkStatus.value = 'available'
        showUpdateModal.value = true
      } else {
        checkStatus.value = 'latest'
      }
    } else {
      checkStatus.value = 'error'
      checkError.value = (res && res.msg) || ''
    }
    return res
  } catch (e: unknown) {
    checkStatus.value = 'error'
    checkError.value = e instanceof Error ? e.message : String(e)
    return null
  } finally {
    checking.value = false
    // 静默检测（启动时）不在界面上保留状态文案
    if (silent && checkStatus.value !== 'available') {
      checkStatus.value = 'idle'
      checkError.value = ''
    }
  }
}

export function useUpdate() {
  return {
    appVersion,
    updateInfo,
    checking,
    showUpdateModal,
    checkStatus,
    checkMessage,
    checkForUpdate
  }
}
