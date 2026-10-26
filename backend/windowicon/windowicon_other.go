//go:build !windows && !linux

// Package windowicon 提供跨平台窗口图标设置入口；非 Windows / Linux 平台为 no-op。
package windowicon

// ApplyWindowIcon 非 Windows / Linux 平台无需运行时设置窗口图标。
func ApplyWindowIcon(_ string, _ []byte) bool {
	return false
}
