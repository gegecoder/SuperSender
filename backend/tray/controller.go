// controller.go 托盘高层控制器：托盘启动、菜单构建、窗口显隐、
// 退出确认与兜底强杀流程。底层实现按平台拆分：
// Windows 使用 fyne.io/systray（见 tray_platform_windows.go），
// Linux/macOS 使用 Wails v3 原生 SystemTray（见 tray_platform_other.go）。
package tray

import (
	"os"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"SuperSender/backend/applog"
	"SuperSender/backend/closeconfirm"
)

// Controller 托盘控制器：持有应用图标、Wails v3 实例与退出标记，
// 由 App Service 注入。
type Controller struct {
	iconPNG []byte
	appName string
	tooltip string
	app     *application.App
	// quitting 指向应用的退出确认标记，避免 OnBeforeClose 二次询问
	quitting *atomic.Bool
}

// NewController 创建托盘控制器。iconPNG 为内嵌 PNG 图标字节，
// quitting 为应用级退出标记（调用方持有）。
func NewController(iconPNG []byte, appName, tooltip string, quitting *atomic.Bool) *Controller {
	return &Controller{
		iconPNG:  iconPNG,
		appName:  appName,
		tooltip:  tooltip,
		quitting: quitting,
	}
}

// Bind 注入 Wails v3 应用实例（ServiceStartup 时调用）
func (c *Controller) Bind(app *application.App) {
	c.app = app
}

// Start 启动系统托盘。各平台具体实现见 tray_platform_*.go。
func (c *Controller) Start() {
	go startTray(c)
}

// Stop 移除托盘图标，可安全重复调用
func (c *Controller) Stop() {
	stopTray()
}

// showWindow 在独立 goroutine 中调用 Wails v3 API 显示并激活主窗口。
// 从非 Wails 主线程调用时，v3 runtime 会通过 Invoke 跨线程派发。
func (c *Controller) showWindow() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				applog.Printf("[tray] showWindow panic: %v", r)
			}
		}()
		if c.app == nil {
			return
		}
		if w, ok := c.app.Window.Get("main"); ok {
			w.Show()
			w.UnMinimise()
		}
	}()
}

// QuitApp 退出应用：标记 quitting 后请求 Wails v3 退出，
// 同时启动兜底 goroutine 在超时后强制退出进程，
// 防止 Wails 退出流程卡住。
func (c *Controller) QuitApp() {
	c.quitting.Store(true)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				applog.Printf("[tray] quitApp panic: %v", r)
			}
		}()
		if c.app != nil {
			c.app.Quit()
		}
	}()
	// 兜底：2 秒后若进程仍未退出，强制终止
	go func() {
		t := time.NewTimer(2 * time.Second)
		defer t.Stop()
		<-t.C
		applog.Printf("[tray] 正常退出超时，强制结束进程")
		os.Exit(0)
	}()
}

// MinimizeToTray 隐藏主窗口（v3 无 OnBeforeClose 拦截，改为由前端在关闭前调用）
func (c *Controller) MinimizeToTray() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				applog.Printf("[tray] minimize panic: %v", r)
			}
		}()
		if c.app == nil {
			return
		}
		if w, ok := c.app.Window.Get("main"); ok {
			w.Hide()
		}
	}()
}

// OnBeforeClose 关闭拦截（v3 beta 无原生拦截 API，暂由前端 JS 侧拦截窗口关闭事件后调用本方法）
// 返回 true 表示阻止关闭（隐藏到托盘），false 表示允许退出
func (c *Controller) OnBeforeClose() bool {
	if c.quitting.Load() {
		return false
	}
	_ = closeconfirm.ActionMinimize // 保留接口兼容
	c.MinimizeToTray()
	return true
}
