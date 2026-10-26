//go:build !windows

// Package sockopt 提供跨平台 socket 选项控制。非 Windows 平台无需独占处理。
package sockopt

import "syscall"

// ControlExclusiveAddr 非 Windows 平台为空实现
func ControlExclusiveAddr(network, address string, c syscall.RawConn) error {
	return nil
}
