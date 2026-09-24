//go:build windows

// Package winreg 封装 KfuPet 的注册表读写：安装记录与 Windows 标准卸载入口。
package winreg

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// 安装信息写在机器级（HKLM）而不是用户级（HKCU）：程序装到 %ProgramFiles%（机装），
// 若记录只落在安装者自己的用户域，多用户机器上其他用户就看不到"已安装"。
// 写 HKLM 需要管理员权限，本程序以 requireAdministrator 运行（见 app.manifest）。
//
// 读写统一附加 regView：固定使用 64 位注册表视图，避免进程位数不同时被系统
// 重定向到 Wow6432Node（32 位视图），导致同一条记录两边读不到。
const (
	// installRegistryPath 是安装记录的注册表位置。
	installRegistryPath = `Software\KfuPet`

	// uninstallEntryPath 是 Windows 标准卸载入口的位置，
	// 写在这里的程序会出现在「设置 → 应用和功能」列表中。
	uninstallEntryPath = `Software\Microsoft\Windows\CurrentVersion\Uninstall\KfuPet`
)

// regView 固定 64 位注册表视图，附加到打开/创建键的访问标记上。
const regView = registry.WOW64_64KEY

// 注册表内的值名。
const (
	valueInstallPath = "InstallPath"
	valueVersion     = "DisplayVersion"
)

// ReadInstallRecord 读取注册表安装记录。
// 先读机器级（HKLM），读不到再回退用户级（HKCU）——后者是早期版本写下的位置，
// 老用户升级一次后记录就会迁到机器级。
// 记录不存在时返回 (nil, nil)，表示"从未安装"，不算错误。
func ReadInstallRecord() (*InstallRecord, error) {
	rec, err := readInstallRecord(registry.LOCAL_MACHINE)
	if err != nil || rec != nil {
		return rec, err
	}
	return readInstallRecord(registry.CURRENT_USER)
}

// readInstallRecord 从指定根键读取安装记录；记录不存在时返回 (nil, nil)。
func readInstallRecord(root registry.Key) (*InstallRecord, error) {
	k, err := registry.OpenKey(root, installRegistryPath, registry.QUERY_VALUE|regView)
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

// ReadMachineInstallRecord 只读机器级安装记录。
// ReadInstallRecord 会回退到 HKCU，拿回一条早期版本写在用户域的记录，
// 而那并不能说明机器级已经写好——「修复」要用它判断记录是否真的补齐到位。
// 记录不存在时返回 (nil, nil)。
func ReadMachineInstallRecord() (*InstallRecord, error) {
	return readInstallRecord(registry.LOCAL_MACHINE)
}

// ReadUninstallEntry 读取机器级标准卸载入口；入口不存在时返回 (nil, nil)。
// 不存在的判断只看键，键下某个值缺失时按空串返回，由调用方逐字段比对。
func ReadUninstallEntry() (*UninstallEntry, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstallEntryPath, registry.QUERY_VALUE|regView)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer k.Close()

	get := func(name string) string {
		v, _, _ := k.GetStringValue(name)
		return v
	}
	return &UninstallEntry{
		DisplayName:     get("DisplayName"),
		DisplayVersion:  get("DisplayVersion"),
		UninstallString: get("UninstallString"),
		DisplayIcon:     get("DisplayIcon"),
		Publisher:       get("Publisher"),
	}, nil
}

// WriteInstallRecord 写入（或覆盖）机器级安装记录。
func WriteInstallRecord(rec InstallRecord) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, installRegistryPath, registry.SET_VALUE|regView)
	if err != nil {
		return err
	}
	defer k.Close()

	if err := k.SetStringValue(valueInstallPath, rec.InstallPath); err != nil {
		return err
	}
	return k.SetStringValue(valueVersion, rec.DisplayVersion)
}

// WriteUninstallEntry 写入机器级的 Windows 标准卸载入口。
func WriteUninstallEntry(e UninstallEntry) error {
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, uninstallEntryPath, registry.SET_VALUE|regView)
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
	return clearKey(uninstallEntryPath)
}

// ClearInstallRecord 删除安装记录；项本就不存在时视为成功。
func ClearInstallRecord() error {
	return clearKey(installRegistryPath)
}

// clearKey 删除机器级与用户级两处的同名键。
// 两处都清是为了兜住早期版本写在 HKCU 的记录，避免新版本卸完还残留旧记录。
// 注意 registry.DeleteKey 走的是 RegDeleteKey，作用于调用进程所在的注册表视图，
// 无法附加 regView。本程序只编译 amd64（app_windows_amd64.syso），进程即 64 位，
// 视图与写入时固定的 64 位视图一致。
func clearKey(path string) error {
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		if err := registry.DeleteKey(root, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
	}
	return nil
}
