//go:build !windows

package main

import "time"

// acquireInstanceLock 非 Windows 平台不设限：安装本身也不可用，仅保证可编译。
func acquireInstanceLock(time.Duration) error { return nil }

// releaseInstanceLock 同上，空实现。
func releaseInstanceLock() {}
