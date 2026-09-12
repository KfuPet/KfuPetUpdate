//go:build !windows

package winapi

import "time"

// AcquireInstanceLock 非 Windows 平台不设限：安装本身也不可用，仅保证可编译。
func AcquireInstanceLock(time.Duration) error { return nil }

// ReleaseInstanceLock 同上，空实现。
func ReleaseInstanceLock() {}
