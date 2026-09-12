//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// startDetached 以隐藏窗口的方式启动一个不等它结束的进程（临时副本）。
func startDetached(exe, dir string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return cmd.Start()
}

// waitProcessExit 阻塞等待指定进程退出，最长 timeout。
// 进程已退出、不存在或无权限打开时立即返回（都按"已退出"处理）。
func waitProcessExit(pid int, timeout time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
}

// removeTempDirLater 安排在本进程退出后删掉临时副本目录。
// 运行中的 exe 删不掉自己，于是交给一个脱离本进程的 cmd 延时执行：
// 先用 ping 拖住几秒等我们退出（无控制台时 timeout 命令会直接报错），
// 那时映像已解锁，rmdir 就能连目录一起删掉。
// 只在自己确实位于临时副本目录内时才动手，避免误删安装目录。
func removeTempDirLater() {
	dir := selfDir()
	if dir == "" || !strings.HasPrefix(filepath.Base(dir), tempDirPrefix) {
		return
	}
	script := fmt.Sprintf("ping -n 3 127.0.0.1 >nul & rmdir /s /q \"%s\"", dir)
	cmd := exec.Command("cmd", "/c", script)
	// 工作目录必须挪出待删目录：进程会占住自己的当前目录，否则 rmdir 必然失败。
	cmd.Dir = os.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	_ = cmd.Start()
}
