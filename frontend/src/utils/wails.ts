// Wails v3 绑定 API 封装：所有对 Go 后端的调用集中在此，
// 前端其余文件不直接访问 @wailsio/runtime 或 bindings/ 目录，便于维护与类型收窄。
//
// v3 核心变化（与 v2 的主要不同）：
//   · 调用：window.go.main.App.xxx() → import { Xxx } from '@/../../bindings/SuperSender/app'
//   · 事件：runtime.EventsOn(name, cb) → Events.On(name, (ev) => cb(ev.data))
//     （v3 回调参数是 WailsEvent 对象，用 .data 取 payload）

import {
  AddFrontendLog,
  BuildSyslog,
  CheckForUpdate as _CheckForUpdate,
  CheckPortInUse,
  ClearLogs,
  ClearReceive,
  ClearStats,
  CreateSession,
  DeleteSession,
  DeleteTemplate,
  DisableReceiveLog,
  EnableReceiveLog,
  ExportReceive,
  ExportReceiveFiltered,
  ExportTemplates,
  FacilityOptions,
  GetAppDataDir,
  GetDefaultLogDir,
  GetLocalIPs,
  GetLogDir,
  GetNICs,
  GetPortOccupant,
  GetRecentLogs,
  GetReceiveBuffer,
  GetReceiveLogStatus,
  GetSessionConfig,
  GetSettings,
  GetSnapshot,
  GetTemplate,
  ImportTemplates,
  ListSessions,
  ListTemplates,
  OpenFileDialog,
  OpenLogDir,
  PickDirectory,
  ReadTextFile,
  PauseReceive,
  Ping,
  ReorderSessions,
  ResumeReceive,
  SaveFileDialog,
  SaveSettings,
  SaveTemplate,
  SendOnce,
  SeverityOptions,
  StartSending,
  StopAllSessions,
  StopSending,
  UpdateSessionConfig,
  Version
} from '../../bindings/SuperSender/app.js'

import { Events } from '@wailsio/runtime'

import type {
  APIResult,
  LogEntry,
  NICInfo,
  ReceiveEvent,
  SessionConfig,
  SessionInfo,
  Settings,
  StatsSnapshot,
  SyslogConfig,
  SyslogOption,
  Template
} from '@/types'

// 把前端类型 cast 成 bindings 兼容的类型（两者字段一致但 enum 形式不同，TS 严格模式下不兼容）
function asAny<T>(v: T): any { return v }

// === 元信息 ===
export const getVersion = () => Version()
export const getAppDataDir = () => GetAppDataDir() as Promise<string>
export const getLocalIPs = () => GetLocalIPs() as Promise<string[]>
// 网卡列表（名称+IP），服务端绑定 IP 下拉
export const getNICs = () => GetNICs() as Promise<NICInfo[]>
export const getRecentLogs = () => GetRecentLogs() as Promise<LogEntry[]>
export const clearLogs = () => ClearLogs()
// 前端写入运行日志（前后端日志统一排查链路）
export const addFrontendLog = (msg: string) => AddFrontendLog(msg)
// 事件通道自检：ping → 后端回 pong:<id>
export const ping = (id: string) => Ping(id)
// 版本更新检测
export const CheckForUpdate = () => _CheckForUpdate() as unknown as Promise<any>
// 端口占用检测：实际尝试监听，占用返回 true
export const checkPortInUse = (protocol: 'tcp' | 'udp', port: number) =>
  CheckPortInUse(protocol, port) as Promise<boolean>
// 查询占用指定端口的进程名（如 "SuperSender.exe (PID 4216)"）
export const getPortOccupant = (port: number) => GetPortOccupant(port) as Promise<string>

// === 文件对话框 ===
export const openFileDialog = (title: string) => OpenFileDialog(title)
export const saveFileDialog = (title: string, defaultName: string) => SaveFileDialog(title, defaultName)
// 读取文本文件内容（UTF-8 优先、GBK 回退），加载样本文件后直接填入报文编辑器
export const readTextFile = (path: string) => ReadTextFile(path) as unknown as Promise<string>

// === 会话管理 ===
export const createSession = (cfg: SessionConfig) => CreateSession(asAny(cfg)) as unknown as Promise<SessionConfig>
export const deleteSession = (id: string) => DeleteSession(id)
export const listSessions = () => ListSessions() as unknown as Promise<SessionInfo[]>
export const reorderSessions = (ids: string[]) => ReorderSessions(ids)
export const getSessionConfig = (id: string) => GetSessionConfig(id) as unknown as Promise<SessionConfig>
export const updateSessionConfig = (cfg: SessionConfig) => UpdateSessionConfig(asAny(cfg))
export const startSending = (id: string) => StartSending(id)
export const stopSending = (id: string) => StopSending(id)
export const stopAllSessions = () => StopAllSessions()
export const sendOnce = (id: string, payload: string) => SendOnce(id, payload)
export const pauseReceive = (id: string) => PauseReceive(id)
export const resumeReceive = (id: string) => ResumeReceive(id)
export const clearReceive = (id: string) => ClearReceive(id)
export const exportReceive = (id: string, path: string) => ExportReceive(id, path)
export const exportReceiveFiltered = (id: string, path: string, keyword: string) =>
  ExportReceiveFiltered(id, path, keyword)
export const getReceiveBuffer = (id: string) => GetReceiveBuffer(id)
export const clearStats = (id: string) => ClearStats(id)
export const getSnapshot = (id: string) => GetSnapshot(id)
export const enableReceiveLog = (id: string, path: string) => EnableReceiveLog(id, path)
export const disableReceiveLog = (id: string) => DisableReceiveLog(id)
export const getReceiveLogStatus = (id: string) =>
  GetReceiveLogStatus(id) as unknown as Promise<{ enabled: boolean; path: string }>

// === 模板 ===
export const listTemplates = () => ListTemplates()
export const saveTemplate = (t: Template) => SaveTemplate(t)
export const deleteTemplate = (id: string) => DeleteTemplate(id)
export const getTemplate = (id: string) => GetTemplate(id)
// 模板导入导出（JSON 文件）
export const exportTemplates = (path: string) => ExportTemplates(path) as Promise<APIResult>
export const importTemplates = (path: string) => ImportTemplates(path) as Promise<APIResult>

// === Syslog ===
export const buildSyslog = (cfg: SyslogConfig, message: string) => BuildSyslog(cfg, message)
export const facilityOptions = () => FacilityOptions() as unknown as Promise<SyslogOption[]>
export const severityOptions = () => SeverityOptions() as unknown as Promise<SyslogOption[]>

// === 设置 ===
export const getSettings = () => GetSettings()
export const saveSettings = (s: Settings) => SaveSettings(s)

// === 日志目录 ===
// 当前日志目录 / 默认日志目录（设置页提示与日志浮层展示）
export const getLogDir = () => GetLogDir() as Promise<string>
export const getDefaultLogDir = () => GetDefaultLogDir() as Promise<string>
// 在系统文件管理器中打开日志目录
export const openLogDir = () => OpenLogDir() as Promise<APIResult>
// 系统原生目录选择对话框（取消返回空串）
export const pickDirectory = (title: string) => PickDirectory(title) as Promise<string>

// === 事件订阅（Wails v3 runtime.Events.On 回调参数是 WailsEvent 对象，用 .data 取 payload） ===
// Wails 事件名约定：stats:<id> recv:<id> error:<id> status:<id>

// 订阅统计事件（按会话 id）
export function onStats(id: string, cb: (snap: StatsSnapshot) => void): () => void {
  return Events.On(`stats:${id}`, (ev: any) => cb(ev.data))
}

// 订阅接收报文事件
export function onReceive(id: string, cb: (evt: ReceiveEvent) => void): () => void {
  return Events.On(`recv:${id}`, (ev: any) => cb(ev.data))
}

// 订阅错误事件
export function onError(id: string, cb: (msg: string) => void): () => void {
  return Events.On(`error:${id}`, (ev: any) => cb(ev.data))
}

// 订阅状态变更事件
export function onStatus(id: string, cb: (status: string) => void): () => void {
  return Events.On(`status:${id}`, (ev: any) => cb(ev.data))
}

// 取消订阅指定事件名
export function offEvent(...eventNames: string[]) {
  (Events.Off as any)(...eventNames)
}

// 通用事件订阅（返回取消函数，别名）
export function onEvent(name: string, cb: (...args: any[]) => void): () => void {
  return Events.On(name, (ev: any) => cb(ev.data))
}

// 类型重导出，方便业务代码统一从 @/utils/wails 引入
export type {
  APIResult,
  ReceiveEvent,
  SessionConfig,
  SessionInfo,
  Settings,
  StatsSnapshot,
  SyslogConfig,
  SyslogOption,
  Template
}
