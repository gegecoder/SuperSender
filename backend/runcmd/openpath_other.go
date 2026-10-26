//go:build !windows

// Linux/macOS 平台实现：调用系统文件管理器打开目录/文件。
package runcmd

import (
	"os/exec"
	"runtime"
)

// OpenPath 用系统文件管理器打开指定目录（异步发起，不等待进程结束）。
// macOS 用 open，Linux 用 xdg-open。
func OpenPath(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}
