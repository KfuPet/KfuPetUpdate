package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// tempDirPrefix 是临时副本目录的固定前缀，用于启动时清理历史残留。
const tempDirPrefix = "KfuPetUpdate-"

// processWaitTimeout 是等待目标进程退出的上限；超时仍继续，由后续占用检测兜底报错。
const processWaitTimeout = 60 * time.Second

// copySelf 把当前运行的程序复制到 dst。
// 运行中的 exe 映像允许以只读方式打开，所以读自己没有障碍；
// 写目标必须是别的路径，否则会撞上"自己占用自己"。
func copySelf(dst string) error {
	self, err := selfPath()
	if err != nil {
		return err
	}
	if samePath(self, dst) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(self)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// selfPath 返回当前进程的 exe 路径。
func selfPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Clean(self), nil
}

// selfDir 返回当前进程所在目录；取不到时返回空串。
func selfDir() string {
	self, err := selfPath()
	if err != nil {
		return ""
	}
	return filepath.Dir(self)
}

// isSelfWithin 判断本程序是否正运行于 dir 内。
// 是则任何删除/替换 dir 的操作都会因自身映像被占用而失败，必须先交棒给临时副本。
func isSelfWithin(dir string) bool {
	self, err := selfPath()
	if err != nil {
		return false
	}
	return pathWithin(dir, self)
}

// pathWithin 判断 target 是否位于 base 之内；base 尚不存在也能判断。
// Windows 下先统一大小写，避免注册表里的路径与实际路径大小写不同导致漏判。
func pathWithin(base, target string) bool {
	if runtime.GOOS == "windows" {
		base, target = strings.ToLower(base), strings.ToLower(target)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// samePath 判断两个路径是否指向同一个文件（Windows 下大小写不敏感）。
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// relayToTemp 把自身复制到临时目录并重启一个副本来继续执行 c，本进程随即退出。
// 临时副本位于安装目录之外，因此它能删除/替换安装目录；它会先等本进程退出
// （通过追加的 --wait-pid），以免本进程的映像仍占用安装目录内的文件。
func relayToTemp(c command) error {
	// 副本要接着干活，得先拿得到单实例锁，所以交棒前必须先让出去。
	releaseInstanceLock()

	tempDir, err := os.MkdirTemp("", tempDirPrefix)
	if err != nil {
		return fmt.Errorf("创建临时目录失败：%w", err)
	}

	dst := filepath.Join(tempDir, updaterName)
	if err := copySelf(dst); err != nil {
		return fmt.Errorf("准备临时副本失败：%w", err)
	}

	c.WaitPIDs = append(c.WaitPIDs, os.Getpid())
	if err := startDetached(dst, tempDir, c.args()); err != nil {
		return fmt.Errorf("启动临时副本失败：%w", err)
	}
	return nil
}

// waitForProcesses 依次等待给定进程退出，每个最多等 processWaitTimeout。
// 进程不存在或无权限打开时直接跳过，由后续占用检测兜底。
func waitForProcesses(pids []int) {
	for _, pid := range pids {
		if pid <= 0 || pid == os.Getpid() {
			continue
		}
		waitProcessExit(pid, processWaitTimeout)
	}
}

// cleanStaleTempDirs 清理遗留下来的临时副本目录。
// 正常退出的副本会自删目录（见 removeTempDirLater），这里兜底的是崩溃、
// 被强杀等没走成自删的情况。仍被占用的（另一个实例正在用）删除会失败，忽略即可。
func cleanStaleTempDirs() {
	base := os.TempDir()
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	self := selfDir()
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), tempDirPrefix) {
			continue
		}
		dir := filepath.Join(base, e.Name())
		if samePath(dir, self) {
			continue // 正在就地运行，别把自己删了
		}
		_ = os.RemoveAll(dir)
	}
}
