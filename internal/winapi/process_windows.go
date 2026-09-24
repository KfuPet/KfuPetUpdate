//go:build windows

package winapi

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

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
	WaitProcessExitTimeout(pid, timeout)
}

// WaitProcessExitTimeout 等待指定进程退出，返回它是否在 timeout 内退出。
// 等待是可打断的：进程一退出立即返回 true，不会干等满 timeout。
// 进程不存在或无权限打开时同样返回 true（都按"已退出"处理，由后续占用检测兜底报错）。
func WaitProcessExitTimeout(pid int, timeout time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)

	event, err := windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
	// WAIT_OBJECT_0：进程已退出；WAIT_TIMEOUT / 出错：仍活着或状态未知。
	return err == nil && event == windows.WAIT_OBJECT_0
}

// KillProcess 强制终止指定进程（TerminateProcess）。
// 有两处使用：用户明确同意后强杀卡住的 KfuPet；以及结束一个闲着的旧更新程序实例
// （那种实例没有进行中的工作可丢，见调用方的判断）。
// 绝不能对正在安装/修复的实例调用：强杀会把安装目录留在装了一半的状态。
func KillProcess(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)

	return windows.TerminateProcess(h, 1)
}

// FindOtherProcessPID 找出另一个以给定文件名运行的进程，返回它的 PID；找不到返回 0。
// 用进程快照按 exe 名比对：本程序只有两种名字（分发名与安装目录内的常驻副本），
// 撞名的可能性可以忽略。names 由调用方给出，避免这里再抄一份文件名。
func FindOtherProcessPID(names []string) int {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snapshot)

	self := uint32(os.Getpid())
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if entry.ProcessID == 0 || entry.ProcessID == self {
			continue
		}
		name := windows.UTF16ToString(entry.ExeFile[:])
		for _, want := range names {
			if strings.EqualFold(name, want) {
				return int(entry.ProcessID)
			}
		}
	}
	return 0
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
