// 任务卡片可拖拽布局：栅格项布局的 localStorage 持久化与重置。
// 每类任务（协议×模式）共享一套布局，按 key 存储；默认项为基准合并，
// 版本迭代新增卡片时自动补入、删除的卡片自动剔除，已保存的位置/尺寸保留。
import { ref, type Ref } from 'vue'
import type { Layout, LayoutItem } from 'grid-layout-plus'

const PREFIX = 'super-sender:task-layout:v1:'

export interface UseTaskLayoutReturn {
  layout: Ref<Layout>
  /** 布局变化（拖拽/缩放结束）后调用，防抖写入 localStorage */
  persist: (next: Layout) => void
  /** 恢复默认布局并清除已保存的布局 */
  resetLayout: () => void
}

export function useTaskLayout(key: string, defaults: Layout): UseTaskLayoutReturn {
  const storageKey = PREFIX + key
  const cloneDefaults = (): Layout => defaults.map((d) => ({ ...d }))

  // 以默认项为基准合并：保留用户自定义的 x/y/w/h，约束项（minW/minH 等）始终取最新默认值
  function merge(saved: LayoutItem[] | null): Layout {
    const base = cloneDefaults()
    if (!Array.isArray(saved) || saved.length === 0) return base
    return base.map((d) => {
      const s = saved.find((x) => String(x.i) === String(d.i))
      if (!s) return d
      return {
        ...d,
        x: Number.isFinite(s.x) ? s.x : d.x,
        y: Number.isFinite(s.y) ? s.y : d.y,
        w: Number.isFinite(s.w) ? s.w : d.w,
        h: Number.isFinite(s.h) ? s.h : d.h
      }
    })
  }

  let saved: LayoutItem[] | null = null
  try {
    const raw = localStorage.getItem(storageKey)
    if (raw) saved = JSON.parse(raw) as LayoutItem[]
  } catch {
    saved = null
  }

  const layout = ref<Layout>(merge(saved))

  let saveTimer: ReturnType<typeof setTimeout> | null = null
  function persist(next: Layout) {
    if (saveTimer) clearTimeout(saveTimer)
    saveTimer = setTimeout(() => {
      try {
        localStorage.setItem(storageKey, JSON.stringify(next))
      } catch {
        /* 存储失败（隐私模式等）静默忽略，布局仅本次有效 */
      }
    }, 200)
  }

  function resetLayout() {
    layout.value = cloneDefaults()
    try {
      localStorage.removeItem(storageKey)
    } catch {
      /* ignore */
    }
  }

  return { layout, persist, resetLayout }
}
