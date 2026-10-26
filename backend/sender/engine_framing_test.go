package sender

import (
	"io"
	"net"
	"testing"
	"time"

	"SuperSender/backend/models"
	"SuperSender/backend/stats"
)

// TestTCPLengthFraming_EngineRoundTrip 通过本地 TCP 回环验证引擎协议化全链路：
// 客户端（长度前缀 4 字节大端）发送 "hi" → 对端收到 [0,0,0,2,'h','i']；
// 对端回送同协议帧 "ok" → 引擎接收协程拆出一条 "ok" 报文事件。
func TestTCPLengthFraming_EngineRoundTrip(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	received := make(chan string, 1)
	cfg := &models.SessionConfig{
		ID:         "ses-test-frame",
		Name:       "frame-test",
		Protocol:   models.ProtocolTCPClient,
		TargetIP:   "127.0.0.1",
		TargetPort: ln.Addr().(*net.TCPAddr).Port,
		Mode:       models.SendModeSingle,
		TCP: &models.TCPFrameConfig{
			Mode:        models.TCPFrameLength,
			HeaderBytes: 4,
			ByteOrder:   "big",
		},
	}
	eng := New(cfg, stats.NewCollector(cfg.ID), EventCallbacks{
		OnReceive: func(evt models.ReceiveEvent) {
			select {
			case received <- evt.Data:
			default:
			}
		},
		OnError:  func(_ string, e error) { t.Logf("引擎错误回调: %v", e) },
		OnStatus: func(_ string, _ models.SessionStatus) {},
		OnStats:  func(_ models.StatsSnapshot) {},
	})

	if err := eng.Start(); err != nil {
		t.Fatalf("启动引擎失败: %v", err)
	}
	defer eng.Stop()

	srvConn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer srvConn.Close()

	// 客户端封帧发送
	if err := eng.SendOnce("hi"); err != nil {
		t.Fatalf("SendOnce 失败: %v", err)
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(srvConn, head); err != nil {
		t.Fatalf("读取长度头失败: %v", err)
	}
	if string(head) != string([]byte{0, 0, 0, 2}) {
		t.Fatalf("长度头不符: %v", head)
	}
	body := make([]byte, 2)
	if _, err := io.ReadFull(srvConn, body); err != nil {
		t.Fatalf("读取报文体失败: %v", err)
	}
	if string(body) != "hi" {
		t.Fatalf("报文体不符: %q", body)
	}

	// 对端按同协议回送，验证引擎分帧接收
	if _, err := srvConn.Write([]byte{0, 0, 0, 2, 'o', 'k'}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got != "ok" {
			t.Fatalf("引擎拆帧内容不符: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时未收到引擎拆出的报文")
	}
}

// TestTCPRawMode_BackwardCompatible 验证未配置分帧（nil）时裸流行为不变：
// 对端一次 Read 拿到的整块数据即一条接收事件。
func TestTCPRawMode_BackwardCompatible(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	received := make(chan string, 1)
	cfg := &models.SessionConfig{
		ID:         "ses-test-raw",
		Name:       "raw-test",
		Protocol:   models.ProtocolTCPClient,
		TargetIP:   "127.0.0.1",
		TargetPort: ln.Addr().(*net.TCPAddr).Port,
		Mode:       models.SendModeSingle,
		TCP:        nil, // 旧版本会话无分帧配置
	}
	eng := New(cfg, stats.NewCollector(cfg.ID), EventCallbacks{
		OnReceive: func(evt models.ReceiveEvent) {
			select {
			case received <- evt.Data:
			default:
			}
		},
		OnError:  func(_ string, e error) { t.Logf("引擎错误回调: %v", e) },
		OnStatus: func(_ string, _ models.SessionStatus) {},
		OnStats:  func(_ models.StatsSnapshot) {},
	})
	if err := eng.Start(); err != nil {
		t.Fatalf("启动引擎失败: %v", err)
	}
	defer eng.Stop()

	srvConn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer srvConn.Close()

	// 裸流发送：不应附加任何帧字节
	if err := eng.SendOnce("abc"); err != nil {
		t.Fatalf("SendOnce 失败: %v", err)
	}
	buf := make([]byte, 16)
	n, err := srvConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "abc" {
		t.Fatalf("裸流发送不应封帧，got %q", buf[:n])
	}

	// 裸流接收：整块上报
	if _, err := srvConn.Write([]byte("one-chunk")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-received:
		if got != "one-chunk" {
			t.Fatalf("裸流接收应整块上报，got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("超时未收到裸流报文")
	}
}
