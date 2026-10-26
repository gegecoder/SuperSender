//go:build windows

// Windows 托盘实现：fyne.io/systray（原生 Win32 消息循环 + ICO 图标）。
package tray

import (
	stdruntime "runtime"
	"sync"

	"fyne.io/systray"

	"SuperSender/backend/applog"
)

// trayStopOnce 保证 systray.Quit 只执行一次
var trayStopOnce sync.Once

// startTray 启动系统托盘（桌面角标 + 右键菜单）。
// 必须运行在独立的、固定 OS 线程的 goroutine 中，
// 因为 Windows 端需要在同一线程创建隐藏窗口并泵送消息循环。
func startTray(c *Controller) {
	stdruntime.LockOSThread()
	systray.Run(c.onTrayReady, nil)
}

// stopTray 移除系统托盘图标，可安全重复调用
func stopTray() {
	trayStopOnce.Do(func() {
		systray.Quit()
	})
}

// onTrayReady 托盘初始化完成后构建菜单并绑定事件
func (c *Controller) onTrayReady() {
	// 图标字节由 tray 包负责 PNG → ICO 转换
	SetIcon(MustICO(c.iconPNG))
	SetTooltip(c.tooltip)

	mShow := systray.AddMenuItem("显示主窗口", "显示 SuperSender 主窗口")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("退出", "退出 SuperSender")

	// 左键单击图标唤出主窗口。
	// 注意：此回调运行在 systray 的 LockOSThread 消息循环线程上，
	// 直接调用 Wails runtime 可能因线程锁定产生不可预期行为，
	// 因此统一转交给独立 goroutine 执行。
	systray.SetOnTapped(func() {
		c.showWindow()
	})

	go func() {
		for {
			select {
			case <-mShow.ClickedCh:
				c.showWindow()
			case <-mQuit.ClickedCh:
				applog.Printf("[tray] 用户点击退出")
				c.QuitApp()
				return
			}
		}
	}()
}
