// Package sender 发包引擎核心：支持 TCP/UDP 客户端+服务端、单次/毫秒间隔/精准速率压测、
// 手动/计数/时长停止条件、顺序/随机样本读取、多目标批量发包。
// 所有发送协程通过 context 控制，停止时关闭 socket，杜绝 goroutine 泄漏。
package sender

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"SuperSender/backend/applog"
	"SuperSender/backend/models"
	"SuperSender/backend/sockopt"
	"SuperSender/backend/stats"
	"SuperSender/backend/syslog"
	"SuperSender/backend/tcpproto"
	"SuperSender/backend/template"
)

// EventCallbacks 引擎向会话层推送事件回调集合
type EventCallbacks struct {
	OnStats   func(snap models.StatsSnapshot) // 实时统计推送
	OnReceive func(evt models.ReceiveEvent)   // 接收报文推送
	OnError   func(sessionID string, err error)
	OnStatus  func(sessionID string, status models.SessionStatus)
}

// Engine 单会话发包引擎
type Engine struct {
	cfg   *models.SessionConfig
	stats *stats.Collector
	cb    EventCallbacks

	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	running    atomic.Bool
	listener   net.Listener
	udpConn    net.PacketConn
	conns      map[string]net.Conn  // TCP server 模式下已接入的客户端
	conn       net.Conn             // TCP/UDP 客户端模式连接
	udpClients map[string]time.Time // UDP server 模式下近期通信过的远端地址（带 TTL，避免无限增长）
	rng        *rand.Rand
	rngMu      sync.Mutex
	seq        int64
	recvPause  atomic.Bool

	// 报文行样本（读取策略数据源）：Start 时从 PayloadText 按行拆分，空行跳过。
	// 运行中表单禁用保证 PayloadText 不变，快照一次即可；lineSeq 独立于参数化 seq，
	// 避免顺序轮转消耗序号导致 ${seq} 跳号。
	lines   []string
	lineSeq int64

	// frame TCP 应用层分帧编解码器（仅 tcp_client/tcp_server 使用；
	// 未配置或 raw 模式下 Encode/Feed 为恒等变换，不影响裸流行为）
	frame *tcpproto.Codec

	// TCP 连接参数兜底值（New 时从 cfg 解析：旧会话零值按默认处理）
	dialTimeout       time.Duration // 拨号超时
	reconnectInterval time.Duration // 自动重连间隔
	keepAlivePeriod   time.Duration // SO_KEEPALIVE 间隔
	tcpNoDelay        bool          // TCP_NODELAY
	tcpKeepAlive      bool          // SO_KEEPALIVE
}

// udpClientTTL UDP 服务端远端地址的存活时长（超过则从统计中剔除）
const udpClientTTL = 60 * time.Second

// New 创建引擎实例
func New(cfg *models.SessionConfig, st *stats.Collector, cb EventCallbacks) *Engine {
	e := &Engine{
		cfg:        cfg,
		stats:      st,
		cb:         cb,
		conns:      make(map[string]net.Conn),
		udpClients: make(map[string]time.Time),
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		frame:      tcpproto.New(cfg.TCP),
	}
	e.resolveTCPConnOptions()
	return e
}

// resolveTCPConnOptions 解析 TCP 连接参数并对旧会话零值做兜底。
// bool 零值 false 与"默认 true"冲突无法逐字段区分，因此当 9 个新字段
// 全部为零值时判定为旧会话（未配置），NoDelay/KeepAlive 按默认 true 处理；
// 前端 createDefaultSessionConfig 会填充全部字段，用户显式关闭可正常生效。
func (e *Engine) resolveTCPConnOptions() {
	cfg := e.cfg
	e.dialTimeout = time.Duration(cfg.DialTimeoutMs) * time.Millisecond
	if e.dialTimeout <= 0 {
		e.dialTimeout = 3 * time.Second
	}
	e.reconnectInterval = time.Duration(cfg.ReconnectIntervalMs) * time.Millisecond
	if e.reconnectInterval <= 0 {
		e.reconnectInterval = time.Second
	}
	e.keepAlivePeriod = time.Duration(cfg.KeepAliveIntervalSec) * time.Second
	if e.keepAlivePeriod <= 0 {
		e.keepAlivePeriod = 30 * time.Second
	}
	legacy := cfg.DialTimeoutMs == 0 && !cfg.AutoReconnect && cfg.ReconnectIntervalMs == 0 &&
		cfg.ReconnectMax == 0 && !cfg.TcpNoDelay && !cfg.TcpKeepAlive &&
		cfg.KeepAliveIntervalSec == 0 && cfg.MaxConnections == 0 && !cfg.EchoBack
	e.tcpNoDelay = cfg.TcpNoDelay || legacy
	e.tcpKeepAlive = cfg.TcpKeepAlive || legacy
}

// IsRunning 是否正在运行
func (e *Engine) IsRunning() bool {
	return e.running.Load()
}

// Start 启动发包引擎（根据协议分发）
func (e *Engine) Start() error {
	e.mu.Lock()
	if e.running.Load() {
		e.mu.Unlock()
		return fmt.Errorf("会话 %s 已在运行中，禁止重复启动", e.cfg.ID)
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.ctx, e.cancel = ctx, cancel
	e.mu.Unlock()
	e.initPayloadLines()

	switch e.cfg.Protocol {
	case models.ProtocolTCPClient, models.ProtocolUDPClient:
		return e.startClient(ctx)
	case models.ProtocolTCPServer:
		return e.startTCPServer(ctx)
	case models.ProtocolUDPServer:
		return e.startUDPServer(ctx)
	default:
		return fmt.Errorf("未知协议: %s", e.cfg.Protocol)
	}
}

// Stop 停止发包引擎，释放全部 socket 资源
func (e *Engine) Stop() {
	e.mu.Lock()
	cancel := e.cancel
	e.cancel = nil
	wasRunning := e.running.Load()
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	e.running.Store(false)
	e.stats.Stop()
	e.closeAll()
	if wasRunning {
		snap := e.stats.Snapshot()
		applog.Printf("[session %s] 引擎停止，本次发送 %d 条 / 接收 %d 条", e.cfg.ID, snap.SessionSent, snap.Received)
	}
	e.cb.OnStatus(e.cfg.ID, models.StatusIdle)
}

// PauseReceive 暂停接收区刷新（仍计数，不推送 UI）
func (e *Engine) PauseReceive() { e.recvPause.Store(true) }

// ResumeReceive 恢复接收区刷新
func (e *Engine) ResumeReceive() { e.recvPause.Store(false) }

// closeAll 关闭所有 socket 与监听器
func (e *Engine) closeAll() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.listener != nil {
		_ = e.listener.Close()
		e.listener = nil
	}
	if e.udpConn != nil {
		_ = e.udpConn.Close()
		e.udpConn = nil
	}
	for id, c := range e.conns {
		_ = c.Close()
		delete(e.conns, id)
	}
	if e.conn != nil {
		_ = e.conn.Close()
		e.conn = nil
	}
	// 清空 UDP 服务端远端地址表
	e.udpClients = make(map[string]time.Time)
}

// ClientCount 返回服务端模式下当前接入的终端数。
// TCP 服务端：当前已 Accept 且未断开的客户端连接数；
// UDP 服务端：近 udpClientTTL 内有过通信的不同远端地址数；
// 客户端模式：恒为 0。
func (e *Engine) ClientCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	switch e.cfg.Protocol {
	case models.ProtocolTCPServer:
		return len(e.conns)
	case models.ProtocolUDPServer:
		now := time.Now()
		// 清理过期条目并统计存活数
		count := 0
		for addr, last := range e.udpClients {
			if now.Sub(last) > udpClientTTL {
				delete(e.udpClients, addr)
				continue
			}
			count++
		}
		return count
	default:
		return 0
	}
}

// === 客户端模式 ===

func (e *Engine) startClient(ctx context.Context) error {
	// 预建连接
	if err := e.dialClient(ctx); err != nil {
		applog.Errorf("[session %s] 建立连接失败: %v", e.cfg.ID, err)
		e.cb.OnError(e.cfg.ID, err)
		e.cb.OnStatus(e.cfg.ID, models.StatusError)
		return err
	}
	e.running.Store(true)
	e.stats.Start()
	e.cb.OnStatus(e.cfg.ID, models.StatusRunning)
	applog.Printf("[session %s] 客户端启动 (%s → %s)，速率 %d 条/秒，停止条件 %s",
		e.cfg.ID, e.cfg.Protocol, e.targetAddr(), e.cfg.RatePerSec, e.cfg.StopCondition)

	// 接收协程：TCP 客户端走独立的接收+自动重连生命周期；UDP 保留原接收循环
	if e.cfg.Protocol == models.ProtocolTCPClient {
		go e.tcpClientConnLoop(ctx)
	} else {
		go e.recvLoop(ctx)
	}

	// 发送协程
	if e.cfg.Mode == models.SendModeSingle {
		// 单次模式不自动循环，等待手动 SendOnce
		return nil
	}
	go e.sendLoop(ctx)
	return nil
}

// dialClient 建立 TCP/UDP 客户端连接
// UDP 多目标/广播场景使用未连接 socket（WriteTo），单目标使用已连接 socket。
// ctx 取消时进行中的拨号立即中断（重连协程随 Stop 快速退出）。
func (e *Engine) dialClient(ctx context.Context) error {
	switch e.cfg.Protocol {
	case models.ProtocolTCPClient:
		addr := e.targetAddr()
		var d net.Dialer
		d.Timeout = e.dialTimeout // 拨号超时兜底 3s
		if e.cfg.SourceIP != "" || e.cfg.SourcePort > 0 {
			laddr := &net.TCPAddr{}
			if e.cfg.SourceIP != "" {
				laddr.IP = net.ParseIP(e.cfg.SourceIP)
			}
			if e.cfg.SourcePort > 0 {
				laddr.Port = e.cfg.SourcePort
			}
			d.LocalAddr = laddr
		}
		c, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return fmt.Errorf("TCP 拨号失败 %s: %w", addr, err)
		}
		// 连接参数：TCP_NODELAY（默认开）、SO_KEEPALIVE（默认开，间隔可配）
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(e.tcpNoDelay)
			if e.tcpKeepAlive {
				_ = tc.SetKeepAlive(true)
				_ = tc.SetKeepAlivePeriod(e.keepAlivePeriod)
			}
		}
		e.mu.Lock()
		e.conn = c
		e.mu.Unlock()
	case models.ProtocolUDPClient:
		if e.cfg.EnableMulti || e.cfg.Broadcast {
			// 多目标/广播：使用未连接 socket
			var laddr *net.UDPAddr
			if e.cfg.SourceIP != "" || e.cfg.SourcePort > 0 {
				laddr = &net.UDPAddr{}
				if e.cfg.SourceIP != "" {
					laddr.IP = net.ParseIP(e.cfg.SourceIP)
				}
				if e.cfg.SourcePort > 0 {
					laddr.Port = e.cfg.SourcePort
				}
			}
			pc, err := net.ListenUDP("udp", laddr)
			if err != nil {
				return fmt.Errorf("UDP 监听失败: %w", err)
			}
			e.udpConn = pc
		} else {
			addr := e.targetAddr()
			var d net.Dialer
			if e.cfg.SourceIP != "" || e.cfg.SourcePort > 0 {
				laddr := &net.UDPAddr{}
				if e.cfg.SourceIP != "" {
					laddr.IP = net.ParseIP(e.cfg.SourceIP)
				}
				if e.cfg.SourcePort > 0 {
					laddr.Port = e.cfg.SourcePort
				}
				d.LocalAddr = laddr
			}
			c, err := d.DialContext(ctx, "udp", addr)
			if err != nil {
				return fmt.Errorf("UDP 拨号失败 %s: %w", addr, err)
			}
			e.mu.Lock()
			e.conn = c
			e.mu.Unlock()
		}
	}
	return nil
}

// currentConn 加锁获取当前客户端连接（重连协程会并发换接，发送侧须取局部引用）
func (e *Engine) currentConn() net.Conn {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.conn
}

// targetAddr 获取主目标地址
func (e *Engine) targetAddr() string {
	if e.cfg.TargetIP == "" {
		return "127.0.0.1:" + fmt.Sprint(e.cfg.TargetPort)
	}
	return fmt.Sprintf("%s:%d", e.cfg.TargetIP, e.cfg.TargetPort)
}

// isTCP 当前会话是否为 TCP 协议（协议化分帧只作用于 TCP，不影响 UDP/Syslog）
func (e *Engine) isTCP() bool {
	return e.cfg.Protocol == models.ProtocolTCPClient || e.cfg.Protocol == models.ProtocolTCPServer
}

// framePayload TCP 发送封帧：按会话配置的分帧协议包装报文体；
// 非 TCP 协议或 raw 模式下原样返回（字节不变，旧行为完全保留）。
func (e *Engine) framePayload(payload []byte) []byte {
	if !e.isTCP() {
		return payload
	}
	return e.frame.Encode(payload)
}

// multiTargets 多目标列表（UDP 批量发包）
func (e *Engine) multiTargets() []string {
	if !e.cfg.EnableMulti || len(e.cfg.Targets) == 0 {
		return []string{e.targetAddr()}
	}
	out := make([]string, 0, len(e.cfg.Targets))
	for _, t := range e.cfg.Targets {
		out = append(out, fmt.Sprintf("%s:%d", t.IP, t.Port))
	}
	return out
}

// === TCP 服务端模式 ===

func (e *Engine) startTCPServer(ctx context.Context) error {
	bind := e.bindAddr()
	// Windows 下端口独占绑定：端口真被占用时立即报错，避免 REUSEADDR 假成功收不到包
	lc := net.ListenConfig{Control: sockopt.ControlExclusiveAddr}
	ln, err := lc.Listen(ctx, "tcp", bind)
	if err != nil {
		applog.Errorf("[session %s] TCP 监听失败 %s: %v", e.cfg.ID, bind, err)
		e.cb.OnError(e.cfg.ID, fmt.Errorf("TCP 监听失败 %s: %w", bind, err))
		e.cb.OnStatus(e.cfg.ID, models.StatusError)
		return err
	}
	e.listener = ln
	e.running.Store(true)
	e.stats.Start()
	e.cb.OnStatus(e.cfg.ID, models.StatusListening)
	applog.Printf("[session %s] TCP 服务端开始监听 %s", e.cfg.ID, bind)

	// Accept 循环
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			c, err := ln.Accept()
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				e.cb.OnError(e.cfg.ID, fmt.Errorf("TCP Accept 失败: %w", err))
				return
			}
			id := c.RemoteAddr().String()
			e.mu.Lock()
			// 最大连接数限制：达上限拒绝新连接，不退出 Accept 循环、不起 handler
			if e.cfg.MaxConnections > 0 && len(e.conns) >= e.cfg.MaxConnections {
				e.mu.Unlock()
				applog.Printf("[session %s] 连接数已达上限 %d，拒绝新连接 %s", e.cfg.ID, e.cfg.MaxConnections, id)
				_ = c.Close()
				continue
			}
			e.conns[id] = c
			connCount := len(e.conns)
			e.mu.Unlock()
			applog.Printf("[session %s] TCP 客户端接入 %s（当前 %d 个）", e.cfg.ID, id, connCount)
			go e.handleServerConn(ctx, id, c)
		}
	}()
	return nil
}

func (e *Engine) handleServerConn(ctx context.Context, id string, c net.Conn) {
	defer func() {
		c.Close()
		e.mu.Lock()
		delete(e.conns, id)
		remain := len(e.conns)
		e.mu.Unlock()
		applog.Printf("[session %s] TCP 客户端断开 %s（剩余 %d 个）", e.cfg.ID, id, remain)
	}()
	// 每连接独立的分帧解码器（协议化配置下解决粘包/拆包；raw 模式逐 Read 上报）
	decoder := e.frame.NewDecoder()
	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		frames, derr := decoder.Feed(buf[:n])
		if derr != nil {
			applog.Errorf("[session %s] TCP 分帧解码失败（%s）: %v，关闭连接", e.cfg.ID, id, derr)
			e.cb.OnError(e.cfg.ID, fmt.Errorf("TCP 分帧解码失败: %w", derr))
			return
		}
		if len(frames) == 0 {
			continue // 半包：等待更多字节
		}
		applog.Printf("[session %s] 收到 TCP 报文 %d 帧 / %d 字节 ← %s", e.cfg.ID, len(frames), n, c.RemoteAddr())
		for _, frame := range frames {
			e.emitReceive(frame, c.RemoteAddr().String())
			// 接收回显：把 Feed 吐出的帧原始 wire 字节原样写回本连接（便于双向链路自测）；
			// 写失败仅记日志，回显不计入 IncSent 统计
			if e.cfg.EchoBack {
				if _, werr := c.Write(frame); werr != nil {
					applog.Errorf("[session %s] 接收回显失败（%s）: %v", e.cfg.ID, id, werr)
				}
			}
		}
		e.stats.IncReceived(int64(len(frames)), int64(n))
	}
}

// === UDP 服务端模式 ===

func (e *Engine) startUDPServer(ctx context.Context) error {
	bind := e.bindAddr()
	// Windows 下端口独占绑定：端口真被占用时立即报错，避免 REUSEADDR 假成功收不到包
	lc := net.ListenConfig{Control: sockopt.ControlExclusiveAddr}
	pc, err := lc.ListenPacket(ctx, "udp", bind)
	if err != nil {
		applog.Errorf("[session %s] UDP 监听失败 %s: %v", e.cfg.ID, bind, err)
		e.cb.OnError(e.cfg.ID, fmt.Errorf("UDP 监听失败 %s: %w", bind, err))
		e.cb.OnStatus(e.cfg.ID, models.StatusError)
		return err
	}
	e.udpConn = pc
	e.running.Store(true)
	e.stats.Start()
	e.cb.OnStatus(e.cfg.ID, models.StatusListening)
	applog.Printf("[session %s] UDP 服务端开始监听 %s", e.cfg.ID, bind)

	go e.udpServerRecvLoop(ctx, pc)
	return nil
}

func (e *Engine) udpServerRecvLoop(ctx context.Context, pc net.PacketConn) {
	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, raddr, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			e.cb.OnError(e.cfg.ID, fmt.Errorf("UDP ReadFrom 失败: %w", err))
			return
		}
		// 记录远端地址到存活表（用于统计当前接入终端数）
		e.mu.Lock()
		e.udpClients[raddr.String()] = time.Now()
		e.mu.Unlock()
		applog.Printf("[session %s] 收到 UDP 报文 %d 字节 ← %s", e.cfg.ID, n, raddr)
		e.emitReceive(buf[:n], raddr.String())
		e.stats.IncReceived(1, int64(n))
	}
}

// bindAddr 服务端绑定地址
func (e *Engine) bindAddr() string {
	ip := e.cfg.SourceIP
	if ip == "" {
		ip = "0.0.0.0"
	}
	port := e.cfg.SourcePort
	if port == 0 {
		port = e.cfg.TargetPort
	}
	return fmt.Sprintf("%s:%d", ip, port)
}

// === TCP 客户端：接收 + 自动重连生命周期 ===

// tcpClientConnLoop TCP 客户端连接状态机：在当前连接上接收，读错误（对端关闭/
// 网络断开）后依据 AutoReconnect 决定退出或自动重连。进入时连接已由
// startClient/SendOnce 建立，本协程独占断开连接的拆除与重建；ctx 取消立即退出。
//
// 状态流转：
//
//	接收中 ──Read错误──▶ 拆除旧连接
//	 ├ AutoReconnect=false        → OnError(连接已断开)+OnStatus(idle) → 退出
//	 └ AutoReconnect=true         → 等待 ReconnectInterval → 拨号
//	                               ├ 成功：记录日志，重新进入接收中（计数清零）
//	                               ├ 失败：记录日志，累计次数，循环等待再拨
//	                               └ 达 ReconnectMax(>0)：OnError+OnStatus(idle) → 退出
//	任意阶段 ctx.Done()（Stop）→ 立即退出，不重连
func (e *Engine) tcpClientConnLoop(ctx context.Context) {
	attempts := 0 // 本轮断线后的连续重连失败次数（重连成功清零）
	for {
		readErr := e.tcpClientReadOnce(ctx)
		if ctx.Err() != nil {
			return // Stop 取消：立即退出
		}

		// 拆除已断开的连接（发送侧据此感知重连中空窗）
		e.mu.Lock()
		old := e.conn
		if old != nil {
			_ = old.Close()
			e.conn = nil
		}
		e.mu.Unlock()

		if !e.cfg.AutoReconnect {
			applog.Printf("[session %s] TCP 连接已断开: %v", e.cfg.ID, readErr)
			e.cb.OnError(e.cfg.ID, fmt.Errorf("连接已断开"))
			e.cb.OnStatus(e.cfg.ID, models.StatusIdle)
			return
		}
		applog.Printf("[session %s] TCP 连接断开: %v，开始自动重连（间隔 %s，上限 %d）",
			e.cfg.ID, readErr, e.reconnectInterval, e.cfg.ReconnectMax)

		// 重连状态机：间隔等待 → 带超时拨号 → 成功继续接收 / 失败累计重试
		for {
			if e.cfg.ReconnectMax > 0 && attempts >= e.cfg.ReconnectMax {
				applog.Errorf("[session %s] 自动重连 %d 次仍失败，放弃重连", e.cfg.ID, attempts)
				e.cb.OnError(e.cfg.ID, fmt.Errorf("连接已断开，自动重连 %d 次失败", attempts))
				e.cb.OnStatus(e.cfg.ID, models.StatusIdle)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(e.reconnectInterval):
			}
			attempts++
			if err := e.dialClient(ctx); err != nil {
				applog.Errorf("[session %s] 第 %d 次自动重连失败: %v", e.cfg.ID, attempts, err)
				continue
			}
			applog.Printf("[session %s] 第 %d 次自动重连成功，恢复接收 %s",
				e.cfg.ID, attempts, e.targetAddr())
			attempts = 0
			break
		}
	}
}

// tcpClientReadOnce 在当前 TCP 客户端连接上阻塞接收并分帧上报，
// 直到 Read/解码返回错误（连接断开），交由 tcpClientConnLoop 决定重连策略。
func (e *Engine) tcpClientReadOnce(ctx context.Context) error {
	e.mu.Lock()
	conn := e.conn
	e.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("连接不存在")
	}
	decoder := e.frame.NewDecoder()
	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := conn.Read(buf)
		if err != nil {
			return err
		}
		frames, derr := decoder.Feed(buf[:n])
		if derr != nil {
			applog.Errorf("[session %s] TCP 分帧解码失败: %v", e.cfg.ID, derr)
			e.cb.OnError(e.cfg.ID, fmt.Errorf("TCP 分帧解码失败: %w", derr))
			return derr
		}
		if len(frames) == 0 {
			continue // 半包：等待更多字节
		}
		applog.Printf("[session %s] 收到 TCP 报文 %d 帧 / %d 字节 ← %s",
			e.cfg.ID, len(frames), n, conn.RemoteAddr())
		src := conn.RemoteAddr().String()
		for _, frame := range frames {
			e.emitReceive(frame, src)
		}
		e.stats.IncReceived(int64(len(frames)), int64(n))
	}
}

// === 接收协程（客户端模式） ===

func (e *Engine) recvLoop(ctx context.Context) {
	// 进入循环前在上锁状态下捕获连接引用：Stop/closeAll 会在锁内置空 e.conn/e.udpConn，
	// 循环内若直接读字段会与之产生数据竞争；局部引用在连接被 Close 后照常收到读错误并退出。
	e.mu.Lock()
	conn := e.conn
	pc := e.udpConn
	e.mu.Unlock()
	if conn == nil && pc == nil {
		return
	}
	// TCP 客户端使用有状态分帧解码器（raw 模式逐 Read 上报，行为同旧版本）；
	// UDP 为数据报协议，天然有边界，不做分帧处理
	var decoder *tcpproto.StreamDecoder
	if conn != nil && e.isTCP() {
		decoder = e.frame.NewDecoder()
	}
	buf := make([]byte, 65536)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		var n int
		var src string
		var err error
		if conn != nil {
			n, err = conn.Read(buf)
			src = conn.RemoteAddr().String()
		} else if pc != nil {
			var raddr net.Addr
			n, raddr, err = pc.ReadFrom(buf)
			if raddr != nil {
				src = raddr.String()
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			e.cb.OnError(e.cfg.ID, fmt.Errorf("读取失败: %w", err))
			return
		}
		if decoder != nil {
			frames, derr := decoder.Feed(buf[:n])
			if derr != nil {
				applog.Errorf("[session %s] TCP 分帧解码失败: %v", e.cfg.ID, derr)
				e.cb.OnError(e.cfg.ID, fmt.Errorf("TCP 分帧解码失败: %w", derr))
				return
			}
			if len(frames) == 0 {
				continue // 半包：等待更多字节
			}
			for _, frame := range frames {
				e.emitReceive(frame, src)
			}
			e.stats.IncReceived(int64(len(frames)), int64(n))
			continue
		}
		e.emitReceive(buf[:n], src)
		e.stats.IncReceived(1, int64(n))
	}
}

// emitReceive 推送接收报文事件
func (e *Engine) emitReceive(data []byte, source string) {
	if e.recvPause.Load() {
		return
	}
	evt := models.ReceiveEvent{
		SessionID: e.cfg.ID,
		Timestamp: time.Now(),
		Source:    source,
		Data:      e.decodePayload(data),
		HexData:   hex.EncodeToString(data),
	}
	e.cb.OnReceive(evt)
}

// sendEncoding 发送编码：优先 sendEncoding，未配置回落通用 encoding
func (e *Engine) sendEncoding() models.Encoding {
	if e.cfg.SendEncoding != "" {
		return e.cfg.SendEncoding
	}
	return e.cfg.Encoding
}

// recvEncoding 接收编码：优先 recvEncoding，未配置回落通用 encoding
func (e *Engine) recvEncoding() models.Encoding {
	if e.cfg.RecvEncoding != "" {
		return e.cfg.RecvEncoding
	}
	return e.cfg.Encoding
}

// decodePayload 按接收编码解码为字符串
func (e *Engine) decodePayload(data []byte) string {
	if e.recvEncoding() == models.EncodingGBK {
		out, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), data)
		if err == nil {
			return string(out)
		}
	}
	return string(data)
}

// encodePayload 按发送编码将文本编码为字节
func (e *Engine) encodePayload(text string) []byte {
	if e.sendEncoding() == models.EncodingGBK {
		out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewEncoder(), []byte(text))
		if err == nil {
			return out
		}
	}
	return []byte(text)
}

// === 发送主循环 ===

func (e *Engine) sendLoop(ctx context.Context) {
	switch e.cfg.Mode {
	case models.SendModeInterval:
		e.runIntervalMode(ctx)
	case models.SendModeRate:
		e.runRateMode(ctx)
	}
}

// runIntervalMode 模式A：按毫秒间隔定时发送
func (e *Engine) runIntervalMode(ctx context.Context) {
	interval := time.Duration(e.cfg.IntervalMS) * time.Millisecond
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	start := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if e.checkStop(ctx, start) {
			return
		}
		if err := e.dispatchOne(); err != nil {
			// 正常停止竞态：Stop 已关闭连接但本 goroutine 尚未感知 ctx 取消，静默退出
			if ctx.Err() != nil || !e.running.Load() {
				return
			}
			e.cb.OnError(e.cfg.ID, err)
			return
		}
	}
}

// runRateMode 模式B：精准按条/秒速率压测（核心能力）
// 使用基于时间补偿的令牌桶，保证长时间平均速率精准
func (e *Engine) runRateMode(ctx context.Context) {
	rate := e.cfg.RatePerSec
	if rate <= 0 {
		rate = 1
	}
	// 限流安全上限保护（防止打满 CPU）
	maxRate := 200000
	if rate > maxRate {
		rate = maxRate
	}
	interval := time.Second / time.Duration(rate)
	// 高速率时分批降低调度开销
	var batch int = 1
	if rate >= 10000 {
		batch = rate / 1000
		if batch < 1 {
			batch = 1
		}
	}
	start := time.Now()
	nextDeadline := start
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		// 时间补偿：保证按速率精准推进
		now := time.Now()
		if now.Before(nextDeadline) {
			time.Sleep(nextDeadline.Sub(now))
		}
		for i := 0; i < batch; i++ {
			if e.checkStop(ctx, start) {
				return
			}
			if err := e.dispatchOne(); err != nil {
				// 正常停止竞态：Stop 已关闭连接但本 goroutine 尚未感知 ctx 取消，静默退出
				if ctx.Err() != nil || !e.running.Load() {
					return
				}
				e.cb.OnError(e.cfg.ID, err)
				return
			}
		}
		nextDeadline = nextDeadline.Add(time.Duration(batch) * interval)
	}
}

// checkStop 检查停止条件，满足则停止并返回 true
func (e *Engine) checkStop(ctx context.Context, start time.Time) bool {
	switch e.cfg.StopCondition {
	case models.StopByCount:
		if e.stats.Snapshot().SessionSent >= e.cfg.StopCount {
			applog.Printf("[session %s] 已达到总条数 %d，自动停止", e.cfg.ID, e.cfg.StopCount)
			e.Stop()
			return true
		}
	case models.StopByDuration:
		if time.Since(start) >= time.Duration(e.cfg.StopDurationSec)*time.Second {
			applog.Printf("[session %s] 已达到运行时长 %d 秒，自动停止", e.cfg.ID, e.cfg.StopDurationSec)
			e.Stop()
			return true
		}
	}
	return false
}

// dispatchOne 发送单个报文（根据样本策略选择内容，多目标场景下发到全部目标）
func (e *Engine) dispatchOne() error {
	payload := e.pickPayload()
	// TCP/UDP 客户端单连接：通过已连接 socket 发送
	if conn := e.currentConn(); conn != nil {
		// TCP 协议化：按分帧配置封帧后再写入（UDP/raw 模式返回原字节）
		wire := e.framePayload(payload)
		_, err := conn.Write(wire)
		if err != nil {
			applog.Errorf("[session %s] 发送失败 (%s): %v", e.cfg.ID, conn.RemoteAddr(), err)
			// 自动重连场景：连接断开/重连中空窗仅跳过本轮，不中断 sendLoop，
			// 由 tcpClientConnLoop 负责恢复；未开启重连维持原行为（报错停止）
			if e.cfg.Protocol == models.ProtocolTCPClient && e.cfg.AutoReconnect {
				applog.Printf("[session %s] 连接暂不可用，跳过本轮发送，等待自动重连", e.cfg.ID)
				return nil
			}
			return err
		}
		applog.Printf("[session %s] 已发送 %d 字节 → %s", e.cfg.ID, len(wire), conn.RemoteAddr())
		e.stats.IncSent(1, int64(len(wire)))
		return nil
	}
	// TCP 客户端无连接（重连中空窗或已断开）
	if e.cfg.Protocol == models.ProtocolTCPClient {
		if e.cfg.AutoReconnect {
			applog.Printf("[session %s] 连接未就绪（重连中），跳过本轮发送", e.cfg.ID)
			return nil
		}
		return fmt.Errorf("无可用发送通道：TCP 连接未建立")
	}
	// UDP 客户端未连接 socket（多目标/广播）：WriteTo 到每个目标
	if e.udpConn != nil && e.cfg.Protocol == models.ProtocolUDPClient {
		targets := e.multiTargets()
		sent := int64(0)
		for _, addr := range targets {
			ua, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				continue
			}
			if e.cfg.Broadcast {
				ua.IP = net.IPv4bcast
			}
			if _, err := e.udpConn.WriteTo(payload, ua); err == nil {
				sent++
			}
		}
		if sent > 0 {
			e.stats.IncSent(sent, sent*int64(len(payload)))
		}
		return nil
	}
	// TCP 服务端：广播到所有已连客户端
	if e.cfg.Protocol == models.ProtocolTCPServer {
		e.broadcastTCP(payload)
		return nil
	}
	// UDP 服务端：发送到配置的目标
	if e.cfg.Protocol == models.ProtocolUDPServer && e.udpConn != nil {
		targets := e.multiTargets()
		sent := int64(0)
		for _, addr := range targets {
			ua, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				continue
			}
			if _, err := e.udpConn.WriteTo(payload, ua); err == nil {
				sent++
			}
		}
		if sent > 0 {
			e.stats.IncSent(sent, sent*int64(len(payload)))
		}
		return nil
	}
	return fmt.Errorf("无可用发送通道")
}

// broadcastTCP TCP 服务端广播到所有已连客户端（按会话分帧配置统一封帧一次）
func (e *Engine) broadcastTCP(payload []byte) {
	wire := e.framePayload(payload)
	e.mu.Lock()
	defer e.mu.Unlock()
	sent := int64(0)
	bytes := int64(0)
	for _, c := range e.conns {
		n, err := c.Write(wire)
		if err == nil {
			sent++
			bytes += int64(n)
		}
	}
	if sent > 0 {
		e.stats.IncSent(sent, bytes)
	}
}

// initPayloadLines 将报文文本按行拆分为非空行样本（读取策略数据源）。
// 顺序/随机策略均从该快照选行；单行或空文本时退回整段发送，行为与旧版一致。
func (e *Engine) initPayloadLines() {
	e.lines = e.lines[:0]
	for _, ln := range strings.Split(e.cfg.PayloadText, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if strings.TrimSpace(ln) == "" {
			continue // 空行跳过
		}
		e.lines = append(e.lines, ln)
	}
}

// pickPayload 根据样本策略选择本次发送的报文内容
func (e *Engine) pickPayload() []byte {
	text := e.cfg.PayloadText
	// 读取策略：多行样本按策略逐行选取（空行已在 Start 时剔除）。
	// sequential 顺序轮转、random 随机选取；单行时使用整段原文。
	if n := len(e.lines); n > 1 {
		if e.cfg.ReadStrategy == models.ReadRandom {
			e.rngMu.Lock()
			text = e.lines[e.rng.Intn(n)]
			e.rngMu.Unlock()
		} else {
			idx := int(atomic.AddInt64(&e.lineSeq, 1)-1) % n
			text = e.lines[idx]
		}
	}
	// 若启用模板则使用模板内容
	if e.cfg.TemplateID != "" && e.cfg.UseFile == false {
		// 模板内容由会话层注入到 PayloadText，此处直接使用
	}
	// 参数化渲染
	if e.cfg.ParamEnabled {
		seq := atomic.AddInt64(&e.seq, 1)
		e.rngMu.Lock()
		text = template.Render(text, seq, e.rng)
		e.rngMu.Unlock()
	}
	// Syslog 封装
	encoded := syslog.WrapIfNeeded(e.cfg, text)
	// 发送编码（UTF-8/GBK）：所有发送路径（dispatchOne/dispatchOnePayload/SendOnce）
	// 均经由本方法或 SendOnce 同一编码点，编码选择在此统一生效
	return e.encodePayload(string(encoded))
}

// SendOnce 单次手动发送
func (e *Engine) SendOnce(payloadText string) error {
	if !e.running.Load() {
		// 单次模式下允许在未启动状态发送（需要先建立连接）
		if e.cfg.Protocol == models.ProtocolTCPClient || e.cfg.Protocol == models.ProtocolUDPClient {
			// 懒启动同样需要可取消的生命周期 ctx：Stop 时能立即终止接收/重连协程
			ctx, cancel := context.WithCancel(context.Background())
			e.mu.Lock()
			e.ctx, e.cancel = ctx, cancel
			e.mu.Unlock()
			if err := e.dialClient(ctx); err != nil {
				cancel()
				e.mu.Lock()
				e.ctx, e.cancel = nil, nil
				e.mu.Unlock()
				e.cb.OnError(e.cfg.ID, err)
				return err
			}
			e.running.Store(true)
			e.stats.Start()
			e.cb.OnStatus(e.cfg.ID, models.StatusRunning)
			if e.cfg.Protocol == models.ProtocolTCPClient {
				go e.tcpClientConnLoop(ctx)
			} else {
				go e.recvLoop(ctx)
			}
		} else {
			// 服务端模式必须先 Start 监听
			return fmt.Errorf("服务端模式请先点击启动监听")
		}
	}
	text := payloadText
	if text == "" {
		text = e.cfg.PayloadText
	}
	if e.cfg.ParamEnabled && text != "" {
		seq := atomic.AddInt64(&e.seq, 1)
		e.rngMu.Lock()
		text = template.Render(text, seq, e.rng)
		e.rngMu.Unlock()
	}
	// 与 pickPayload 同一编码点：Syslog 封装后按发送编码（UTF-8/GBK）转为字节
	payload := e.encodePayload(string(syslog.WrapIfNeeded(e.cfg, text)))
	return e.dispatchOnePayload(payload)
}

// dispatchOnePayload 使用既定 payload 发送一次（复用 dispatchOne 的发送逻辑）
func (e *Engine) dispatchOnePayload(payload []byte) error {
	if conn := e.currentConn(); conn != nil {
		wire := e.framePayload(payload)
		_, err := conn.Write(wire)
		if err == nil {
			e.stats.IncSent(1, int64(len(wire)))
		}
		return err
	}
	if e.udpConn != nil && e.cfg.Protocol == models.ProtocolUDPClient {
		targets := e.multiTargets()
		sent := int64(0)
		for _, addr := range targets {
			ua, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				continue
			}
			if e.cfg.Broadcast {
				ua.IP = net.IPv4bcast
			}
			if _, err := e.udpConn.WriteTo(payload, ua); err == nil {
				sent++
			}
		}
		if sent > 0 {
			e.stats.IncSent(sent, sent*int64(len(payload)))
		}
		return nil
	}
	if e.cfg.Protocol == models.ProtocolTCPServer {
		e.broadcastTCP(payload)
		return nil
	}
	if e.cfg.Protocol == models.ProtocolUDPServer && e.udpConn != nil {
		targets := e.multiTargets()
		sent := int64(0)
		for _, addr := range targets {
			ua, err := net.ResolveUDPAddr("udp", addr)
			if err != nil {
				continue
			}
			if _, err := e.udpConn.WriteTo(payload, ua); err == nil {
				sent++
			}
		}
		if sent > 0 {
			e.stats.IncSent(sent, sent*int64(len(payload)))
		}
		return nil
	}
	return fmt.Errorf("无可用发送通道")
}

// SampleLines 从文本拆分样本（按行）
func SampleLines(text string) [][]byte {
	lines := strings.Split(text, "\n")
	out := make([][]byte, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			out = append(out, []byte(l))
		}
	}
	return out
}

// HexDump 生成十六进制可读形式
func HexDump(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	buf := bytes.Buffer{}
	for i, b := range data {
		if i > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(fmt.Sprintf("%02x", b))
	}
	return buf.String()
}
