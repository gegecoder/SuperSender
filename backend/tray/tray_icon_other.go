//go:build !windows

package tray

// 非 Windows 平台空实现：托盘图标/提示在 Linux/macOS 下由 fyne.io/systray 自行处理，
// 本应用仅在 Windows 提供桌面角标，其它平台为 no-op。
func SetIcon(_ []byte) {}
func SetTooltip(_ string) {}
