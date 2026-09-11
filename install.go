package main

import (
	"os"
	"path/filepath"
)

// executableName 是安装目录内用于校验程序是否仍存在的文件名。
const executableName = "KfuPet.exe"

// installRecord 是从注册表读到的原始安装记录。
type installRecord struct {
	InstallPath    string // 软件安装目录完整路径
	DisplayVersion string // 本地版本号，不带 v 前缀
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
		return installState{}
	}

	return installState{
		Installed: true,
		Path:      rec.InstallPath,
		Version:   rec.DisplayVersion,
	}
}
