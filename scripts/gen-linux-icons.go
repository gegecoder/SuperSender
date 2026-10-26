//go:build ignore

// 生成 Linux hicolor 图标集：把 build/icon.png 缩放为
// build/linux/icons/hicolor/<size>x<size>/apps/SuperSender.png。
// 由 Taskfile 的 generate:linux:icons 任务调用：
//
//	go run scripts/gen-linux-icons.go
package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

func main() {
	sizes := []int{16, 22, 24, 32, 48, 64, 128, 256, 512}

	srcFile, err := os.Open("build/icon.png")
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开 build/icon.png 失败: %v\n", err)
		os.Exit(1)
	}
	defer srcFile.Close()

	src, err := png.Decode(srcFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "解码 build/icon.png 失败: %v\n", err)
		os.Exit(1)
	}

	for _, size := range sizes {
		dst := image.NewRGBA(image.Rect(0, 0, size, size))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)

		outDir := filepath.Join("build", "linux", "icons", "hicolor",
			fmt.Sprintf("%dx%d", size, size), "apps")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "创建目录失败: %v\n", err)
			os.Exit(1)
		}
		outPath := filepath.Join(outDir, "SuperSender.png")
		out, err := os.Create(outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "创建 %s 失败: %v\n", outPath, err)
			os.Exit(1)
		}
		if err := png.Encode(out, dst); err != nil {
			out.Close()
			fmt.Fprintf(os.Stderr, "写入 %s 失败: %v\n", outPath, err)
			os.Exit(1)
		}
		out.Close()
		fmt.Printf("generated %s\n", outPath)
	}
}
