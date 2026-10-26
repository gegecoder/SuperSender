// SuperSender 主入口：Wails v3 原生窗口应用 + 内嵌 webview + 系统托盘角标。
// 单 exe 绿色免安装，无运行时依赖；前端资源通过 go:embed 内嵌到二进制。
package main

import (
	"embed"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"

	"SuperSender/backend/appconfig"
	"SuperSender/backend/applog"
)

//go:embed all:frontend/dist
var assets embed.FS

// app.config.json 是应用名称/版本/更新检测等信息的唯一配置源，编译期内嵌
//
//go:embed app.config.json
var appConfigRaw []byte

func main() {
	// 拦截标准 log 输出到内存环形缓冲，供前端日志浮层查询
	applog.Init()

	// 统一信息配置（Go 与前端共用根目录下的同一份 app.config.json）
	cfg, err := appconfig.LoadAppConfig(appConfigRaw)
	if err != nil {
		applog.Errorf("加载应用配置失败: %v", err)
		os.Exit(1)
	}
	applog.Printf("[app] %s v%s 启动中…", cfg.Name, cfg.Version)

	// 初始化核心 Service（存储 / 模板 / 会话管理器等）
	svc, err := NewApp(cfg)
	if err != nil {
		applog.Errorf("初始化失败: %v", err)
		os.Exit(1)
	}

	// 创建 Wails v3 应用，注册我们的 App Service
	app := application.New(application.Options{
		Name:        "SuperSender",
		Description: cfg.Description,
		// Linux 反域名应用 ID：GtkApplication id / Wayland app_id / X11 WM_CLASS
		// 统一为 com.supersender.net，与 .desktop 文件名（productIdentifier）
		// 精确匹配，任务栏/Dock 才能正确显示应用图标。
		Linux: application.LinuxOptions{
			ApplicationID: "com.supersender.net",
		},
		// macOS：不随最后一个窗口自动退出。窗口关闭由 InstallCloseHandler 拦截，
		// 走自建安全退出流程，避免 Wails 默认关闭链路在主线程嵌套重入导致卡死。
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		// macOS 的 Cmd+Q / Dock 退出同样走自建安全退出流程
		ShouldQuit: func() bool {
			if runtime.GOOS == "darwin" {
				svc.RequestQuit()
				return false
			}
			return true
		},
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		OnShutdown: func() {
			// 应用退出时的统一清理
			svc.OnShutdown()
		},
	})

	// 创建主窗口：默认尺寸取主屏工作区的 80%（DIP），最小尺寸保底
	width, height := 1024, 640
	if screen := app.Screen.GetPrimary(); screen != nil && screen.Size.Width > 0 && screen.Size.Height > 0 {
		width = screen.Size.Width * 8 / 10
		height = screen.Size.Height * 8 / 10
	}
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "SuperSender",
		Width:            width,
		Height:           height,
		MinWidth:         1024,
		MinHeight:        640,
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})

	// 关闭拦截：macOS / Linux 走自建安全退出（修复点 X 后转圈或无法关闭）；
	// Windows 弹「最小化到托盘 / 直接退出 / 取消」询问框
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		svc.InstallCloseHandler(mainWindow)
	}

	// 启动应用（阻塞直到退出）
	err = app.Run()
	if err != nil {
		applog.Errorf("SuperSender 启动失败: %v", err)
		applog.Close()
		os.Exit(1)
	}
	applog.Close()
}
