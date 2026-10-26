// 模板 Pinia store：管理报文模板列表 CRUD
import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as wails from '@/utils/wails'
import type { APIResult, SyslogConfig, Template } from '@/types'

export const useTemplateStore = defineStore('template', () => {
  const list = ref<Template[]>([])
  // Facility / Severity 下拉选项缓存
  const facilities = ref<{ label: string; value: number }[]>([])
  const severities = ref<{ label: string; value: number }[]>([])

  async function fetchList() {
    try {
      list.value = await wails.listTemplates()
    } catch (e) {
      console.error('拉取模板列表失败：', e)
    }
  }

  async function fetchSyslogOptions() {
    try {
      const [fs, ss] = await Promise.all([wails.facilityOptions(), wails.severityOptions()])
      facilities.value = fs
      severities.value = ss
    } catch (e) {
      console.error('拉取 Syslog 选项失败：', e)
    }
  }

  async function save(t: Template): Promise<Template | null> {
    try {
      const saved = await wails.saveTemplate(t)
      await fetchList()
      return saved
    } catch (e) {
      console.error('保存模板失败：', e)
      return null
    }
  }

  async function remove(id: string): Promise<boolean> {
    try {
      const res = await wails.deleteTemplate(id)
      if (res.success) {
        await fetchList()
        return true
      }
      return false
    } catch (e) {
      console.error('删除模板失败：', e)
      return false
    }
  }

  // 按 id 查询
  function getById(id: string): Template | null {
    return list.value.find(t => t.id === id) ?? null
  }

  // 按分类过滤
  function byCategory(cat: string): Template[] {
    return list.value.filter(t => t.category === cat)
  }

  // 预览 Syslog 拼接结果
  async function previewSyslog(cfg: SyslogConfig, message: string): Promise<string> {
    try {
      return await wails.buildSyslog(cfg, message)
    } catch (e) {
      console.error('Syslog 预览失败：', e)
      return ''
    }
  }

  // 导出全部模板到指定 JSON 文件
  async function exportAll(path: string): Promise<APIResult> {
    try {
      return await wails.exportTemplates(path)
    } catch (e) {
      console.error('导出模板失败：', e)
      return { success: false, message: '导出模板失败' }
    }
  }

  // 从 JSON 文件导入模板（成功后刷新列表）
  async function importAll(path: string): Promise<APIResult> {
    try {
      const res = await wails.importTemplates(path)
      if (res.success) await fetchList()
      return res
    } catch (e) {
      console.error('导入模板失败：', e)
      return { success: false, message: '导入模板失败' }
    }
  }

  return {
    list, facilities, severities,
    fetchList, fetchSyslogOptions, save, remove,
    getById, byCategory, previewSyslog, exportAll, importAll
  }
})
