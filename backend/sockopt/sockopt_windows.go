//go:build windows

// Package sockopt 提供跨平台 socket 选项控制。
// Windows 下强制 SO_EXCLUSIVEADDRUSE：防止 Go 默认 SO_REUSEADDR 导致
// 「端口已被其他进程占用却绑定假成功，入站包全部被抢先绑定者收走」的问题。
package sockopt

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// soExclusiveAddrUse winsock2.h 中 SO_EXCLUSIVEADDRUSE 的值（x/sys/windows 未定义）
const soExclusiveAddrUse = -5

// ControlExclusiveAddr 监听配置回调：设置 SO_EXCLUSIVEADDRUSE（端口独占）
func ControlExclusiveAddr(network, address string, c syscall.RawConn) error {
	var opErr error
	if err := c.Control(func(fd uintptr) {
		opErr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, soExclusiveAddrUse, 1)
	}); err != nil {
		return err
	}
	return opErr
}
