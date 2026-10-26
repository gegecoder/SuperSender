package tcpproto

import (
	"testing"

	"SuperSender/backend/models"
)

// feedByBytes 每次仅喂 1 个字节，强制覆盖半包/粘包的极端拆分场景
func feedByBytes(t *testing.T, d *StreamDecoder, wire []byte) [][]byte {
	t.Helper()
	var got [][]byte
	for i := 0; i < len(wire); i++ {
		frames, err := d.Feed(wire[i : i+1])
		if err != nil {
			t.Fatalf("Feed 第 %d 字节返回错误: %v", i, err)
		}
		got = append(got, frames...)
	}
	return got
}

func assertFrames(t *testing.T, got [][]byte, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("帧数不符：got %d 帧 %q，want %d 帧 %q", len(got), got, len(want), want)
	}
	for i := range want {
		if string(got[i]) != want[i] {
			t.Fatalf("第 %d 帧内容不符：got %q，want %q", i, got[i], want[i])
		}
	}
}

func TestRawCodec_DefaultAndIdentity(t *testing.T) {
	// nil 配置、空 mode、未知 mode 均按裸流处理
	for _, cfg := range []*models.TCPFrameConfig{
		nil,
		{Mode: ""},
		{Mode: "weird"},
	} {
		c := New(cfg)
		if c.Mode() != models.TCPFrameRaw {
			t.Fatalf("期望裸流兜底，实际 %s", c.Mode())
		}
		payload := []byte("anything")
		if got := c.Encode(payload); string(got) != "anything" {
			t.Fatalf("裸流 Encode 不应改动报文，got %q", got)
		}
		d := c.NewDecoder()
		frames, err := d.Feed([]byte("ab"))
		if err != nil || len(frames) != 1 || string(frames[0]) != "ab" {
			t.Fatalf("裸流 Feed 应原样返回，frames=%q err=%v", frames, err)
		}
	}
}

func TestDelimiter_CRLF_RoundTrip(t *testing.T) {
	c := New(&models.TCPFrameConfig{Mode: models.TCPFrameDelimiter, Delimiter: `\r\n`})
	msgs := []string{"hello", "world", "SuperSender 你好"}
	var wire []byte
	for _, m := range msgs {
		wire = append(wire, c.Encode([]byte(m))...)
	}
	// 期望 hello\r\nworld\r\nSuperSender 你好\r\n
	if want := "hello\r\nworld\r\nSuperSender 你好\r\n"; string(wire) != want {
		t.Fatalf("封帧结果不符：%q", wire)
	}

	// 整包喂入：三帧
	d := c.NewDecoder()
	frames, err := d.Feed(wire)
	if err != nil {
		t.Fatal(err)
	}
	assertFrames(t, frames, msgs...)

	// 逐字节喂入（极端拆包）结果一致
	got := feedByBytes(t, c.NewDecoder(), wire)
	assertFrames(t, got, msgs...)

	// 半包：末尾分隔符未到齐时不吐帧
	d2 := c.NewDecoder()
	if f, _ := d2.Feed([]byte("abc\r")); len(f) != 0 {
		t.Fatalf("半包不应成帧，got %q", f)
	}
	f, _ := d2.Feed([]byte("\ndef"))
	assertFrames(t, f, "abc") // "def" 无结尾分隔符，继续缓冲
}

func TestDelimiter_DoubleAppendAvoided(t *testing.T) {
	c := New(&models.TCPFrameConfig{Mode: models.TCPFrameDelimiter, Delimiter: `\n`})
	got := c.Encode([]byte("already\n"))
	if string(got) != "already\n" {
		t.Fatalf("已以分隔符结尾时不应重复追加：%q", got)
	}
}

func TestDelimiter_NULByte(t *testing.T) {
	c := New(&models.TCPFrameConfig{Mode: models.TCPFrameDelimiter, Delimiter: `\x00`})
	wire := c.Encode([]byte("a"))
	wire = append(wire, c.Encode([]byte("b"))...) // a\0b\0
	frames, err := c.NewDecoder().Feed(wire)
	if err != nil {
		t.Fatal(err)
	}
	assertFrames(t, frames, "a", "b")
}

func TestFixed_RoundTrip(t *testing.T) {
	c := New(&models.TCPFrameConfig{Mode: models.TCPFrameFixed, FrameLength: 4})
	// 发送：不足整帧补 0x00
	if got := c.Encode([]byte("ab")); string(got) != "ab\x00\x00" {
		t.Fatalf("定长封帧补零错误：%q", got)
	}
	// 恰好整帧不补
	if got := c.Encode([]byte("abcd")); string(got) != "abcd" {
		t.Fatalf("整帧不应补字节：%q", got)
	}
	// 超过一帧：跨多帧，末帧补零
	if got := c.Encode([]byte("abcdef")); string(got) != "abcdef\x00\x00" {
		t.Fatalf("跨帧封帧错误：%q", got)
	}

	// 接收：跨 chunk 拼接后按 4 字节切帧
	d := c.NewDecoder()
	var got [][]byte
	f1, _ := d.Feed([]byte("abc"))   // 不够
	f2, _ := d.Feed([]byte("defgh")) // 补足第二帧、第三帧半包
	got = append(got, f1...)
	got = append(got, f2...)
	assertFrames(t, got, "abcd", "efgh")
}

func TestFixed_InvalidLengthFallsBackToRaw(t *testing.T) {
	c := New(&models.TCPFrameConfig{Mode: models.TCPFrameFixed, FrameLength: 0})
	if c.Mode() != models.TCPFrameRaw {
		t.Fatalf("非法固定长度应退化为裸流，实际 %s", c.Mode())
	}
}

func TestLengthPrefix_AllHeaderSizesAndOrders(t *testing.T) {
	body := "length-prefixed-message"
	cases := []struct {
		name        string
		headerBytes int
		byteOrder   string
		self        bool
	}{
		{"1字节", 1, "big", false},
		{"2字节大端", 2, "big", false},
		{"2字节小端", 2, "little", false},
		{"4字节大端不含头", 4, "big", false},
		{"4字节大端含头", 4, "big", true},
		{"4字节小端含头", 4, "little", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(&models.TCPFrameConfig{
				Mode:               models.TCPFrameLength,
				HeaderBytes:        tc.headerBytes,
				ByteOrder:          tc.byteOrder,
				LengthIncludesSelf: tc.self,
			})
			wire := c.Encode([]byte(body))
			if len(wire) != tc.headerBytes+len(body) {
				t.Fatalf("封帧总长度错误：got %d", len(wire))
			}
			// 整包
			frames, err := c.NewDecoder().Feed(wire)
			if err != nil {
				t.Fatal(err)
			}
			assertFrames(t, frames, body)
			// 逐字节（包头/包体任意边界拆分）
			got := feedByBytes(t, c.NewDecoder(), wire)
			assertFrames(t, got, body)
		})
	}
}

func TestLengthPrefix_StickyAndPartial(t *testing.T) {
	c := New(&models.TCPFrameConfig{Mode: models.TCPFrameLength, HeaderBytes: 2, ByteOrder: "big"})
	w1 := c.Encode([]byte("aaa"))
	w2 := c.Encode([]byte("bb"))
	wire := append(w1, w2...) // 粘包：两帧一次到达
	d := c.NewDecoder()
	frames, err := d.Feed(wire[:3]) // 仅够第一帧头部+1字节体
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatalf("第一帧未到齐不应成帧，got %q", frames)
	}
	frames, err = d.Feed(wire[3:])
	if err != nil {
		t.Fatal(err)
	}
	assertFrames(t, frames, "aaa", "bb")
}

func TestLengthPrefix_InvalidLength(t *testing.T) {
	// 含头模式下长度值小于包头：非法
	c := New(&models.TCPFrameConfig{
		Mode: models.TCPFrameLength, HeaderBytes: 4,
		ByteOrder: "big", LengthIncludesSelf: true,
	})
	bad := []byte{0x00, 0x00, 0x00, 0x02, 'x'}
	if _, err := c.NewDecoder().Feed(bad); err == nil {
		t.Fatal("长度值小于包头应报错")
	}

	// 声明体过长：超过上限
	c2 := New(&models.TCPFrameConfig{
		Mode: models.TCPFrameLength, HeaderBytes: 4,
		ByteOrder: "big", MaxFrameLength: 10,
	})
	bad2 := []byte{0x00, 0x00, 0x00, 0x0B} // 11 > 10
	if _, err := c2.NewDecoder().Feed(bad2); err == nil {
		t.Fatal("超过单帧上限应报错")
	}
}

func TestUnescapeDelimiter(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`\r\n`, "\r\n"},
		{`\n`, "\n"},
		{`\t`, "\t"},
		{`\0`, "\x00"},
		{`\\`, `\`},
		{`\x01ab`, "\x01ab"},
		{"|", "|"},   // 普通字符原样
		{`\d`, `\d`}, // 不识别的转义原样保留
	}
	for _, tc := range cases {
		if got := string(UnescapeDelimiter(tc.in)); got != tc.want {
			t.Fatalf("UnescapeDelimiter(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
