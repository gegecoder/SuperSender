//go:build windows

// Windows 平台实现：端口探测执行 netstat/tasklist 时隐藏控制台窗口，
// 通过 HideWindow + CREATE_NO_WINDOW 防止 cmd 窗口闪烁。
package runcmd

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// RunHidden 执行命令并隐藏控制台窗口，超时（3 秒）自动终止防止卡住
func RunHidden(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd.Output()
}
