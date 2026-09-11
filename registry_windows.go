//go:build windows

package main

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// installRegistryPath 是安装记录的注册表位置（用户域，无需管理员权限）。
const installRegistryPath = `Software\KfuPet`

// 注册表内的值名。
const (
	valueInstallPath = "InstallPath"
	valueVersion     = "DisplayVersion"
)

// readInstallRecord 读取注册表安装记录。
// 记录不存在时返回 (nil, nil)，表示"从未安装"，不算错误。
func readInstallRecord() (*installRecord, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, installRegistryPath, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer k.Close()

	installPath, _, err := k.GetStringValue(valueInstallPath)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	// 版本号缺失不视为致命，仅记录为空。
	version, _, _ := k.GetStringValue(valueVersion)

	return &installRecord{InstallPath: installPath, DisplayVersion: version}, nil
}

// writeInstallRecord 写入（或覆盖）安装记录。
func writeInstallRecord(rec installRecord) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, installRegistryPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if err := k.SetStringValue(valueInstallPath, rec.InstallPath); err != nil {
		return err
	}
	return k.SetStringValue(valueVersion, rec.DisplayVersion)
}

// clearInstallRecord 删除整个安装记录项；项本就不存在时视为成功。
func clearInstallRecord() error {
	if err := registry.DeleteKey(registry.CURRENT_USER, installRegistryPath); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}
