//go:build windows

// Windows 平台实现：用资源管理器打开目录/文件。
package runcmd

import "os/exec"

// OpenPath 用系统文件管理器打开指定目录（异步发起，不等待进程结束）。
// explorer 成功时也可能返回非零退出码，因此只用 Start 不等结果。
func OpenPath(path string) error {
	return exec.Command("explorer", path).Start()
}
