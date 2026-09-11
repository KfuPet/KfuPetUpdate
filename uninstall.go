package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// uninstallOptions 是卸载时用户的选择。
type uninstallOptions struct {
	KeepUserData bool // 保留个人数据
}

// userDataDirs 返回 KfuPet 可能写入的个人数据目录。
// KfuPet 是 .NET 应用，按惯例取 Roaming（%APPDATA%）与 Local（%LOCALAPPDATA%）下的同名目录；
// 若实际路径不同，改这一处即可。
func userDataDirs() []string {
	var dirs []string
	if base, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(base, "KfuPet"))
	}
	if base, err := os.UserCacheDir(); err == nil {
		dirs = append(dirs, filepath.Join(base, "KfuPet"))
	}
	return dirs
}

// uninstallEntryFor 组装标准卸载入口的内容。
// UninstallString 指向本程序自身：在「应用和功能」里点卸载会打开本界面，
// 再由用户点「卸载」确认（等 CLI 模式落地后可改成带参数直接卸载）。
func uninstallEntryFor(installDir, version string) uninstallEntry {
	self, err := os.Executable()
	if err != nil {
		self = ""
	} else {
		self = `"` + self + `"` // 路径可能含空格，统一加引号
	}

	return uninstallEntry{
		DisplayName:     "KfuPet",
		DisplayVersion:  version,
		UninstallString: self,
		DisplayIcon:     filepath.Join(installDir, executableName),
		Publisher:       "KfuPet",
	}
}

// uninstallKfuPet 卸载 KfuPet。
// 顺序很关键：先删程序文件，最后才删注册表记录。中间任何一步失败都保留注册表，
// 用户重试时才有据可依；反过来（先删记录）会留下"显示未安装、文件却还在"的状态，
// 用户以为卸干净了，比直接报错更糟。
func uninstallKfuPet(installDir string, opts uninstallOptions) error {
	// 程序正在运行时文件被占用，先让用户退出。
	if isExecutableBusy(filepath.Join(installDir, executableName)) {
		return fmt.Errorf("KfuPet 正在运行，请先退出后再卸载")
	}

	if err := os.RemoveAll(installDir); err != nil {
		return fmt.Errorf("删除安装目录失败：%w", err)
	}
	// 顺带清掉安装过程中可能遗留的暂存与备份目录。
	for _, leftover := range []string{installDir + ".new", installDir + ".old"} {
		if err := os.RemoveAll(leftover); err != nil {
			return fmt.Errorf("清理残留目录失败（%s）：%w", leftover, err)
		}
	}

	if err := removeShortcuts(); err != nil {
		return err
	}

	if !opts.KeepUserData {
		if err := removeUserData(); err != nil {
			return err
		}
	}

	if err := clearInstallRecord(); err != nil {
		return fmt.Errorf("删除安装信息失败：%w", err)
	}
	if err := clearUninstallEntry(); err != nil {
		return fmt.Errorf("删除卸载入口失败：%w", err)
	}
	return nil
}

// removeUserData 删除 KfuPet 的个人数据目录；目录本就不存在不算错误。
func removeUserData() error {
	for _, dir := range userDataDirs() {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("删除个人数据失败（%s）：%w", dir, err)
		}
	}
	return nil
}
