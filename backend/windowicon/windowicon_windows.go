//go:build windows

// Package windowicon 提供 Windows 平台运行时设置窗口图标的能力。
// 通过 Win32 API (WM_SETICON) 将内嵌 PNG 生成的多尺寸 ICO 设置到主窗口，
// 不依赖编译期 syso 资源，wails dev 下同样立即生效。
package windowicon

import (
	"os"
	"syscall"
	"unsafe"

	traypkg "SuperSender/backend/tray"
)

// Win32 图标相关常量
// 参考 https://learn.microsoft.com/windows/win32/menurc/loadimagew
const (
	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	wmSetIcon      = 0x0080
	iconSmall      = 0
	iconBig        = 1
	smCXIcon       = 11
	smCYIcon       = 12
	smCXSmIcon     = 49
	smCYSmIcon     = 50
)

// user32 动态链接库及所需过程
var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procFindWindowW      = user32.NewProc("FindWindowW")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
)

// sysMetric 读取一个系统尺寸指标
func sysMetric(index int) int {
	v, _, _ := procGetSystemMetrics.Call(uintptr(index))
	return int(v)
}

// loadSizedIcon 从 ico 文件加载指定像素的 HICON（多尺寸 ico 会自动取最接近的帧）
func loadSizedIcon(path string, cx, cy int) syscall.Handle {
	pathPtr, _ := syscall.UTF16PtrFromString(path)
	h, _, _ := procLoadImageW.Call(
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		imageIcon,
		uintptr(cx),
		uintptr(cy),
		lrLoadFromFile,
	)
	return syscall.Handle(h)
}

// ApplyWindowIcon 将内嵌 PNG 生成的多尺寸 ICO 应用到已创建的主窗口：
// 同时设置大图标（Alt+Tab/任务栏）与小图标（标题栏左上角）。
// 窗口标题必须与 main.go 中 options.Title 一致，FindWindowW 才能定位到窗口。
// iconPNG 为内嵌的应用图标 PNG 字节。
func ApplyWindowIcon(title string, iconPNG []byte) bool {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return false
	}

	// 由内嵌 iconPNG 生成多尺寸 ICO，覆盖标题栏（16/20/24）与任务栏（32/40/48/64）
	icoBytes := traypkg.MustICOScaled(iconPNG, []int{16, 20, 24, 32, 40, 48, 64})

	f, err := os.CreateTemp("", "supersender-icon-*.ico")
	if err != nil {
		return false
	}
	tmpPath := f.Name()
	_, _ = f.Write(icoBytes)
	_ = f.Close()
	defer os.Remove(tmpPath) // HICON 加载后与文件无关，可立即删除临时文件

	smCX := sysMetric(smCXSmIcon)
	smCY := sysMetric(smCYSmIcon)
	bigCX := sysMetric(smCXIcon)
	bigCY := sysMetric(smCYIcon)

	applied := false
	if hSmall := loadSizedIcon(tmpPath, smCX, smCY); hSmall != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, uintptr(hSmall))
		applied = true
	}
	if hBig := loadSizedIcon(tmpPath, bigCX, bigCY); hBig != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, uintptr(hBig))
		applied = true
	}
	// HICON 由窗口持有，进程结束时随窗口销毁，这里不调用 DestroyIcon
	return applied
}
