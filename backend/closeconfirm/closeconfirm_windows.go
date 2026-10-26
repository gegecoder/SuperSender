//go:build windows

// Package closeconfirm 提供关闭窗口时的用户确认对话框：
// 最小化到托盘 / 直接退出 / 取消。
package closeconfirm

import (
	"context"
	"syscall"
	"unsafe"
)

// 关闭确认对话框的语义动作
const (
	ActionMinimize = "minimize" // 最小化到托盘
	ActionQuit     = "quit"     // 直接退出
	ActionCancel   = "cancel"   // 取消，保持窗口
)

// Win32 MessageBox 标志与返回值
// 参考 https://learn.microsoft.com/windows/win32/api/winuser/nf-winuser-messageboxw
const (
	mbYesNoCancel  = 0x00000003
	mbIconQuestion = 0x00000020
	mbSystemModal  = 0x00001000

	idCancel = 2
	idYes    = 6
	idNo     = 7
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

// AskCloseAction 弹出系统原生「是 / 否 / 取消」三按钮询问框。
// Wails 封装的 QuestionDialog 在 Windows 上固定为 MB_YESNO（只有是/否、
// 且自定义文案不生效），因此这里直接调用 user32.MessageBoxW。
//
//	是   -> 最小化到托盘
//	否   -> 直接退出
//	取消 -> 保持窗口
func AskCloseAction(_ context.Context, appName string) string {
	titlePtr, _ := syscall.UTF16PtrFromString("关闭 " + appName)
	messagePtr, _ := syscall.UTF16PtrFromString(
		"关闭窗口后，是否最小化到系统托盘继续在后台运行？\n\n" +
			"「是」最小化到托盘，「否」直接退出程序。")

	// hWnd 传 0 配合 MB_SYSTEMMODAL，无需依赖主窗口句柄即可弹出模态框
	ret, _, _ := procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(messagePtr)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(mbYesNoCancel|mbIconQuestion|mbSystemModal),
	)

	switch ret {
	case idYes:
		return ActionMinimize
	case idNo:
		return ActionQuit
	default: // idCancel、对话框 X 关闭或异常
		return ActionCancel
	}
}
