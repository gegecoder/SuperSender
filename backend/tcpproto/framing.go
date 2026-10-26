// Package tcpproto 实现 TCP 应用层分帧协议（协议化改造）。
//
// TCP 是面向字节流的协议，一次 Read 返回的字节块与对端一次 Write 并不对应
// （粘包/拆包）。本包在不改变 UDP/Syslog 行为的前提下，为 TCP 客户端与服务端
// 提供四种可配置的分帧方式：
//
//   - raw       原始裸流（默认）：读到什么就上报什么，与旧版本行为完全一致
//   - delimiter 分隔符分帧：以 \r\n、\n 或自定义分隔符切分报文
//   - fixed     固定长度分帧：每帧固定 N 字节
//   - length    长度前缀分帧：1/2/4 字节长度头 + 报文体，支持大端/小端
//
// 发送侧通过 Codec.Encode 封帧（无状态）；接收侧通过 StreamDecoder.Feed
// 喂入任意大小的 Read 块，按配置拆出完整报文（有状态，每连接一个实例）。
package tcpproto

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"SuperSender/backend/models"
)

// defaultMaxFrameLength 长度前缀模式下接收侧单帧默认上限（16MB），防止异常长度头耗尽内存
const defaultMaxFrameLength = 16 * 1024 * 1024

// Codec 单会话 TCP 分帧编解码器。
// 发送侧 Encode 无状态可并发；接收侧请使用 NewDecoder 创建有状态解码器。
type Codec struct {
	mode               models.TCPFrameMode
	delim              []byte
	frameLength        int
	headerBytes        int
	bigEndian          bool
	lengthIncludesSelf bool
	maxFrameLength     int
}

// New 根据会话 TCP 分帧配置创建编解码器。
// cfg 为 nil、Mode 为空或未知值时一律按 raw 裸流处理，保证旧配置完全兼容。
func New(cfg *models.TCPFrameConfig) *Codec {
	c := &Codec{
		mode:           models.TCPFrameRaw,
		bigEndian:      true,
		headerBytes:    4,
		maxFrameLength: defaultMaxFrameLength,
	}
	if cfg == nil {
		return c
	}
	switch cfg.Mode {
	case models.TCPFrameDelimiter, models.TCPFrameFixed, models.TCPFrameLength:
		c.mode = cfg.Mode
	}
	if cfg.MaxFrameLength > 0 {
		c.maxFrameLength = cfg.MaxFrameLength
	}

	switch c.mode {
	case models.TCPFrameDelimiter:
		c.delim = UnescapeDelimiter(cfg.Delimiter)
	case models.TCPFrameFixed:
		if cfg.FrameLength > 0 {
			c.frameLength = cfg.FrameLength
		} else {
			// 非法固定长度退化为裸流，避免发送侧死循环/接收侧永不吐帧
			c.mode = models.TCPFrameRaw
		}
	case models.TCPFrameLength:
		switch cfg.HeaderBytes {
		case 1, 2, 4:
			c.headerBytes = cfg.HeaderBytes
		default:
			c.headerBytes = 4
		}
		c.bigEndian = cfg.ByteOrder != "little"
		c.lengthIncludesSelf = cfg.LengthIncludesSelf
	}
	return c
}

// Mode 返回当前分帧模式
func (c *Codec) Mode() models.TCPFrameMode { return c.mode }

// Encode 发送封帧：把一条报文体编码为实际写入 TCP 流的字节。
// raw 模式原样返回；调用方无需关心当前模式。
func (c *Codec) Encode(payload []byte) []byte {
	switch c.mode {
	case models.TCPFrameDelimiter:
		if len(c.delim) == 0 {
			return payload
		}
		if len(payload) > 0 && hasSuffix(payload, c.delim) {
			return payload // 已以分隔符结尾时不重复追加
		}
		out := make([]byte, 0, len(payload)+len(c.delim))
		out = append(out, payload...)
		out = append(out, c.delim...)
		return out

	case models.TCPFrameFixed:
		n := c.frameLength
		rem := len(payload) % n
		if rem == 0 {
			return payload
		}
		out := make([]byte, 0, len(payload)+(n-rem))
		out = append(out, payload...)
		out = append(out, make([]byte, n-rem)...)
		return out

	case models.TCPFrameLength:
		bodyLen := len(payload)
		length := bodyLen
		if c.lengthIncludesSelf {
			length += c.headerBytes
		}
		out := make([]byte, c.headerBytes+bodyLen)
		c.putHeaderLength(out[:c.headerBytes], uint64(length))
		copy(out[c.headerBytes:], payload)
		return out

	default:
		return payload
	}
}

// NewDecoder 创建与本编解码器配套的有状态流式解码器（每个 TCP 连接一个）。
func (c *Codec) NewDecoder() *StreamDecoder {
	return &StreamDecoder{c: c}
}

// StreamDecoder TCP 流式分帧解码器，非线程安全（每连接单协程读取）。
type StreamDecoder struct {
	c   *Codec
	buf []byte
}

// Feed 喂入一次 Read 得到的字节块，返回本次可拆出的全部完整报文。
// raw 模式下每个非空 chunk 直接作为一条报文返回（保持旧行为）；
// 其它模式下会缓冲半包，等待后续字节；帧格式非法时返回错误。
func (d *StreamDecoder) Feed(chunk []byte) ([][]byte, error) {
	if len(chunk) == 0 {
		return nil, nil
	}
	switch d.c.mode {
	case models.TCPFrameDelimiter:
		return d.feedDelimiter(chunk), nil
	case models.TCPFrameFixed:
		return d.feedFixed(chunk), nil
	case models.TCPFrameLength:
		return d.feedLength(chunk)
	default:
		// 裸流：每次 Read 即一条报文
		return [][]byte{cloneBytes(chunk)}, nil
	}
}

// feedDelimiter 分隔符拆帧：分隔符本身不计入报文内容
func (d *StreamDecoder) feedDelimiter(chunk []byte) [][]byte {
	if len(d.c.delim) == 0 {
		return [][]byte{cloneBytes(chunk)}
	}
	d.buf = append(d.buf, chunk...)
	var frames [][]byte
	for {
		idx := indexBytes(d.buf, d.c.delim)
		if idx < 0 {
			break
		}
		frames = append(frames, cloneBytes(d.buf[:idx]))
		d.buf = d.buf[idx+len(d.c.delim):]
	}
	return frames
}

// feedFixed 固定长度切帧
func (d *StreamDecoder) feedFixed(chunk []byte) [][]byte {
	d.buf = append(d.buf, chunk...)
	var frames [][]byte
	for len(d.buf) >= d.c.frameLength {
		frames = append(frames, cloneBytes(d.buf[:d.c.frameLength]))
		d.buf = d.buf[d.c.frameLength:]
	}
	return frames
}

// feedLength 长度前缀拆帧
func (d *StreamDecoder) feedLength(chunk []byte) ([][]byte, error) {
	d.buf = append(d.buf, chunk...)
	var frames [][]byte
	head := d.c.headerBytes
	for len(d.buf) >= head {
		length := int(d.c.headerLength(d.buf[:head]))
		bodyLen := length
		if d.c.lengthIncludesSelf {
			if length < head {
				return frames, fmt.Errorf("长度前缀非法：长度值 %d 小于包头 %d 字节", length, head)
			}
			bodyLen = length - head
		}
		if bodyLen < 0 || bodyLen > d.c.maxFrameLength {
			return frames, fmt.Errorf("长度前缀非法：单帧 %d 字节超过上限 %d", bodyLen, d.c.maxFrameLength)
		}
		if len(d.buf) < head+bodyLen {
			break // 报文体未到齐，继续等待
		}
		frames = append(frames, cloneBytes(d.buf[head:head+bodyLen]))
		d.buf = d.buf[head+bodyLen:]
	}
	return frames, nil
}

// headerLength 按配置的字节数与字节序解析长度头
func (c *Codec) headerLength(b []byte) uint64 {
	switch c.headerBytes {
	case 1:
		return uint64(b[0])
	case 2:
		if c.bigEndian {
			return uint64(binary.BigEndian.Uint16(b))
		}
		return uint64(binary.LittleEndian.Uint16(b))
	case 4:
		if c.bigEndian {
			return uint64(binary.BigEndian.Uint32(b))
		}
		return uint64(binary.LittleEndian.Uint32(b))
	default:
		return 0
	}
}

// putHeaderLength 按配置的字节数与字节序写入长度头
func (c *Codec) putHeaderLength(b []byte, v uint64) {
	switch c.headerBytes {
	case 1:
		b[0] = byte(v)
	case 2:
		if c.bigEndian {
			binary.BigEndian.PutUint16(b, uint16(v))
		} else {
			binary.LittleEndian.PutUint16(b, uint16(v))
		}
	case 4:
		if c.bigEndian {
			binary.BigEndian.PutUint32(b, uint32(v))
		} else {
			binary.LittleEndian.PutUint32(b, uint32(v))
		}
	}
}

// UnescapeDelimiter 解析分隔符输入串中的常用转义序列：
// \r \n \t \0 \\ 以及 \xHH（两位十六进制）；无法识别的转义按原文字符保留。
func UnescapeDelimiter(s string) []byte {
	if s == "" {
		return nil
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch != '\\' || i+1 >= len(s) {
			out = append(out, ch)
			continue
		}
		next := s[i+1]
		switch next {
		case 'r':
			out = append(out, '\r')
			i++
		case 'n':
			out = append(out, '\n')
			i++
		case 't':
			out = append(out, '\t')
			i++
		case '0':
			out = append(out, 0)
			i++
		case '\\':
			out = append(out, '\\')
			i++
		case 'x', 'X':
			if i+3 < len(s) && isHex(s[i+2]) && isHex(s[i+3]) {
				hi := hexVal(s[i+2])
				lo := hexVal(s[i+3])
				out = append(out, byte(hi<<4|lo))
				i += 3
			} else {
				out = append(out, ch)
			}
		default:
			out = append(out, ch)
		}
	}
	return out
}

func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

func hasSuffix(s, suffix []byte) bool {
	return len(s) >= len(suffix) && string(s[len(s)-len(suffix):]) == string(suffix)
}

func indexBytes(s, sep []byte) int {
	return bytes.Index(s, sep)
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexVal(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	default:
		return int(b-'A') + 10
	}
}
