package sender

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"SuperSender/backend/models"
	"SuperSender/backend/stats"
)

// noopCBs 组装不推送事件的最小回调集合
func noopCBs() EventCallbacks {
	return EventCallbacks{
		OnReceive: func(_ models.ReceiveEvent) {},
		OnError:   func(_ string, _ error) {},
		OnStatus:  func(_ string, _ models.SessionStatus) {},
		OnStats:   func(_ models.StatsSnapshot) {},
	}
}

// TestTCPClient_AutoReconnect 验证断线自动重连状态机：
// 服务端接受首个连接后立即关闭 → 引擎读到 EOF → 按间隔自动重拨，
// 第二次连接建立后 SendOnce 的报文应到达新连接。
func TestTCPClient_AutoReconnect(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	var acceptCount atomic.Int32
	secondGot := make(chan []byte, 1)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			n := acceptCount.Add(1)
			if n == 1 {
				_ = c.Close() // 模拟对端断开
				continue
			}
			go func(c net.Conn) { // 第二次连接保持并读取一条报文
				defer c.Close()
				_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
				buf := make([]byte, 64)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				secondGot <- buf[:n]
			}(c)
		}
	}()

	cfg := &models.SessionConfig{
		ID:                  "ses-test-reconn",
		Name:                "reconn-test",
		Protocol:            models.ProtocolTCPClient,
		TargetIP:            "127.0.0.1",
		TargetPort:          ln.Addr().(*net.TCPAddr).Port,
		Mode:                models.SendModeSingle,
		AutoReconnect:       true,
		ReconnectIntervalMs: 100,
	}
	eng := New(cfg, stats.NewCollector(cfg.ID), noopCBs())
	if err := eng.Start(); err != nil {
		t.Fatalf("启动引擎失败: %v", err)
	}
	defer eng.Stop()

	// 等待第二次接入（自动重连成功）
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if acceptCount.Load() >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if acceptCount.Load() < 2 {
		t.Fatalf("未发生自动重连，Accept 次数=%d", acceptCount.Load())
	}

	if err := eng.SendOnce("z"); err != nil {
		t.Fatalf("重连后 SendOnce 失败: %v", err)
	}
	select {
	case got := <-secondGot:
		if string(got) != "z" {
			t.Fatalf("重连后报文不符: %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("超时未在重连后的新连接上收到报文")
	}
}

// TestTCPServer_EchoBackAndMaxConnections 验证：
// 1) EchoBack=true 时服务端把收到的原始字节原样回显；
// 2) MaxConnections=1 时第二个连接被拒绝（服务端主动关闭）。
func TestTCPServer_EchoBackAndMaxConnections(t *testing.T) {
	cfg := &models.SessionConfig{
		ID:             "ses-test-echo",
		Name:           "echo-test",
		Protocol:       models.ProtocolTCPServer,
		SourceIP:       "127.0.0.1",
		SourcePort:     0,
		TargetPort:     0,
		Mode:           models.SendModeSingle,
		MaxConnections: 1,
		EchoBack:       true,
	}
	eng := New(cfg, stats.NewCollector(cfg.ID), noopCBs())
	if err := eng.Start(); err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	defer eng.Stop()

	eng.mu.Lock()
	addr := eng.listener.Addr().String()
	eng.mu.Unlock()

	c1, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("首个连接失败: %v", err)
	}
	defer c1.Close()

	// 等待服务端登记连接
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && eng.ClientCount() != 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if eng.ClientCount() != 1 {
		t.Fatalf("首个连接未登记，count=%d", eng.ClientCount())
	}

	// 回显：发送原始字节，应原样收到
	if _, err := c1.Write([]byte("ping-echo")); err != nil {
		t.Fatal(err)
	}
	_ = c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 32)
	n, err := c1.Read(buf)
	if err != nil {
		t.Fatalf("读取回显失败: %v", err)
	}
	if string(buf[:n]) != "ping-echo" {
		t.Fatalf("回显内容不符: %q", buf[:n])
	}

	// 第二个连接：达上限应被拒绝关闭
	c2, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		// 某些平台拒绝直接表现为拨号失败，同样符合预期
		return
	}
	defer c2.Close()
	_ = c2.SetReadDeadline(time.Now().Add(2 * time.Second))
	one := make([]byte, 1)
	if _, err := c2.Read(one); err == nil {
		t.Fatal("达到最大连接数的新连接应被服务端关闭")
	}
	if eng.ClientCount() != 1 {
		t.Fatalf("被拒绝连接不应计入，count=%d", eng.ClientCount())
	}
}
