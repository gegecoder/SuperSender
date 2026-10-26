// Package main 提供 Wails v3 App Service：所有前端可调用方法均在此声明，
// 内部转发到 backend/* 各模块。事件推送通过 a.app.Event.Emit 完成。
package main

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"SuperSender/backend/appconfig"
	"SuperSender/backend/applog"
	"SuperSender/backend/closeconfirm"
	"SuperSender/backend/models"
	"SuperSender/backend/runcmd"
	"SuperSender/backend/session"
	"SuperSender/backend/sockopt"
	"SuperSender/backend/storage"
	"SuperSender/backend/syslog"
	"SuperSender/backend/template"
	"SuperSender/backend/tray"
	"SuperSender/backend/update"
	"SuperSender/backend/windowicon"
)

// 应用图标源（PNG 字节）。tray 包接收此字节并按需转为 ICO。
//
//go:embed assets/icon.png
var appIconPNG []byte

// App Wails v3 Service：聚合存储 / 模板 / 会话三大后端模块
type App struct {
	// app 持有 Wails Application 实例，用于发事件、弹文件对话框等
	// （注意不能直接存 context——v3 的 Service 生命周期内 context 会被 cancel）
	app *application.App

	storage   *storage.Storage
	templates *template.Manager
	sessions  *session.Manager
	tray      *tray.Controller
	// emitFunc 会话事件推送闭包（ServiceStartup 时创建，重置数据时重建会话管理器复用）
	emitFunc session.EmitFunc
	// appCfg 统一信息配置（来自根目录 app.config.json，编译期内嵌）
	appCfg appconfig.AppConfig
	// quitting 标记用户已确认退出，用于跳过二次询问（v3 暂时无 close 拦截 API，留作备用）
	quitting atomic.Bool
	// shutdownOnce 保证 ServiceShutdown 幂等（Wails 退出链路与自建退出链路可能都会调用）
	shutdownOnce sync.Once
	// quitOnce 保证自建安全退出流程只执行一次（macOS 关闭窗口 / Cmd+Q / 托盘退出共用）
	quitOnce sync.Once
	// confirming 防止 Windows 关闭询问框重复弹出（连续点击 X 时只保留一个）
	confirming atomic.Bool
}

// NewApp 创建 App Service 实例（此时还未绑定到 Wails Application）
func NewApp(cfg appconfig.AppConfig) (*App, error) {
	store, err := storage.New("SuperSender")
	if err != nil {
		return nil, fmt.Errorf("初始化存储失败: %w", err)
	}
	// 按全局设置切换日志目录（空 = 默认目录）；失败时保留默认目录继续运行
	settings := store.LoadSettingsOrDefault()
	if _, err := applog.SetDir(settings.LogDir); err != nil {
		applog.Warnf("[applog] 切换日志目录失败，继续使用 %s: %v", applog.Dir(), err)
	}
	applog.Printf("[applog] 日志目录 → %s", applog.Dir())

	templates := template.New(store)
	return &App{
		storage:   store,
		templates: templates,
		appCfg:    cfg,
	}, nil
}

// ServiceStartup Wails v3 Service 启动钩子：绑定 Application 实例、初始化会话管理器和托盘
func (a *App) ServiceStartup(ctx context.Context, opts application.ServiceOptions) error {
	// 获取全局 Application 实例
	a.app = application.Get()

	// 事件推送闭包：通过 Wails v3 的 EventManager 推送事件
	// 事件名约定：stats:<id> recv:<id> error:<id> status:<id>
	a.emitFunc = func(event string, data ...any) {
		a.app.Event.Emit(event, data...)
	}
	a.sessions = session.New(a.storage, a.templates, a.emitFunc)
	applog.Printf("[app] ServiceStartup 完成：会话管理器已就绪")

	// 启动系统托盘（独立 goroutine，固定 OS 线程）
	a.tray = tray.NewController(appIconPNG, "SuperSender", "SuperSender", &a.quitting)
	a.tray.Bind(a.app)
	a.tray.Start()

	// 设置窗口标题栏左上角 + 任务栏图标（内嵌 icon.png，dev/打包均生效）。
	const windowTitle = "SuperSender"
	windowicon.ApplyWindowIcon(windowTitle, appIconPNG)
	go func() {
		t := time.NewTimer(500 * time.Millisecond)
		defer t.Stop()
		<-t.C
		windowicon.ApplyWindowIcon(windowTitle, appIconPNG)
	}()

	return nil
}

// ServiceShutdown Wails v3 Service 关闭钩子：停止全部会话（幂等）
func (a *App) ServiceShutdown() error {
	a.shutdownOnce.Do(func() {
		applog.Printf("[app] ServiceShutdown：停止会话/托盘…")
		if a.sessions != nil {
			a.sessions.StopAll()
			a.sessions.PersistAll()
		}
		if a.tray != nil {
			a.tray.Stop()
		}
		applog.Printf("[app] 清理完成")
	})
	return nil
}

// InstallCloseHandler 安装主窗口关闭拦截（按平台分支）：
//   - macOS：取消默认关闭链路，隐藏窗口并走自建安全退出。
//     Wails 默认链路（windowShouldClose → 默认 listener → [window close] →
//     applicationShouldTerminate → cleanup）容易嵌套重入导致主线程卡死。
//   - Linux：同样取消默认关闭并走自建退出，避免点 X 后窗口无响应、
//     被桌面环境判定为「强制退出」。
//   - Windows：取消默认关闭，弹「最小化到托盘 / 直接退出 / 取消」询问框。
func (a *App) InstallCloseHandler(w *application.WebviewWindow) {
	if w == nil {
		return
	}
	switch runtime.GOOS {
	case "darwin", "linux":
		w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			if a.quitting.Load() {
				return
			}
			e.Cancel()
			// 先隐藏窗口，避免用户看到关闭动画卡住
			go func() {
				defer func() { _ = recover() }()
				w.Hide()
			}()
			a.RequestQuit()
		})
	case "windows":
		w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			if a.quitting.Load() {
				return
			}
			// 取消默认关闭，由用户选择后续动作
			e.Cancel()
			if !a.confirming.CompareAndSwap(false, true) {
				return
			}
			go func() {
				defer func() {
					a.confirming.Store(false)
					_ = recover()
				}()
				switch closeconfirm.AskCloseAction(context.Background(), "SuperSender") {
				case closeconfirm.ActionMinimize:
					applog.Printf("[app] 用户选择最小化到托盘")
					w.Hide()
				case closeconfirm.ActionQuit:
					applog.Printf("[app] 用户选择直接退出")
					a.RequestQuit()
				default: // 取消：保持窗口
					applog.Printf("[app] 用户取消关闭")
				}
			}()
		})
	}
}

// RequestQuit 自建安全退出流程：停会话 → 关日志 → 结束进程；
// 3 秒未退出则强制结束，保证用户点击关闭后一定能退出。
func (a *App) RequestQuit() {
	a.quitOnce.Do(func() {
		a.quitting.Store(true)
		go func() {
			applog.Printf("[app] 触发安全退出…")
			time.AfterFunc(3*time.Second, func() {
				applog.Printf("[app] 正常退出超时，强制结束进程")
				applog.Close()
				os.Exit(0)
			})
			_ = a.ServiceShutdown()
			applog.Printf("[app] 安全退出完成")
			applog.Close()
			os.Exit(0)
		}()
	})
}

// OnShutdown 由 main.go 的 app.OnShutdown 调用，做最终清理（ServiceShutdown 已经做了大部分）
func (a *App) OnShutdown() {
	// 留空——ServiceShutdown 已覆盖，这里作为额外兜底
}

// ================================================================
//   以下方法签名保持不变，前端 utils/wails.ts 全部照旧调用
// ================================================================

// GetRecentLogs 返回程序运行日志（内存环形缓冲，最多 2000 条）
func (a *App) GetRecentLogs() []applog.Entry {
	return applog.Recent()
}

// ClearLogs 清空运行日志
func (a *App) ClearLogs() {
	applog.Clear()
}

// AddFrontendLog 前端写入运行日志（前后端日志统一在日志浮层排查链路）
func (a *App) AddFrontendLog(msg string) {
	applog.Printf("[frontend] %s", msg)
}

// Ping 事件通道自检
func (a *App) Ping(id string) {
	applog.Printf("[app] 收到前端 Ping(%s) → 回复 Pong", id)
	a.app.Event.Emit("pong:"+id, id)
}

// ResetAllData 重置全部持久化数据
func (a *App) ResetAllData() models.APIResult {
	applog.Printf("[app] 开始重置全部数据（恢复出厂状态）…")
	a.sessions.StopAll()
	if err := a.storage.ResetAll(); err != nil {
		applog.Errorf("[app] 重置数据失败: %v", err)
		return models.APIResult{Success: false, Message: err.Error()}
	}
	a.templates = template.New(a.storage)
	a.sessions = session.New(a.storage, a.templates, a.emitFunc)
	applog.Printf("[app] 数据已重置为默认状态")
	return models.APIResult{Success: true, Message: "已重置为默认状态"}
}

// Version 返回应用版本信息
func (a *App) Version() map[string]string {
	return map[string]string{
		"version": a.appCfg.Version,
		"name":    a.appCfg.Name,
	}
}

// CheckForUpdate 版本更新检测
func (a *App) CheckForUpdate() update.Info {
	info := update.CheckForUpdate(a.appCfg)
	if info.Msg == "ok" {
		if info.HasUpdate {
			applog.Printf("[update] 发现新版本 %s（当前 %s）→ %s", info.LatestVersion, info.CurrentVersion, info.ReleaseUrl)
		} else {
			applog.Printf("[update] 已是最新版本 %s", info.CurrentVersion)
		}
	} else {
		applog.Errorf("[update] 检测更新失败: %s", info.Msg)
	}
	return info
}

// NICInfo 网卡信息
type NICInfo struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
}

// GetNICs 返回本机所有启用状态的 IPv4 网卡
func (a *App) GetNICs() []NICInfo {
	nics := []NICInfo{{Name: "全部网卡", IP: "0.0.0.0"}}
	ifaces, err := net.Interfaces()
	if err != nil {
		return nics
	}
	seen := map[string]bool{"0.0.0.0": true}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || seen[ip4.String()] {
				continue
			}
			seen[ip4.String()] = true
			nics = append(nics, NICInfo{Name: iface.Name, IP: ip4.String()})
		}
	}
	return nics
}

// GetLocalIPs 返回本机所有 IPv4 地址
func (a *App) GetLocalIPs() []string {
	ips := []string{"127.0.0.1"}
	seen := map[string]bool{"127.0.0.1": true}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || seen[ip4.String()] {
				continue
			}
			seen[ip4.String()] = true
			ips = append(ips, ip4.String())
		}
	}
	return ips
}

// CheckPortInUse 检测端口是否已被占用
func (a *App) CheckPortInUse(protocol string, port int) bool {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	lc := net.ListenConfig{Control: sockopt.ControlExclusiveAddr}
	if protocol == "tcp" {
		ln, err := lc.Listen(context.Background(), "tcp", addr)
		if err != nil {
			return true
		}
		_ = ln.Close()
		return false
	}
	pc, err := lc.ListenPacket(context.Background(), "udp", addr)
	if err != nil {
		return true
	}
	_ = pc.Close()
	return false
}

// GetPortOccupant 查询占用指定端口的进程名
func (a *App) GetPortOccupant(port int) string {
	target := fmt.Sprintf(":%d", port)
	for _, proto := range []string{"udp", "tcp"} {
		out, err := runcmd.RunHidden("netstat", "-ano", "-p", proto)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(out))
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 4 || !strings.EqualFold(fields[0], proto) {
				continue
			}
			if strings.HasSuffix(fields[1], target) {
				pid := fields[len(fields)-1]
				return fmt.Sprintf("%s (PID %s)", a.pidName(pid), pid)
			}
		}
	}
	return ""
}

// pidName 通过 tasklist 将 PID 映射为进程名
func (a *App) pidName(pid string) string {
	out, err := runcmd.RunHidden("tasklist", "/FI", "PID eq "+pid, "/FO", "CSV", "/NH")
	if err != nil {
		return "未知进程"
	}
	s := string(out)
	i := strings.Index(s, "\"")
	if i < 0 {
		return "未知进程"
	}
	j := strings.Index(s[i+1:], "\"")
	if j < 0 {
		return "未知进程"
	}
	return s[i+1 : i+1+j]
}

// GetAppDataDir 返回应用数据目录
func (a *App) GetAppDataDir() string {
	return a.storage.BaseDir()
}

// === 会话管理 ===

func (a *App) CreateSession(cfg models.SessionConfig) (models.SessionConfig, error) {
	return a.sessions.CreateSession(cfg)
}
func (a *App) DeleteSession(id string) models.APIResult {
	if err := a.sessions.DeleteSession(id); err != nil {
		return models.APIResult{Success: false, Message: err.Error()}
	}
	return models.APIResult{Success: true, Message: "会话已删除"}
}
func (a *App) ListSessions() []models.SessionInfo { return a.sessions.ListSessions() }
func (a *App) ReorderSessions(ids []string) models.APIResult {
	if err := a.sessions.ReorderSessions(ids); err != nil {
		return models.APIResult{Success: false, Message: err.Error()}
	}
	return models.APIResult{Success: true, Message: "顺序已保存"}
}
func (a *App) GetSessionConfig(id string) (models.SessionConfig, error) {
	cfg, ok := a.sessions.GetConfig(id)
	if !ok {
		return models.SessionConfig{}, fmt.Errorf("会话不存在: %s", id)
	}
	return cfg, nil
}
func (a *App) UpdateSessionConfig(cfg models.SessionConfig) models.APIResult {
	if err := a.sessions.UpdateConfig(cfg.ID, cfg); err != nil {
		return models.APIResult{Success: false, Message: err.Error()}
	}
	return models.APIResult{Success: true, Message: "配置已更新"}
}
func (a *App) StartSending(id string) models.APIResult {
	if err := a.sessions.Start(id); err != nil {
		return models.APIResult{Success: false, Message: err.Error()}
	}
	return models.APIResult{Success: true, Message: "已启动"}
}
func (a *App) StopSending(id string) models.APIResult {
	a.sessions.Stop(id)
	return models.APIResult{Success: true, Message: "已停止"}
}
func (a *App) StopAllSessions() models.APIResult {
	a.sessions.StopAll()
	return models.APIResult{Success: true, Message: "全部会话已停止"}
}
func (a *App) SendOnce(id, payload string) models.APIResult {
	if err := a.sessions.SendOnce(id, payload); err != nil {
		return models.APIResult{Success: false, Message: err.Error()}
	}
	return models.APIResult{Success: true, Message: "已发送"}
}
func (a *App) PauseReceive(id string)  { a.sessions.PauseReceive(id) }
func (a *App) ResumeReceive(id string) { a.sessions.ResumeReceive(id) }
func (a *App) ClearReceive(id string)  { a.sessions.ClearReceive(id) }
func (a *App) ExportReceive(id, path string) models.APIResult {
	if err := a.sessions.ExportReceive(id, path); err != nil {
		return models.APIResult{Success: false, Message: err.Error()}
	}
	return models.APIResult{Success: true, Message: path}
}

// ExportReceiveFiltered 仅导出查询浮层关键字命中的接收日志，Message 附带命中条数
func (a *App) ExportReceiveFiltered(id, path, keyword string) models.APIResult {
	n, err := a.sessions.ExportReceiveFiltered(id, path, keyword)
	if err != nil {
		return models.APIResult{Success: false, Message: err.Error()}
	}
	return models.APIResult{Success: true, Message: fmt.Sprintf("%s（共 %d 条）", path, n)}
}
func (a *App) GetReceiveBuffer(id string) []models.ReceiveEvent {
	return a.sessions.GetReceiveBuffer(id)
}
func (a *App) ClearStats(id string) { a.sessions.ClearStats(id) }
func (a *App) GetSnapshot(id string) (models.StatsSnapshot, error) {
	snap, ok := a.sessions.GetSnapshot(id)
	if !ok {
		return models.StatsSnapshot{}, fmt.Errorf("会话不存在: %s", id)
	}
	return snap, nil
}
func (a *App) EnableReceiveLog(id, path string) models.APIResult {
	if err := a.sessions.EnableReceiveLog(id, path); err != nil {
		return models.APIResult{Success: false, Message: err.Error()}
	}
	return models.APIResult{Success: true, Message: path}
}
func (a *App) DisableReceiveLog(id string) { a.sessions.DisableReceiveLog(id) }

// GetReceiveLogStatus 查询实时落盘状态（前端切换会话后恢复勾选显示）
func (a *App) GetReceiveLogStatus(id string) models.ReceiveLogStatus {
	return a.sessions.GetReceiveLogStatus(id)
}

// === 模板 ===

func (a *App) ListTemplates() []models.Template { return a.templates.List() }
func (a *App) SaveTemplate(t models.Template) (models.Template, error) {
	return a.templates.Save(t)
}
func (a *App) DeleteTemplate(id string) models.APIResult {
	if err := a.templates.Delete(id); err != nil {
		return models.APIResult{Success: false, Message: err.Error()}
	}
	return models.APIResult{Success: true, Message: "已删除"}
}
func (a *App) GetTemplate(id string) (models.Template, error) {
	t := a.templates.Get(id)
	if t == nil {
		return models.Template{}, fmt.Errorf("模板不存在: %s", id)
	}
	return *t, nil
}
func (a *App) ExportTemplates(path string) models.APIResult {
	if path == "" {
		return models.APIResult{Success: false, Message: "已取消导出"}
	}
	if err := a.templates.Export(path); err != nil {
		applog.Errorf("[template] 导出模板失败: %v", err)
		return models.APIResult{Success: false, Message: err.Error()}
	}
	n := len(a.templates.List())
	applog.Printf("[template] 已导出 %d 条模板到 %s", n, path)
	return models.APIResult{Success: true, Message: fmt.Sprintf("已导出 %d 条模板", n)}
}
func (a *App) ImportTemplates(path string) models.APIResult {
	if path == "" {
		return models.APIResult{Success: false, Message: "已取消导入"}
	}
	n, err := a.templates.Import(path)
	if err != nil {
		applog.Errorf("[template] 导入模板失败: %v", err)
		return models.APIResult{Success: false, Message: err.Error()}
	}
	applog.Printf("[template] 成功导入 %d 条模板（来源 %s）", n, path)
	return models.APIResult{Success: true, Message: fmt.Sprintf("成功导入 %d 条", n)}
}

// === Syslog ===

func (a *App) BuildSyslog(cfg models.SyslogConfig, message string) string {
	return string(syslog.Build(&cfg, message))
}
func (a *App) FacilityOptions() []map[string]any { return syslog.FacilityOptions() }
func (a *App) SeverityOptions() []map[string]any { return syslog.SeverityOptions() }

// === 文件对话框（Wails v3 原生 Dialog API） ===

// OpenFileDialog 弹出系统原生文件打开对话框
func (a *App) OpenFileDialog(title string) string {
	path, err := a.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("文本文件", "*.txt;*.log;*.csv").
		AddFilter("所有文件", "*.*").
		PromptForSingleSelection()
	if err != nil {
		return ""
	}
	return path
}

// ReadTextFile 读取本地文本文件内容返回给前端（加载样本文件后直接填入报文编辑器，不再绑定文件路径）。
// 优先按 UTF-8 解析；若含非法 UTF-8 序列则回退按 GBK 解码，兼容常见中文日志样本文件。
func (a *App) ReadTextFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	out, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), data)
	if err != nil {
		return string(data), nil
	}
	return string(out), nil
}

// SaveFileDialog 弹出系统原生文件保存对话框
func (a *App) SaveFileDialog(title, defaultName string) string {
	path, err := a.app.Dialog.SaveFile().
		SetFilename(defaultName).
		AddFilter("文本文件", "*.txt;*.log;*.csv").
		AddFilter("所有文件", "*.*").
		PromptForSingleSelection()
	if err != nil {
		return ""
	}
	return path
}

// === 全局设置 ===

func (a *App) GetSettings() models.Settings {
	return a.storage.LoadSettingsOrDefault()
}
func (a *App) SaveSettings(s models.Settings) models.APIResult {
	// 日志目录：先切换（失败时保持原目录并拒绝保存），再持久化设置
	dir, err := applog.SetDir(s.LogDir)
	if err != nil {
		applog.Errorf("[settings] 日志目录不可用: %v", err)
		return models.APIResult{Success: false, Message: fmt.Sprintf("日志目录不可用：%v", err)}
	}
	if strings.TrimSpace(s.LogDir) != "" {
		s.LogDir = dir // 保存绝对路径，便于用户确认实际位置
	}
	if err := a.storage.SaveSettings(s); err != nil {
		applog.Errorf("[settings] 保存设置失败: %v", err)
		return models.APIResult{Success: false, Message: err.Error()}
	}
	applog.Printf("[settings] 设置已保存（主题=%s 语言=%s 日志目录=%s）", s.Theme, s.Language, applog.Dir())
	return models.APIResult{Success: true, Message: "设置已保存"}
}

// === 日志目录 ===

// GetLogDir 返回当前日志目录
func (a *App) GetLogDir() string { return applog.Dir() }

// GetDefaultLogDir 返回默认日志目录（供设置页提示）
func (a *App) GetDefaultLogDir() string { return applog.DefaultDir() }

// OpenLogDir 在系统文件管理器中打开日志目录
func (a *App) OpenLogDir() models.APIResult {
	dir := applog.Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		applog.Errorf("[applog] 创建日志目录失败: %v", err)
		return models.APIResult{Success: false, Message: err.Error()}
	}
	if err := runcmd.OpenPath(dir); err != nil {
		applog.Errorf("[applog] 打开日志目录失败: %v", err)
		return models.APIResult{Success: false, Message: err.Error()}
	}
	applog.Printf("[applog] 已打开日志目录 %s", dir)
	return models.APIResult{Success: true, Message: dir}
}

// PickDirectory 弹出系统原生目录选择对话框，取消时返回空串
func (a *App) PickDirectory(title string) string {
	path, err := a.app.Dialog.OpenFile().
		SetTitle(title).
		CanChooseFiles(false).
		CanChooseDirectories(true).
		PromptForSingleSelection()
	if err != nil {
		return ""
	}
	return path
}
