//go:build !windows

// Package closeconfirm 非 Windows 平台 stub：SuperSender 仅在 Windows 上构建，
// 此文件不会被编译。v3 MessageDialog API 与 v2 不同（异步 + 回调），
// 如需移植到 Linux/macOS 需重写 AskCloseAction。
package closeconfirm

import "context"

const (
	ActionMinimize = "minimize"
	ActionQuit     = "quit"
	ActionCancel   = "cancel"
)

// AskCloseAction 非 Windows 平台 stub，始终返回取消。
func AskCloseAction(_ context.Context, appName string) string {
	return ActionCancel
}
