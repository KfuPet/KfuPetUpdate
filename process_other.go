//go:build !windows

package main

import (
	"os/exec"
	"time"
)

// startDetached 见 Windows 实现；非 Windows 平台仅保证可编译。
func startDetached(exe, dir string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	return cmd.Start()
}

// waitProcessExit 非 Windows 平台不做等待。
func waitProcessExit(int, time.Duration) {}

// removeTempDirLater 非 Windows 平台不做处理，仅保证可编译。
func removeTempDirLater() {}
