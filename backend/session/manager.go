// Package session 会话管理器：维护多会话生命周期、goroutine 调度、
// 通过事件回调向前端推送统计/接收报文/状态。停止时彻底释放资源，杜绝泄漏。
package session

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"SuperSender/backend/applog"
	"SuperSender/backend/models"
	"SuperSender/backend/sender"
	"SuperSender/backend/stats"
	"SuperSender/backend/storage"
	"SuperSender/backend/template"
)

// pkgRand 包级随机源，用于生成会话ID
var pkgRand = rand.New(rand.NewSource(time.Now().UnixNano()))
var pkgRandMu sync.Mutex

// EmitFunc 推送事件到前端（由 app.go 注入 runtime.EventsEmit）
type EmitFunc func(event string, data ...any)

// Session 单会话运行态
type Session struct {
	ID     string
	Config models.SessionConfig
	Engine *sender.Engine
	Stats  *stats.Collector
	Status models.SessionStatus

	mu          sync.Mutex
	receiveBuf  []models.ReceiveEvent
	recvMax     int // 接收区最大缓存条数
	statsCancel context.CancelFunc

	// 实时落盘：开启后每收到一条报文即追加到文件
	realTimeLogMu   sync.Mutex
	realTimeLogFile *os.File
	realTimeLogPath string
}

// writeRealTimeLog 将接收报文追加写入实时落盘文件（如已开启）
func (s *Session) writeRealTimeLog(evt models.ReceiveEvent) {
	s.realTimeLogMu.Lock()
	defer s.realTimeLogMu.Unlock()
	if s.realTimeLogFile == nil {
		return
	}
	line := fmt.Sprintf("[%s] %s %s\n", evt.Timestamp.Format(time.RFC3339), evt.Source, evt.Data)
	_, _ = s.realTimeLogFile.WriteString(line)
}

// closeRealTimeLog 关闭实时落盘文件句柄
func (s *Session) closeRealTimeLog() {
	s.realTimeLogMu.Lock()
	defer s.realTimeLogMu.Unlock()
	if s.realTimeLogFile != nil {
		_ = s.realTimeLogFile.Close()
		s.realTimeLogFile = nil
		s.realTimeLogPath = ""
	}
}

// EnableReceiveLog 开启实时落盘到指定路径
func (m *Manager) EnableReceiveLog(id, path string) error {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话不存在: %s", id)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("打开落盘文件失败: %w", err)
	}
	s.closeRealTimeLog()
	s.realTimeLogMu.Lock()
	s.realTimeLogFile = f
	s.realTimeLogPath = path
	s.realTimeLogMu.Unlock()
	applog.Printf("[session %s] 开启接收实时落盘 → %s", id, path)
	return nil
}

// DisableReceiveLog 关闭实时落盘
func (m *Manager) DisableReceiveLog(id string) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return
	}
	s.closeRealTimeLog()
}

// GetReceiveLogStatus 查询实时落盘状态（前端切换会话后恢复勾选显示）
func (m *Manager) GetReceiveLogStatus(id string) models.ReceiveLogStatus {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return models.ReceiveLogStatus{}
	}
	s.realTimeLogMu.Lock()
	defer s.realTimeLogMu.Unlock()
	return models.ReceiveLogStatus{Enabled: s.realTimeLogFile != nil, Path: s.realTimeLogPath}
}

// Manager 会话管理器
type Manager struct {
	store *storage.Storage
	tmpl  *template.Manager
	emit  EmitFunc

	mu       sync.RWMutex
	sessions map[string]*Session
	order    []string // 会话在侧边栏中的稳定展示顺序（id 列表，拖拽排序持久化）
}

// New 创建会话管理器并从磁盘加载历史会话
func New(store *storage.Storage, tmpl *template.Manager, emit EmitFunc) *Manager {
	m := &Manager{
		store:    store,
		tmpl:     tmpl,
		emit:     emit,
		sessions: make(map[string]*Session),
	}
	m.loadFromDisk()
	return m
}

// loadFromDisk 从本地 JSON 加载会话配置
// 直接复用 newSession，保证磁盘加载的会话与新建会话回调链一致
func (m *Manager) loadFromDisk() {
	list, err := m.store.LoadSessions()
	if err != nil {
		applog.Errorf("[session] 加载历史会话失败: %v", err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range list {
		cfg := list[i]
		s := m.newSession(cfg)
		m.sessions[cfg.ID] = s
		m.order = append(m.order, cfg.ID)
	}
	applog.Printf("[session] 已从磁盘加载 %d 个会话", len(list))
}

// CreateSession 新建会话
func (m *Manager) CreateSession(cfg models.SessionConfig) (models.SessionConfig, error) {
	if cfg.ID == "" {
		cfg.ID = "ses-" + randID(8)
	}
	now := time.Now()
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = now
	}
	cfg.UpdatedAt = now
	s := m.newSession(cfg)
	m.mu.Lock()
	m.sessions[cfg.ID] = s
	m.order = append(m.order, cfg.ID) // 新会话固定追加到列表末尾
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		applog.Errorf("[session] 新建会话 %s(%s) 持久化失败: %v", cfg.ID, cfg.Name, err)
		return cfg, err
	}
	applog.Printf("[session] 新建会话 %s「%s」(%s)", cfg.ID, cfg.Name, cfg.Protocol)
	return cfg, nil
}

// newSession 构造会话实例（注入引擎与事件回调）
func (m *Manager) newSession(cfg models.SessionConfig) *Session {
	s := &Session{
		ID:         cfg.ID,
		Config:     cfg,
		Stats:      stats.NewCollector(cfg.ID),
		Status:     models.StatusIdle,
		recvMax:    5000,
		receiveBuf: make([]models.ReceiveEvent, 0, 256),
	}
	s.Engine = sender.New(&s.Config, s.Stats, sender.EventCallbacks{
		OnStats: func(snap models.StatsSnapshot) {
			m.emit("stats:"+s.ID, snap)
		},
		OnReceive: func(evt models.ReceiveEvent) {
			s.pushReceive(evt)
			s.writeRealTimeLog(evt)
			m.emit("recv:"+evt.SessionID, evt)
			applog.Printf("[session %s] 已推送 recv 事件到前端（%d 字节）", evt.SessionID, len(evt.Data))
		},
		OnError: func(sid string, err error) {
			applog.Errorf("[session %s] 引擎错误: %v", sid, err)
			m.emit("error:"+sid, err.Error())
		},
		OnStatus: func(sid string, st models.SessionStatus) {
			m.setSessionStatus(sid, st)
			m.emit("status:"+sid, string(st))
		},
	})
	return s
}

// pushReceive 写入接收区环形缓冲
func (s *Session) pushReceive(evt models.ReceiveEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receiveBuf = append(s.receiveBuf, evt)
	if len(s.receiveBuf) > s.recvMax {
		s.receiveBuf = s.receiveBuf[len(s.receiveBuf)-s.recvMax:]
	}
}

// GetReceiveBuffer 返回当前接收区缓冲副本
func (m *Manager) GetReceiveBuffer(id string) []models.ReceiveEvent {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.ReceiveEvent, len(s.receiveBuf))
	copy(out, s.receiveBuf)
	return out
}

// ClearReceive 清空接收区缓冲
func (m *Manager) ClearReceive(id string) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return
	}
	s.mu.Lock()
	s.receiveBuf = s.receiveBuf[:0]
	s.mu.Unlock()
}

// ExportReceive 导出接收区到文件
func (m *Manager) ExportReceive(id, path string) error {
	buf := m.GetReceiveBuffer(id)
	if buf == nil {
		return fmt.Errorf("会话不存在: %s", id)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, evt := range buf {
		line := fmt.Sprintf("[%s] %s %s\n", evt.Timestamp.Format(time.RFC3339), evt.Source, evt.Data)
		if _, err := f.WriteString(line); err != nil {
			return err
		}
	}
	return nil
}

// ExportReceiveFiltered 仅导出接收区中 Data 包含关键字（大小写不敏感）的事件，返回命中条数。
// 过滤规则与前端查询浮层一致，保证"所见即所导"。
func (m *Manager) ExportReceiveFiltered(id, path, keyword string) (int, error) {
	buf := m.GetReceiveBuffer(id)
	if buf == nil {
		return 0, fmt.Errorf("会话不存在: %s", id)
	}
	kw := strings.ToLower(keyword)
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n := 0
	for _, evt := range buf {
		if kw != "" && !strings.Contains(strings.ToLower(evt.Data), kw) {
			continue
		}
		line := fmt.Sprintf("[%s] %s %s\n", evt.Timestamp.Format(time.RFC3339), evt.Source, evt.Data)
		if _, err := f.WriteString(line); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// orderedIDsLocked 返回稳定的会话顺序（调用方需持锁）。
// 以 order 为准；对 order 中缺失的会话（异常数据）追加末尾并就地修复，
// 对 order 中已不存在的 id 自动跳过，保证不会丢会话。
func (m *Manager) orderedIDsLocked() []string {
	seen := make(map[string]bool, len(m.sessions))
	out := make([]string, 0, len(m.sessions))
	for _, id := range m.order {
		if _, ok := m.sessions[id]; ok && !seen[id] {
			out = append(out, id)
			seen[id] = true
		}
	}
	// 容错：存在于 map 但不在 order 的会话，按 ID 排序后追加，顺序确定
	if len(out) < len(m.sessions) {
		var extra []string
		for id := range m.sessions {
			if !seen[id] {
				extra = append(extra, id)
			}
		}
		sort.Strings(extra)
		out = append(out, extra...)
	}
	m.order = append(m.order[:0], out...)
	return out
}

// ReorderSessions 按前端传入的 id 顺序重排侧边栏并持久化。
// 传入列表允许不完整：未包含的已有会话保持相对顺序追加到末尾。
func (m *Manager) ReorderSessions(ids []string) error {
	m.mu.Lock()
	wanted := make(map[string]bool, len(ids))
	newOrder := make([]string, 0, len(m.sessions))
	for _, id := range ids {
		if _, ok := m.sessions[id]; ok && !wanted[id] {
			newOrder = append(newOrder, id)
			wanted[id] = true
		}
	}
	// 未在拖拽结果中出现的会话，按原顺序补到末尾
	for _, id := range m.order {
		if _, ok := m.sessions[id]; ok && !wanted[id] {
			newOrder = append(newOrder, id)
			wanted[id] = true
		}
	}
	m.order = newOrder
	m.mu.Unlock()
	if err := m.persist(); err != nil {
		return err
	}
	applog.Printf("[session] 会话列表顺序已更新（共 %d 项）", len(newOrder))
	return nil
}

// ListSessions 返回会话摘要列表（按侧边栏稳定顺序）
func (m *Manager) ListSessions() []models.SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]models.SessionInfo, 0, len(m.sessions))
	for _, id := range m.order {
		s, ok := m.sessions[id]
		if !ok {
			continue
		}
		target := fmt.Sprintf("%s:%d", s.Config.TargetIP, s.Config.TargetPort)
		if s.Config.EnableMulti && len(s.Config.Targets) > 0 {
			target = fmt.Sprintf("multi(%d)", len(s.Config.Targets))
		}
		st := s.Status
		out = append(out, models.SessionInfo{
			ID:       s.ID,
			Name:     s.Config.Name,
			Protocol: s.Config.Protocol,
			Status:   st,
			Target:   target,
		})
	}
	return out
}

// GetConfig 获取会话配置
func (m *Manager) GetConfig(id string) (models.SessionConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return models.SessionConfig{}, false
	}
	return s.Config, true
}

// UpdateConfig 更新会话配置（运行中禁止修改）
func (m *Manager) UpdateConfig(id string, cfg models.SessionConfig) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("会话不存在: %s", id)
	}
	if s.Engine != nil && s.Engine.IsRunning() {
		m.mu.Unlock()
		return fmt.Errorf("会话运行中，禁止修改配置")
	}
	cfg.ID = id
	cfg.UpdatedAt = time.Now()
	if cfg.CreatedAt.IsZero() {
		cfg.CreatedAt = s.Config.CreatedAt
	}
	s.Config = cfg
	// 重建引擎以绑定新配置；保留 realTimeLogFile 状态以承接落盘
	s.Engine = sender.New(&s.Config, s.Stats, sender.EventCallbacks{
		OnStats: func(snap models.StatsSnapshot) {
			m.emit("stats:"+s.ID, snap)
		},
		OnReceive: func(evt models.ReceiveEvent) {
			s.pushReceive(evt)
			s.writeRealTimeLog(evt)
			m.emit("recv:"+evt.SessionID, evt)
			applog.Printf("[session %s] 已推送 recv 事件到前端（%d 字节）", evt.SessionID, len(evt.Data))
		},
		OnError: func(sid string, err error) {
			applog.Errorf("[session %s] 引擎错误: %v", sid, err)
			m.emit("error:"+sid, err.Error())
		},
		OnStatus: func(sid string, st models.SessionStatus) {
			m.setSessionStatus(sid, st)
			m.emit("status:"+sid, string(st))
		},
	})
	m.mu.Unlock()
	applog.Printf("[session] 更新会话 %s「%s」配置", id, cfg.Name)
	return m.persist()
}

// DeleteSession 删除会话并停止其 goroutine
func (m *Manager) DeleteSession(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("会话不存在: %s", id)
	}
	delete(m.sessions, id)
	// 同步从有序列表移除
	for i, oid := range m.order {
		if oid == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
	m.mu.Unlock()
	if s.Engine != nil {
		s.Engine.Stop()
	}
	if s.statsCancel != nil {
		s.statsCancel()
	}
	s.closeRealTimeLog()
	m.persist()
	applog.Printf("[session] 删除会话 %s「%s」", id, s.Config.Name)
	return nil
}

// Start 启动会话发包
func (m *Manager) Start(id string) error {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话不存在: %s", id)
	}
	if err := s.Engine.Start(); err != nil {
		return err
	}
	// 启动时重置实时统计：清零累计发送/接收计数（本次计数与速率历史由 Engine.Start 内的 stats.Start 清零），
	// 并立即推送一次快照，让界面统计马上归零而不必等下一个统计周期
	s.Stats.ResetTotal()
	m.emit("stats:"+id, s.Stats.Snapshot())
	applog.Printf("[session] 启动会话 %s「%s」(%s)", id, s.Config.Name, s.Config.Protocol)
	// 启动统计推送协程
	ctx, cancel := context.WithCancel(context.Background())
	s.statsCancel = cancel
	go m.statsTicker(ctx, s)
	return nil
}

// Stop 停止会话发包
func (m *Manager) Stop(id string) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return
	}
	applog.Printf("[session] 手动停止会话 %s「%s」", id, s.Config.Name)
	if s.statsCancel != nil {
		s.statsCancel()
		s.statsCancel = nil
	}
	s.Engine.Stop()
}

// StopAll 停止全部会话（应用退出时调用，同时关闭落盘文件）
func (m *Manager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if s.statsCancel != nil {
			s.statsCancel()
		}
		if s.Engine != nil {
			s.Engine.Stop()
		}
		s.closeRealTimeLog()
	}
	applog.Printf("[session] 应用退出，已停止全部 %d 个会话", len(m.sessions))
}

// SendOnce 单次发送
func (m *Manager) SendOnce(id, payload string) error {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("会话不存在: %s", id)
	}
	return s.Engine.SendOnce(payload)
}

// PauseReceive / ResumeReceive
func (m *Manager) PauseReceive(id string) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if ok {
		s.Engine.PauseReceive()
	}
}
func (m *Manager) ResumeReceive(id string) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if ok {
		s.Engine.ResumeReceive()
	}
}

// ClearStats 清空计数
func (m *Manager) ClearStats(id string) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if ok {
		s.Stats.ResetTotal()
	}
}

// GetSnapshot 获取实时统计
func (m *Manager) GetSnapshot(id string) (models.StatsSnapshot, bool) {
	m.mu.RLock()
	s, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return models.StatsSnapshot{}, false
	}
	snap := s.Stats.Snapshot()
	snap.ClientCount = s.Engine.ClientCount()
	return snap, true
}

// setSessionStatus 设置会话状态（线程安全）
func (m *Manager) setSessionStatus(id string, st models.SessionStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Status = st
	}
}

// statsTicker 定时推送统计快照
func (m *Manager) statsTicker(ctx context.Context, s *Session) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := s.Stats.Snapshot()
			snap.ClientCount = s.Engine.ClientCount()
			m.emit("stats:"+s.ID, snap)
		}
	}
}

// persist 持久化全部会话配置
func (m *Manager) persist() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := m.orderedIDsLocked()
	list := make([]models.SessionConfig, 0, len(ids))
	for _, id := range ids {
		if s, ok := m.sessions[id]; ok {
			list = append(list, s.Config)
		}
	}
	return m.store.SaveSessions(list)
}

// PersistAll 显式持久化（用于 shutdown）
func (m *Manager) PersistAll() {
	_ = m.persist()
}

// SaveReceiveToDir 实时落盘到指定目录
func (m *Manager) SaveReceiveToDir(id, dir string) error {
	buf := m.GetReceiveBuffer(id)
	if buf == nil {
		return fmt.Errorf("会话不存在: %s", id)
	}
	path := filepath.Join(dir, "recv-"+id+".log")
	return m.ExportReceive(id, path)
}

func randID(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	pkgRandMu.Lock()
	defer pkgRandMu.Unlock()
	for i := range b {
		b[i] = letters[pkgRand.Intn(len(letters))]
	}
	return string(b)
}
