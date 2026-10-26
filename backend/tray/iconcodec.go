// Package tray 提供系统托盘图标与平台相关设置的跨平台封装。
// iconcodec.go 负责将 PNG 字节包装为 ICO（Windows 托盘与窗口图标均要求 ICO 格式）。
package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"

	"golang.org/x/image/draw"
)

// PngToICO 将 PNG 字节包装为单条目 ICO（PNG 压缩格式，Windows Vista+ 支持）。
// 不做尺寸缩放，直接使用 PNG 原始尺寸；尺寸 >=256 时用 0 表示（ICO 规范）。
// 调用方负责传入可信的 PNG 字节（通常来自 //go:embed 内嵌资源）。
func PngToICO(pngBytes []byte) ([]byte, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	w, h := cfg.Width, cfg.Height
	wh := byte(0)
	if w < 256 {
		wh = byte(w)
	}
	hh := byte(0)
	if h < 256 {
		hh = byte(h)
	}

	var out bytes.Buffer
	// ICONDIR：reserved(0) + type(1=ICO) + count(1)
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	// ICONDIRENTRY（16 字节）
	out.WriteByte(wh)                                       // 宽
	out.WriteByte(hh)                                       // 高
	out.WriteByte(0)                                        // 调色板颜色数（0）
	out.WriteByte(0)                                        // 保留
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))  // 色彩平面数
	_ = binary.Write(&out, binary.LittleEndian, uint16(32)) // 位深度
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(pngBytes)))
	_ = binary.Write(&out, binary.LittleEndian, uint32(22)) // 数据偏移：6 + 16
	// 图像数据
	out.Write(pngBytes)
	return out.Bytes(), nil
}

// MustICO 生成 ICO 数据，失败时 panic（适用于内嵌可信资源，仅启动期调用）。
func MustICO(pngBytes []byte) []byte {
	b, err := PngToICO(pngBytes)
	if err != nil {
		panic("生成托盘图标失败: " + err.Error())
	}
	return b
}

// PngToICOScaled 将 PNG 解码后缩放到指定尺寸列表，编码为多尺寸 ICO。
// 每个条目采用 PNG 压缩数据（Windows Vista 及以上支持），使用 Catmull-Rom 插值保证缩放质量。
// 用于窗口图标（标题栏小图标 + 任务栏/Alt+Tab 大图标），多尺寸可避免 Windows 拉伸模糊。
func PngToICOScaled(pngBytes []byte, sizes []int) ([]byte, error) {
	src, _, err := image.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}

	images := make([][]byte, 0, len(sizes))
	for _, s := range sizes {
		dst := image.NewRGBA(image.Rect(0, 0, s, s))
		draw.CatmullRom.Scale(dst, dst.Rect, src, src.Bounds(), draw.Over, nil)
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			return nil, err
		}
		images = append(images, buf.Bytes())
	}

	var out bytes.Buffer
	// ICONDIR：reserved(0) + type(1=ICO) + count
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(images)))

	// 目录表后紧跟各条目图像数据
	offset := 6 + 16*len(images)
	for i, data := range images {
		s := sizes[i]
		wh := byte(s)
		if s >= 256 {
			wh = 0 // 0 表示 256 像素
		}
		out.WriteByte(wh)                                       // 宽
		out.WriteByte(wh)                                       // 高
		out.WriteByte(0)                                        // 调色板颜色数（0）
		out.WriteByte(0)                                        // 保留
		_ = binary.Write(&out, binary.LittleEndian, uint16(1))  // 色彩平面数
		_ = binary.Write(&out, binary.LittleEndian, uint16(32)) // 位深度
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(data)))
		_ = binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(data)
	}
	for _, data := range images {
		out.Write(data)
	}
	return out.Bytes(), nil
}

// MustICOScaled 生成多尺寸 ICO，失败时 panic（图标为内嵌可信资源）。
func MustICOScaled(pngBytes []byte, sizes []int) []byte {
	b, err := PngToICOScaled(pngBytes, sizes)
	if err != nil {
		panic("生成窗口图标失败: " + err.Error())
	}
	return b
}
