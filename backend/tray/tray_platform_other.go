//go:build !windows

// Linux/macOS 托盘实现：Wails v3 原生 SystemTray。
// 不使用 fyne.io/systray：它的 macOS 实现（systray_darwin.m）会定义
// 与 Wails 同名的 Objective-C MenuItem 类，导致 darwin 链接期
// duplicate symbol 冲突，且无法跨平台交叉编译。
package tray

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"SuperSender/backend/applog"
)

// platformTray 持有原生托盘实例，Stop 时销毁
var platformTray *application.SystemTray

// startTray 创建原生系统托盘并注册菜单/点击回调
func startTray(c *Controller) {
	if c.app == nil {
		return
	}

	tray := c.app.SystemTray.New()
	// label 在 Linux 下作为 StatusNotifierItem 的 Title/Id
	tray.SetLabel(c.appName)
	tray.SetIcon(c.iconPNG)
	tray.SetTooltip(c.tooltip)

	menu := c.app.Menu.New()
	menu.Add("显示主窗口").OnClick(func(*application.Context) {
		c.showWindow()
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) {
		applog.Printf("[tray] 用户点击退出")
		c.QuitApp()
	})
	tray.SetMenu(menu)

	tray.OnClick(func() {
		c.showWindow()
	})

	platformTray = tray
}

// stopTray 销毁原生托盘图标，可安全重复调用
func stopTray() {
	if platformTray != nil {
		platformTray.Destroy()
		platformTray = nil
	}
}
