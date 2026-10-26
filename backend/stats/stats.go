// Package stats 负责单会话实时统计：累计/本次发送、接收计数、发送与接收侧速率、运行时长。
// 使用滑动窗口计算实时速率，goroutine 安全。
package stats

import (
	"sync"
	"sync/atomic"
	"time"

	"SuperSender/backend/models"
)

// Collector 单会话统计收集器
type Collector struct {
	sessionID      string
	totalSent      int64 // 累计发送（跨本次运行）
	sessionSent    int64 // 本次运行发送
	received       int64 // 接收计数
	totalBytesSent int64 // 累计发送字节
	totalBytesRecv int64 // 累计接收字节
	startTime      time.Time
	running        atomic.Bool

	// 滑动窗口用于实时速率（发送/接收各一组）
	mu          sync.Mutex
	samples     []rateSample // 发送样本
	recvSamples []rateSample // 接收样本
	windowMS    int64        // 采样窗口长度（毫秒）
}

type rateSample struct {
	ts    time.Time
	count int64
	bytes int64
}

// NewCollector 创建统计收集器
func NewCollector(sessionID string) *Collector {
	return &Collector{
		sessionID: sessionID,
		windowMS:  1000, // 1秒滑动窗口
	}
}

// Start 标记开始运行
func (c *Collector) Start() {
	c.startTime = time.Now()
	c.running.Store(true)
	atomic.StoreInt64(&c.sessionSent, 0)
	c.mu.Lock()
	c.samples = c.samples[:0]
	c.recvSamples = c.recvSamples[:0]
	c.mu.Unlock()
}

// Stop 标记停止运行
func (c *Collector) Stop() {
	c.running.Store(false)
}

// IncSent 发送计数+N（N 条、bytes 字节）
func (c *Collector) IncSent(n int64, bytes int64) {
	atomic.AddInt64(&c.totalSent, n)
	atomic.AddInt64(&c.sessionSent, n)
	atomic.AddInt64(&c.totalBytesSent, bytes)
	now := time.Now()
	c.mu.Lock()
	c.samples = append(c.samples, rateSample{ts: now, count: n, bytes: bytes})
	c.samples = evictSamples(c.samples, c.windowMS, now)
	c.mu.Unlock()
}

// IncReceived 接收计数+N（N 条、bytes 字节）
func (c *Collector) IncReceived(n int64, bytes int64) {
	atomic.AddInt64(&c.received, n)
	atomic.AddInt64(&c.totalBytesRecv, bytes)
	now := time.Now()
	c.mu.Lock()
	c.recvSamples = append(c.recvSamples, rateSample{ts: now, count: n, bytes: bytes})
	c.recvSamples = evictSamples(c.recvSamples, c.windowMS, now)
	c.mu.Unlock()
}

// ResetTotal 重置累计计数
func (c *Collector) ResetTotal() {
	atomic.StoreInt64(&c.totalSent, 0)
	atomic.StoreInt64(&c.totalBytesSent, 0)
	atomic.StoreInt64(&c.totalBytesRecv, 0)
	atomic.StoreInt64(&c.received, 0)
}

// Snapshot 生成快照
func (c *Collector) Snapshot() models.StatsSnapshot {
	now := time.Now()
	c.mu.Lock()
	c.samples = evictSamples(c.samples, c.windowMS, now)
	c.recvSamples = evictSamples(c.recvSamples, c.windowMS, now)
	rate, bps := sumRate(c.samples, c.windowMS)
	recvRate, recvBps := sumRate(c.recvSamples, c.windowMS)
	c.mu.Unlock()
	dur := int64(0)
	if c.running.Load() {
		dur = int64(now.Sub(c.startTime).Seconds())
	}
	return models.StatsSnapshot{
		SessionID:       c.sessionID,
		TotalSent:       atomic.LoadInt64(&c.totalSent),
		SessionSent:     atomic.LoadInt64(&c.sessionSent),
		Received:        atomic.LoadInt64(&c.received),
		RatePerSec:      rate,
		BytesPerSec:     bps,
		DurationSec:     dur,
		TotalBytesSent:  atomic.LoadInt64(&c.totalBytesSent),
		TotalBytesRecv:  atomic.LoadInt64(&c.totalBytesRecv),
		RecvRatePerSec:  recvRate,
		RecvBytesPerSec: recvBps,
	}
}

// evictSamples 清除窗口外的过期样本，返回裁剪后的切片
func evictSamples(samples []rateSample, windowMS int64, now time.Time) []rateSample {
	cutoff := now.Add(-time.Duration(windowMS) * time.Millisecond)
	idx := 0
	for ; idx < len(samples); idx++ {
		if samples[idx].ts.After(cutoff) {
			break
		}
	}
	if idx > 0 {
		return samples[idx:]
	}
	return samples
}

// sumRate 计算窗口内速率（条/秒、字节/秒），需在持有 c.mu 时调用
func sumRate(samples []rateSample, windowMS int64) (float64, float64) {
	if len(samples) == 0 {
		return 0, 0
	}
	var sumCount, sumBytes int64
	for _, s := range samples {
		sumCount += s.count
		sumBytes += s.bytes
	}
	elapsed := float64(windowMS) / 1000.0
	if elapsed <= 0 {
		return 0, 0
	}
	return float64(sumCount) / elapsed, float64(sumBytes) / elapsed
}
