//go:build linux

// Package windowicon 提供 Linux 平台运行时窗口图标设置能力。
// Wails v3 在 Linux 上默认使用 GTK4，GTK4 已移除 gtk_window_set_icon，
// 窗口/任务栏图标只能由桌面环境按窗口的 application id 匹配 .desktop 文件得到。
// 因此这里做与 AppImage AppRun 相同的用户级桌面集成：写入
// ~/.local/share/icons 下的图标与 ~/.local/share/applications 下的桌面文件，
// 使直接运行二进制时（未安装 deb/rpm）也能显示应用图标。
package windowicon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// appID 与 main.go 中 LinuxOptions.ApplicationID 保持一致
const appID = "com.supersender.net"

// ApplyWindowIcon 安装/刷新用户级桌面图标与 .desktop 条目，返回是否写入成功。
// 在 AppImage 内运行时跳过：AppRun 已安装指向 AppImage 文件的桌面条目，
// 此处若用临时挂载路径覆盖 Exec 反而会破坏启动器图标与启动。
// 已通过 deb/rpm 安装（系统级 .desktop 已存在）时同样跳过，避免覆盖安装包的集成。
func ApplyWindowIcon(_ string, iconPNG []byte) bool {
	if os.Getenv("APPIMAGE") != "" || len(iconPNG) == 0 {
		return false
	}
	if installedDesktopExists() {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	appsDir := filepath.Join(home, ".local", "share", "applications")
	iconDir := filepath.Join(home, ".local", "share", "icons", "hicolor", "64x64", "apps")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		return false
	}
	if err := os.MkdirAll(iconDir, 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(filepath.Join(iconDir, appID+".png"), iconPNG, 0o644); err != nil {
		return false
	}

	desktop := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=SuperSender",
		`Exec="` + exe + `"`,
		"Icon=" + appID,
		"Terminal=false",
		"Categories=Network;",
		"StartupNotify=false",
		"StartupWMClass=" + appID,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(appsDir, appID+".desktop"), []byte(desktop), 0o644); err != nil {
		return false
	}
	// 刷新桌面数据库，让任务栏/启动器立即识别（不存在该工具时静默跳过）
	if p, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command(p, appsDir).Start()
	}
	return true
}

// installedDesktopExists 检查系统级（deb/rpm/AppImage 集成后）是否已有同名桌面条目
func installedDesktopExists() bool {
	dirs := os.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	for _, dir := range strings.Split(dirs, ":") {
		if dir == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "applications", appID+".desktop")); err == nil {
			return true
		}
	}
	return false
}
