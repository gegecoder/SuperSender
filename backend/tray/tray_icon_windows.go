//go:build windows

package tray

import "fyne.io/systray"

// SetIcon 设置系统托盘图标（Windows 要求 ICO 格式字节）。
func SetIcon(icon []byte) { systray.SetIcon(icon) }

// SetTooltip 设置系统托盘鼠标悬停提示文本。
func SetTooltip(tip string) { systray.SetTooltip(tip) }
