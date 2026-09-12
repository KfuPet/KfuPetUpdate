package main

import (
	"os"
	"path/filepath"

	"kfupet-installer/internal/winreg"
)

// executableName 是安装目录内用于校验程序是否仍存在的文件名。
const executableName = "KfuPet.exe"

// updaterName 是本程序在安装目录内的常驻副本名。
// 安装完成后会把自身复制成这个名字，卸载入口与 KfuPet 的「检查更新」都指向它。
const updaterName = "KfuPetUpdate.exe"

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
	rec, err := winreg.ReadInstallRecord()
	if err != nil || rec == nil || rec.InstallPath == "" {
		return installState{}
	}

	exePath := filepath.Join(rec.InstallPath, executableName)
	if info, err := os.Stat(exePath); err != nil || info.IsDir() {
		_ = winreg.ClearInstallRecord()
		_ = winreg.ClearUninstallEntry() // 标准卸载入口是成对写入的，一并清掉
		return installState{}
	}

	return installState{
		Installed: true,
		Path:      rec.InstallPath,
		Version:   rec.DisplayVersion,
	}
}
