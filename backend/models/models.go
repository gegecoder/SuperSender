// Package models 定义前后端共享的数据结构。
// 所有 Wails 绑定方法入参/返回值均使用此处类型，前端 TypeScript 类型据此生成。
package models

import "time"

// Protocol 协议类型枚举
type Protocol string

const (
	ProtocolTCPClient Protocol = "tcp_client" // TCP 客户端
	ProtocolTCPServer Protocol = "tcp_server" // TCP 服务端
	ProtocolUDPClient Protocol = "udp_client" // UDP 客户端
	ProtocolUDPServer Protocol = "udp_server" // UDP 服务端
)

// SendMode 发包模式
type SendMode string

const (
	SendModeSingle   SendMode = "single"   // 单次手动发送
	SendModeInterval SendMode = "interval" // 毫秒间隔定时发送
	SendModeRate     SendMode = "rate"     // 按条/秒精准速率压测
)

// StopCondition 停止条件类型
type StopCondition string

const (
	StopManual     StopCondition = "manual"   // 手动停止
	StopByCount    StopCondition = "count"    // 按总条数停止
	StopByDuration StopCondition = "duration" // 按运行时长停止
)

// ReadStrategy 报文样本读取策略
type ReadStrategy string

const (
	ReadSequential ReadStrategy = "sequential" // 顺序循环读取
	ReadRandom     ReadStrategy = "random"     // 随机选取样本
)

// TCPFrameMode TCP 应用层分帧协议类型（解决裸流粘包/拆包）
type TCPFrameMode string

const (
	TCPFrameRaw       TCPFrameMode = "raw"       // 原始裸流（默认，不分帧，行为同旧版本）
	TCPFrameDelimiter TCPFrameMode = "delimiter" // 分隔符分帧
	TCPFrameFixed     TCPFrameMode = "fixed"     // 固定长度分帧
	TCPFrameLength    TCPFrameMode = "length"    // 长度前缀分帧
)

// TCPFrameConfig TCP 协议化（分帧）配置，仅 TCP 客户端/服务端使用。
// 零值/nil 时按 raw 裸流处理，保证旧会话配置完全兼容。
type TCPFrameConfig struct {
	Mode TCPFrameMode `json:"mode"` // 分帧模式

	// 分隔符模式：分隔符原文，支持转义序列（\r \n \t \0 \\ \xHH）
	Delimiter string `json:"delimiter"`

	// 固定长度模式：每帧固定字节数（发送时末帧补 0x00，接收时按长度切帧）
	FrameLength int `json:"frameLength"`

	// 长度前缀模式
	HeaderBytes        int    `json:"headerBytes"`        // 长度头字节数：1/2/4
	ByteOrder          string `json:"byteOrder"`          // big（大端，默认）/ little（小端）
	LengthIncludesSelf bool   `json:"lengthIncludesSelf"` // 长度值是否包含头部自身字节数
	MaxFrameLength     int    `json:"maxFrameLength"`     // 接收侧单帧最大字节保护（0 取默认 16MB）
}

// Encoding 编码方式
type Encoding string

const (
	EncodingUTF8 Encoding = "utf-8"
	EncodingGBK  Encoding = "gbk"
)

// SessionStatus 会话状态
type SessionStatus string

const (
	StatusIdle      SessionStatus = "idle"      // 空闲
	StatusRunning   SessionStatus = "running"   // 发包中
	StatusError     SessionStatus = "error"     // 错误
	StatusListening SessionStatus = "listening" // 服务端监听中
)

// Target 单个发包目标
type Target struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

// SessionConfig 会话配置（区域1 + 区域2 + 区域3 的全部参数）
type SessionConfig struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Protocol Protocol `json:"protocol"`
	// 任务模式（udp/tcp/syslog），用于前端任务页路由持久化
	TaskMode string `json:"taskMode,omitempty"`
	// 目标地址（单目标场景）
	TargetIP   string `json:"targetIP"`
	TargetPort int    `json:"targetPort"`
	// 绑定本机源地址（可选）
	SourceIP   string `json:"sourceIP"`
	SourcePort int    `json:"sourcePort"`
	// 多目标批量发包列表（UDPSender 核心功能）
	Targets     []Target `json:"targets"`
	EnableMulti bool     `json:"enableMulti"` // 是否启用多目标
	// UDP 广播
	Broadcast bool `json:"broadcast"`

	// 报文来源
	PayloadText  string       `json:"payloadText"` // Monaco 编辑器报文文本
	FilePath     string       `json:"filePath"`    // 加载本地文件路径
	UseFile      bool         `json:"useFile"`     // 是否使用文件作为样本
	ReadStrategy ReadStrategy `json:"readStrategy"`
	TemplateID   string       `json:"templateID"`   // 选用模板ID
	ParamEnabled bool         `json:"paramEnabled"` // 启用参数化

	// 发包压测
	Mode            SendMode      `json:"mode"`
	IntervalMS      int           `json:"intervalMS"` // 模式A：毫秒间隔
	RatePerSec      int           `json:"ratePerSec"` // 模式B：条/秒
	StopCondition   StopCondition `json:"stopCondition"`
	StopCount       int64         `json:"stopCount"`       // 按条数停止
	StopDurationSec int64         `json:"stopDurationSec"` // 按时长停止（秒）
	Encoding        Encoding      `json:"encoding"`        // 兼容旧数据的通用编码
	SendEncoding    Encoding      `json:"sendEncoding"`    // 发送编码（空则回落 Encoding）
	RecvEncoding    Encoding      `json:"recvEncoding"`    // 接收编码（空则回落 Encoding）

	// Syslog 配置（P1）
	Syslog *SyslogConfig `json:"syslog,omitempty"`

	// TCP 协议化（分帧）配置，仅 tcp_client/tcp_server 使用
	TCP *TCPFrameConfig `json:"tcp,omitempty"`

	// TCP 连接参数（客户端拨号/重连、服务端连接管理）。
	// 平铺字段，旧会话无这些字段时为零值，由 sender.Engine 做合理兜底，无需迁移。
	DialTimeoutMs        int  `json:"dialTimeoutMs"`        // 拨号超时毫秒，<=0 兜底 3000
	AutoReconnect        bool `json:"autoReconnect"`        // 客户端断线自动重连（默认值由前端填充）
	ReconnectIntervalMs  int  `json:"reconnectIntervalMs"`  // 重连间隔毫秒，<=0 兜底 1000
	ReconnectMax         int  `json:"reconnectMax"`         // 最大重连次数，0=无限
	TcpNoDelay           bool `json:"tcpNoDelay"`           // TCP_NODELAY，未配置（旧数据）按 true 兜底
	TcpKeepAlive         bool `json:"tcpKeepAlive"`         // SO_KEEPALIVE，未配置（旧数据）按 true 兜底
	KeepAliveIntervalSec int  `json:"keepAliveIntervalSec"` // KeepAlive 间隔秒，<=0 兜底 30
	MaxConnections       int  `json:"maxConnections"`       // 服务端最大连接数，0=不限
	EchoBack             bool `json:"echoBack"`             // 服务端收到帧后原样回显给发送方

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SyslogConfig Syslog 协议封装配置（P1）
type SyslogConfig struct {
	Enabled      bool   `json:"enabled"`
	Advanced     bool   `json:"advanced"`     // 高级配置开关：false 时 Facility/Severity/Hostname/Tag 使用默认值
	Mode         string `json:"mode"`         // 报文头模式：rfc3164(默认) | custom（自定义前缀，空值兜底 rfc3164）
	CustomPrefix string `json:"customPrefix"` // Mode=custom 时替代 RFC3164 头，直接拼接在正文前
	Facility     int    `json:"facility"`     // 0-23
	Severity     int    `json:"severity"`     // 0-7
	Hostname     string `json:"hostname"`     // 主机名
	Tag          string `json:"tag"`          // 标签
}

// SessionInfo 会话摘要（侧边栏列表用）
type SessionInfo struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Protocol Protocol      `json:"protocol"`
	Status   SessionStatus `json:"status"`
	Target   string        `json:"target"` // 显示用 "ip:port" 或 "multi"
}

// StatsSnapshot 实时统计快照（区域5）
type StatsSnapshot struct {
	SessionID      string  `json:"sessionID"`
	TotalSent      int64   `json:"totalSent"`      // 累计发送
	SessionSent    int64   `json:"sessionSent"`    // 本次会话发送
	Received       int64   `json:"received"`       // 接收计数
	RatePerSec     float64 `json:"ratePerSec"`     // 实时条/秒（发送）
	BytesPerSec    float64 `json:"bytesPerSec"`    // 实时字节/秒（发送）
	DurationSec    int64   `json:"durationSec"`    // 运行时长
	TotalBytesSent int64   `json:"totalBytesSent"` // 累计发送字节
	// 接收侧指标（服务端模式主要使用）
	TotalBytesRecv  int64   `json:"totalBytesRecv"`  // 累计接收字节
	RecvRatePerSec  float64 `json:"recvRatePerSec"`  // 实时接收条/秒
	RecvBytesPerSec float64 `json:"recvBytesPerSec"` // 实时接收字节/秒
	ClientCount     int     `json:"clientCount"`     // 服务端模式下当前接入终端数（TCP=已连接客户端；UDP=近60s内有通信的远端数）
}

// ReceiveEvent 接收报文事件（推送到前端接收区）
type ReceiveEvent struct {
	SessionID string    `json:"sessionID"`
	Timestamp time.Time `json:"timestamp"`
	Data      string    `json:"data"`    // 文本
	HexData   string    `json:"hexData"` // 十六进制
	Source    string    `json:"source"`  // 来源地址
}

// Template 报文模板
type Template struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Category  string    `json:"category"` // 分类：custom/syslog-firewall/syslog-switch/syslog-oracle/snmp 或自定义输入
	Content   string    `json:"content"`
	Builtin   bool      `json:"builtin"` // 内置预制不可删
	Vendor    string    `json:"vendor"`  // 厂商
	Product   string    `json:"product"` // 产品
	Model     string    `json:"model"`   // 型号
	Version   string    `json:"version"` // 版本
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Settings 全局设置
type Settings struct {
	Theme    string `json:"theme"`    // dark/light
	Language string `json:"language"` // 界面语言：zh/en，默认 zh
	LogDir   string `json:"logDir"`   // 日志目录，空表示使用默认目录
}

// ErrorResponse 统一错误返回
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// APIResult 通用操作结果
type APIResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ReceiveLogStatus 实时落盘状态（供前端切换会话后恢复勾选）
type ReceiveLogStatus struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
}
