//go:build !windows

// 非 Windows 平台实现：无控制台窗口概念，直接执行命令；
// 保留 3 秒超时，行为与 Windows 版一致（netstat/tasklist 探测在这些平台本就不可用，失败会静默降级）。
package runcmd

import (
	"context"
	"os/exec"
	"time"
)

// RunHidden 执行命令，超时（3 秒）自动终止防止卡住
func RunHidden(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}
