//go:build windows

package winapi

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// CreateNoWindow 对应 Win32 的 CREATE_NO_WINDOW，
// 避免从 GUI 进程拉起子进程（PowerShell、临时副本）时闪出一个控制台窗口。
const CreateNoWindow = 0x08000000

// StartDetached 以隐藏窗口的方式启动一个不等它结束的进程（临时副本）。
func StartDetached(exe, dir string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CreateNoWindow}
	return cmd.Start()
}

// RunHidden 以隐藏窗口方式运行进程并等它结束，返回其退出码。
// 用于静默安装运行环境这类不能弹出窗口的子进程（安装程序返回非零码时不视为启动失败）。
func RunHidden(exe string, args ...string) (int, error) {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CreateNoWindow}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

// WaitProcessExit 阻塞等待指定进程退出，最长 timeout。
// 进程已退出、不存在或无权限打开时立即返回（都按"已退出"处理）。
func WaitProcessExit(pid int, timeout time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
}

// RemoveTempDirLater 安排在本进程退出后删掉 dir。
// 运行中的 exe 删不掉自己，于是交给一个脱离本进程的 cmd 延时执行：
// 先用 ping 拖住几秒等我们退出（无控制台时 timeout 命令会直接报错），
// 那时映像已解锁，rmdir 就能连目录一起删掉。
// 目录是否适合删除（例如必须位于临时副本目录内）由调用方判断。
func RemoveTempDirLater(dir string) {
	script := fmt.Sprintf("ping -n 3 127.0.0.1 >nul & rmdir /s /q \"%s\"", dir)
	cmd := exec.Command("cmd", "/c", script)
	// 工作目录必须挪出待删目录：进程会占住自己的当前目录，否则 rmdir 必然失败。
	cmd.Dir = os.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: CreateNoWindow}
	_ = cmd.Start()
}
