//go:build !windows

package winapi

import (
	"os/exec"
	"time"
)

// StartDetached 见 Windows 实现；非 Windows 平台仅保证可编译。
func StartDetached(exe, dir string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	return cmd.Start()
}

// WaitProcessExit 非 Windows 平台不做等待。
func WaitProcessExit(int, time.Duration) {}

// RemoveTempDirLater 非 Windows 平台不做处理，仅保证可编译。
func RemoveTempDirLater(string) {}
