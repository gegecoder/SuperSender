// Package applog 程序运行日志收集：业务代码直接调用 Printf/Warnf/Errorf
// 写入内存环形缓冲区，供前端「日志」浮层查询展示；
// 同时拦截标准 log 输出，收集第三方库日志。
//
// 日志同时持久化到文件（默认 <os.UserConfigDir>/SuperSender/logs/super-sender.log，
// 可在全局设置中自定义目录），便于用户事后排查问题。
package applog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Entry 单条日志
type Entry struct {
	Time    string `json:"time"`    // HH:mm:ss
	Level   string `json:"level"`   // info / warn / error
	Message string `json:"message"` // 日志正文（不含时间前缀）
}

const (
	maxEntries  = 2000          // 环形缓冲上限
	appDirName  = "SuperSender" // 应用配置目录名
	logFileName = "super-sender.log"
)

var (
	mu      sync.Mutex
	entries []Entry
	console io.Writer // 控制台输出目标（Init 时设置，GUI 下写失败无影响）
)

// 文件持久化状态：fileMu 保护 logDir / logFile，与缓冲区锁分离，
// 避免文件 IO 阻塞前端读取日志。
var (
	fileMu  sync.Mutex
	logDir  string
	logFile *os.File
)

// DefaultDir 默认日志目录：<os.UserConfigDir>/SuperSender/logs，失败回退用户主目录。
func DefaultDir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base, _ = os.UserHomeDir()
	}
	return filepath.Join(base, appDirName, "logs")
}

// Dir 返回当前日志目录（未初始化时返回默认目录）。
func Dir() string {
	fileMu.Lock()
	defer fileMu.Unlock()
	if logDir == "" {
		return DefaultDir()
	}
	return logDir
}

// SetDir 切换日志目录：创建目录、打开日志文件并关闭旧句柄。
// dir 为空时回退默认目录。失败时保持原目录不变并返回错误。
func SetDir(dir string) (string, error) {
	target := strings.TrimSpace(dir)
	if target == "" {
		target = DefaultDir()
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		abs = target
	}

	fileMu.Lock()
	defer fileMu.Unlock()

	if err := os.MkdirAll(abs, 0o755); err != nil {
		return abs, fmt.Errorf("创建日志目录失败: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(abs, logFileName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return abs, fmt.Errorf("打开日志文件失败: %w", err)
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	logDir = abs
	logFile = f
	return abs, nil
}

// Close 关闭日志文件句柄（应用退出时调用，确保日志落盘）。
func Close() {
	fileMu.Lock()
	defer fileMu.Unlock()
	if logFile != nil {
		_ = logFile.Close()
		logFile = nil
	}
}

// writeFile 将一行日志追加写入当前日志文件（未初始化时静默忽略）。
func writeFile(line string) {
	fileMu.Lock()
	defer fileMu.Unlock()
	if logFile != nil {
		_, _ = logFile.WriteString(line)
	}
}

// fileSink 实现 io.Writer，把标准库 log 输出同步写入日志文件。
// 必须放在 io.MultiWriter 第一位：GUI 子系统（-H windowsgui）下 stdout
// 是无效句柄，写失败会中断后续 writer，放最前面可保证文件先落盘。
type fileSink struct{}

func (w *fileSink) Write(p []byte) (int, error) {
	writeFile(string(p))
	return len(p), nil
}

// lineWriter 实现 io.Writer，按行解析标准库 log 输出
type lineWriter struct{}

func (w *lineWriter) Write(p []byte) (int, error) {
	text := string(p)
	// 标准库 log 每次输出以换行结尾，可能包含多行
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		add(line)
	}
	return len(p), nil
}

// add 解析单行日志并入缓冲区
func add(line string) {
	e := Entry{Time: time.Now().Format("15:04:05"), Level: "info", Message: line}
	// 标准库 log 前缀格式：2006/01/02 15:04:05 message
	if i := strings.Index(line, " "); i > 0 && i+1 < len(line) {
		if j := strings.Index(line[i+1:], " "); j > 0 {
			rest := line[i+1+j+1:]
			if rest != "" {
				e.Message = rest
			}
		}
	}
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "[error]") || strings.Contains(lower, "error") || strings.Contains(lower, "失败") || strings.Contains(lower, "panic"):
		e.Level = "error"
	case strings.Contains(lower, "[warn]") || strings.Contains(lower, "warn") || strings.Contains(lower, "警告"):
		e.Level = "warn"
	}
	mu.Lock()
	defer mu.Unlock()
	entries = append(entries, e)
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}
}

// safeStdout 包装 os.Stdout：打包后的 GUI 子系统进程中 stdout 是无效句柄，
// 直接写入会返回错误；io.MultiWriter 遇到错误会中断后续 writer，
// 导致环形缓冲永远收不到日志。此包装吞掉错误，保证缓冲正常收集。
type safeStdout struct{ w io.Writer }

func (s safeStdout) Write(p []byte) (int, error) {
	n, _ := s.w.Write(p)
	return n, nil
}

// Init 初始化：打开默认日志目录，设置控制台输出，并拦截标准 log 输出
// （收集第三方库日志）。
func Init() {
	console = safeStdout{os.Stdout}
	log.SetOutput(io.MultiWriter(&fileSink{}, safeStdout{os.Stdout}, &lineWriter{}))
	// 先落到默认目录，随后 NewApp 会按全局设置切换到用户配置的目录
	if _, err := SetDir(""); err != nil {
		fmt.Fprintf(os.Stderr, "applog: 初始化日志目录失败: %v\n", err)
	}
}

// Printf 记录 info 级日志。业务代码直接调用本函数写入运行日志缓冲，
// 不依赖标准 log 重定向链路，GUI 打包环境下同样可靠。
func Printf(format string, args ...any) {
	write("info", fmt.Sprintf(format, args...))
}

// Warnf 记录 warn 级日志
func Warnf(format string, args ...any) {
	write("warn", fmt.Sprintf(format, args...))
}

// Errorf 记录 error 级日志
func Errorf(format string, args ...any) {
	write("error", fmt.Sprintf(format, args...))
}

// write 直接入缓冲区，同步写日志文件并输出到控制台
func write(level, msg string) {
	msg = strings.TrimRight(msg, "\n")
	if msg == "" {
		return
	}
	now := time.Now()
	e := Entry{Time: now.Format("15:04:05"), Level: level, Message: msg}
	mu.Lock()
	entries = append(entries, e)
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}
	mu.Unlock()

	line := fmt.Sprintf("%s [%s] %s\n", now.Format("2006/01/02 15:04:05"), level, msg)
	writeFile(line)
	if console != nil {
		_, _ = fmt.Fprint(console, line)
	}
}

// Recent 返回最近的日志（最多 maxEntries 条，按时间正序）
func Recent() []Entry {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Entry, len(entries))
	copy(out, entries)
	return out
}

// Clear 清空缓冲区并截断日志文件
func Clear() {
	mu.Lock()
	entries = nil
	mu.Unlock()

	fileMu.Lock()
	defer fileMu.Unlock()
	if logFile != nil {
		_ = logFile.Truncate(0)
	}
}
