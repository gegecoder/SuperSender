// 前后端共享类型定义。
// 字段名与 backend/models/models.go 的 JSON tag 严格一一对应（camelCase），
// 修改任何字段需同步更新 Go 端，否则 Wails 反序列化会丢字段。

// 协议类型
export type Protocol = 'tcp_client' | 'tcp_server' | 'udp_client' | 'udp_server'

// 子模式（客户端 / 服务端）
export type SubMode = 'client' | 'server'

// 发包模式
export type SendMode = 'single' | 'interval' | 'rate'

// 停止条件
export type StopCondition = 'manual' | 'count' | 'duration'

// 报文样本读取策略
export type ReadStrategy = 'sequential' | 'random'

// 编码方式
export type Encoding = 'utf-8' | 'gbk'

// TCP 应用层分帧协议模式（解决裸流粘包/拆包）
export type TCPFrameMode = 'raw' | 'delimiter' | 'fixed' | 'length'

// TCP 协议化（分帧）配置，仅 tcp_client/tcp_server 使用
export interface TCPFrameConfig {
  mode: TCPFrameMode
  delimiter: string // 分隔符原文，支持转义序列 \r \n \t \0 \\ \xHH
  frameLength: number // 固定长度模式：每帧字节数
  headerBytes: number // 长度前缀模式：包头字节数 1/2/4
  byteOrder: 'big' | 'little' // 长度前缀模式：大端/小端
  lengthIncludesSelf: boolean // 长度前缀模式：长度值是否包含包头自身
  maxFrameLength: number // 接收侧单帧上限保护（0 = 后端默认 16MB）
}

// 会话状态
export type SessionStatus = 'idle' | 'running' | 'error' | 'listening'

// 单个发包目标
export interface Target {
  ip: string
  port: number
}

// Syslog 配置
export interface SyslogConfig {
  enabled: boolean
  advanced: boolean // 高级配置开关：false 时 Facility/Severity/Hostname/Tag 使用默认值
  mode: 'rfc3164' | 'custom' // 报文头模式：RFC3164(默认) | 自定义前缀
  customPrefix: string // mode=custom 时替代 RFC3164 头，直接拼接在正文前
  facility: number // 0-23
  severity: number // 0-7
  hostname: string
  tag: string
}

// 会话配置（区域1 + 区域2 + 区域3 全部参数）
export interface SessionConfig {
  id: string
  name: string
  protocol: Protocol
  taskMode?: string // 任务模式（udp/tcp/syslog），用于任务页路由持久化
  targetIP: string
  targetPort: number
  sourceIP: string
  sourcePort: number
  targets: Target[]
  enableMulti: boolean
  broadcast: boolean
  payloadText: string
  filePath: string
  useFile: boolean
  readStrategy: ReadStrategy
  templateID: string
  paramEnabled: boolean
  mode: SendMode
  intervalMS: number
  ratePerSec: number
  stopCondition: StopCondition
  stopCount: number
  stopDurationSec: number
  encoding: Encoding
  sendEncoding: Encoding // 发送编码
  recvEncoding: Encoding // 接收编码
  syslog?: SyslogConfig
  tcp?: TCPFrameConfig // TCP 协议化（分帧）配置，仅 TCP 客户端/服务端
  // TCP 连接参数（旧会话缺失时后端按零值兜底，组件内用 ?? 取默认）
  dialTimeoutMs: number // 拨号超时（毫秒），默认 3000
  autoReconnect: boolean // 客户端断线自动重连
  reconnectIntervalMs: number // 重连间隔（毫秒），默认 1000
  reconnectMax: number // 最大重连次数，0=无限
  tcpNoDelay: boolean // TCP_NODELAY，默认 true
  tcpKeepAlive: boolean // SO_KEEPALIVE，默认 true
  keepAliveIntervalSec: number // KeepAlive 间隔（秒），默认 30
  maxConnections: number // 服务端最大连接数，0=不限
  echoBack: boolean // 服务端收到帧后原样回显给发送方
  createdAt: string
  updatedAt: string
}

// 网卡信息（服务端绑定 IP 下拉用）
export interface NICInfo {
  name: string
  ip: string
}

// 会话摘要（侧边栏列表用）
export interface SessionInfo {
  id: string
  name: string
  protocol: Protocol
  status: SessionStatus
  target: string
}

// 实时统计快照
export interface StatsSnapshot {
  sessionID: string
  totalSent: number
  sessionSent: number
  received: number
  ratePerSec: number
  bytesPerSec: number
  durationSec: number
  totalBytesSent: number
  // 接收侧指标（服务端模式主要使用）
  totalBytesRecv: number // 累计接收字节
  recvRatePerSec: number // 实时接收条/秒
  recvBytesPerSec: number // 实时接收字节/秒
  clientCount: number // 服务端模式下当前接入终端数
}

// 接收报文事件
export interface ReceiveEvent {
  sessionID: string
  timestamp: string
  data: string
  hexData: string
  source: string
}

// 报文模板
export interface Template {
  id: string
  name: string
  category: string // custom / syslog-firewall / syslog-switch / syslog-oracle / snmp 或自定义输入
  content: string
  builtin: boolean
  vendor: string // 厂商
  product: string // 产品
  model: string // 型号
  version: string // 版本
  createdAt: string
  updatedAt: string
}

// 运行日志条目
export interface LogEntry {
  time: string // HH:mm:ss
  level: 'info' | 'warn' | 'error'
  message: string
}

// 全局设置
export interface Settings {
  theme: string // dark / light
  language: string // 界面语言：zh / en，默认 zh
  logDir: string // 日志目录，空表示使用默认目录
}

// 通用操作结果
export interface APIResult {
  success: boolean
  message: string
}

// 错误响应
export interface ErrorResponse {
  error: string
  message: string
}

// Syslog Facility/Severity 下拉选项
export interface SyslogOption {
  label: string
  value: number
}

// 协议名称的展示统一使用 i18n key：proto.tcpClient 等（见 i18n/zh.ts）

// 读取策略下拉（发送时按行读取文本域内容，空行跳过）
export const READ_STRATEGY_OPTIONS: { labelKey: string; value: ReadStrategy }[] = [
  { labelKey: 'options.readSequential', value: 'sequential' },
  { labelKey: 'options.readRandom', value: 'random' }
]

// 编码下拉
export const ENCODING_OPTIONS: { label: string; value: Encoding }[] = [
  { label: 'UTF-8', value: 'utf-8' },
  { label: 'GBK', value: 'gbk' }
]

// TCP 分帧模式下拉
export const TCP_FRAME_MODE_OPTIONS: { labelKey: string; value: TCPFrameMode; descKey: string }[] = [
  { labelKey: 'tcpFrame.raw', value: 'raw', descKey: 'tcpFrame.rawDesc' },
  { labelKey: 'tcpFrame.delimiter', value: 'delimiter', descKey: 'tcpFrame.delimiterDesc' },
  { labelKey: 'tcpFrame.fixed', value: 'fixed', descKey: 'tcpFrame.fixedDesc' },
  { labelKey: 'tcpFrame.length', value: 'length', descKey: 'tcpFrame.lengthDesc' }
]

// 分隔符预设（值使用转义文本，由后端解析为真实字节）
export const TCP_DELIMITER_OPTIONS: { labelKey: string; value: string }[] = [
  { labelKey: 'tcpFrame.crlf', value: '\\r\\n' },
  { labelKey: 'tcpFrame.lf', value: '\\n' },
  { labelKey: 'tcpFrame.nul', value: '\\0' },
  { labelKey: 'tcpFrame.custom', value: '__custom__' }
]

// 长度前缀模式：包头字节数
export const TCP_HEADER_BYTES_OPTIONS: { labelKey: string; value: number }[] = [
  { labelKey: 'tcpFrame.header1', value: 1 },
  { labelKey: 'tcpFrame.header2', value: 2 },
  { labelKey: 'tcpFrame.header4', value: 4 }
]

// 长度前缀模式：字节序
export const TCP_BYTE_ORDER_OPTIONS: { labelKey: string; value: 'big' | 'little' }[] = [
  { labelKey: 'tcpFrame.byteOrderBig', value: 'big' },
  { labelKey: 'tcpFrame.byteOrderLittle', value: 'little' }
]

// 创建默认 TCP 分帧配置（默认原始裸流，旧版本会话行为不变）
export function createDefaultTCPFrameConfig(): TCPFrameConfig {
  return {
    mode: 'raw',
    delimiter: '\\r\\n',
    frameLength: 64,
    headerBytes: 4,
    byteOrder: 'big',
    lengthIncludesSelf: false,
    maxFrameLength: 0
  }
}

// 创建一个空白会话配置（带默认值）
// 注意：createdAt / updatedAt 必须是合法 ISO 时间字符串，
// 否则 Go 端 time.Time 反序列化空串会报错，导致新建会话无响应。
export function createDefaultSessionConfig(name: string): SessionConfig {
  const now = new Date().toISOString()
  return {
    id: '',
    name,
    protocol: 'udp_client',
    taskMode: 'udp',
    targetIP: '127.0.0.1',
    targetPort: 9000,
    sourceIP: '',
    sourcePort: 0,
    targets: [],
    enableMulti: false,
    broadcast: false,
    payloadText: 'Hello SuperSender',
    filePath: '',
    useFile: false,
    readStrategy: 'sequential',
    templateID: '',
    paramEnabled: false,
    mode: 'rate',
    intervalMS: 1000,
    ratePerSec: 1,
    stopCondition: 'count',
    stopCount: 1,
    stopDurationSec: 10,
    encoding: 'utf-8',
    sendEncoding: 'utf-8',
    recvEncoding: 'utf-8',
    syslog: { enabled: false, advanced: false, mode: 'rfc3164', customPrefix: '', facility: 16, severity: 6, hostname: 'supersender', tag: 'app' },
    tcp: createDefaultTCPFrameConfig(),
    dialTimeoutMs: 3000,
    autoReconnect: true,
    reconnectIntervalMs: 1000,
    reconnectMax: 0,
    tcpNoDelay: true,
    tcpKeepAlive: true,
    keepAliveIntervalSec: 30,
    maxConnections: 0,
    echoBack: false,
    createdAt: now,
    updatedAt: now
  }
}
