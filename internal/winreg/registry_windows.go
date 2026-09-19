//go:build windows

// Package winreg 封装 KfuPet 的注册表读写：安装记录与 Windows 标准卸载入口。
package winreg

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// installRegistryPath 是安装记录的注册表位置（用户域，无需管理员权限）。
const installRegistryPath = `Software\KfuPet`

// uninstallEntryPath 是 Windows 标准卸载入口的位置，
// 写在这里的程序会出现在「设置 → 应用和功能」列表中。
const uninstallEntryPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\KfuPet`

// 注册表内的值名。
const (
	valueInstallPath = "InstallPath"
	valueVersion     = "DisplayVersion"
)

// ReadInstallRecord 读取注册表安装记录。
// 记录不存在时返回 (nil, nil)，表示"从未安装"，不算错误。
func ReadInstallRecord() (*InstallRecord, error) {
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

	return &InstallRecord{InstallPath: installPath, DisplayVersion: version}, nil
}

// WriteInstallRecord 写入（或覆盖）安装记录。
func WriteInstallRecord(rec InstallRecord) error {
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

// WriteUninstallEntry 写入标准卸载入口。
func WriteUninstallEntry(e UninstallEntry) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallEntryPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	values := map[string]string{
		"DisplayName":     e.DisplayName,
		"DisplayVersion":  e.DisplayVersion,
		"UninstallString": e.UninstallString,
		"DisplayIcon":     e.DisplayIcon,
		"Publisher":       e.Publisher,
	}
	for name, value := range values {
		if err := k.SetStringValue(name, value); err != nil {
			return err
		}
	}
	return nil
}

// ClearUninstallEntry 删除标准卸载入口；项本就不存在时视为成功。
func ClearUninstallEntry() error {
	if err := registry.DeleteKey(registry.CURRENT_USER, uninstallEntryPath); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

// ClearInstallRecord 删除整个安装记录项；项本就不存在时视为成功。
func ClearInstallRecord() error {
	if err := registry.DeleteKey(registry.CURRENT_USER, installRegistryPath); err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}
