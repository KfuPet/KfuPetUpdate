package main

import (
	"os"
	"path/filepath"
)

// executableName 是安装目录内用于校验程序是否仍存在的文件名。
const executableName = "KfuPet.exe"

// updaterName 是本程序在安装目录内的常驻副本名。
// 安装完成后会把自身复制成这个名字，卸载入口与 KfuPet 的「检查更新」都指向它。
const updaterName = "KfuPetUpdate.exe"

// installRecord 是从注册表读到的原始安装记录。
type installRecord struct {
	InstallPath    string // 软件安装目录完整路径
	DisplayVersion string // 本地版本号，不带 v 前缀
}

// uninstallEntry 是写进 Windows 标准卸载入口的信息，
// 用于让「设置 → 应用和功能」能列出并卸载本程序。
type uninstallEntry struct {
	DisplayName     string // 显示名称
	DisplayVersion  string // 显示版本
	UninstallString string // 点「卸载」时执行的命令
	DisplayIcon     string // 图标来源
	Publisher       string // 发布者
}

// installState 是本机 KfuPet 的安装状态，界面据此决定可用操作。
type installState struct {
	Installed bool   // 是否已安装
	Path      string // 安装目录（Installed 为真时有效）
	Version   string // 本地版本号（Installed 为真时有效）
}

// detectInstallState 读取注册表安装记录并校验其是否仍然有效。
// 若注册表存在记录但程序文件已不在（例如用户直接删了整个文件夹），
// 说明是残留记录：清理注册表并返回"未安装"，避免界面停留在已安装状态却无法操作。
func detectInstallState() installState {
	rec, err := readInstallRecord()
	if err != nil || rec == nil || rec.InstallPath == "" {
		return installState{}
	}

	exePath := filepath.Join(rec.InstallPath, executableName)
	if info, err := os.Stat(exePath); err != nil || info.IsDir() {
		_ = clearInstallRecord()
		_ = clearUninstallEntry() // 标准卸载入口是成对写入的，一并清掉
		return installState{}
	}

	return installState{
		Installed: true,
		Path:      rec.InstallPath,
		Version:   rec.DisplayVersion,
	}
}
