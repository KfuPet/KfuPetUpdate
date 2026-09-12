//go:build !windows

package winreg

import "errors"

// 非 Windows 平台没有注册表，安装状态一律视为未安装。

func ReadInstallRecord() (*InstallRecord, error) { return nil, nil }

// WriteInstallRecord 在非 Windows 平台无法持久化安装记录，直接报错，
// 避免出现"文件已落盘但安装信息丢失"的静默不一致。
func WriteInstallRecord(InstallRecord) error {
	return errors.New("当前平台不支持安装 KfuPet")
}

// 标准卸载入口同样依赖注册表，非 Windows 平台下为空实现。
func WriteUninstallEntry(UninstallEntry) error { return nil }

func ClearUninstallEntry() error { return nil }

func ClearInstallRecord() error { return nil }
