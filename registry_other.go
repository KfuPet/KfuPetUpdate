//go:build !windows

package main

import "errors"

// 非 Windows 平台没有注册表，安装状态一律视为未安装。

func readInstallRecord() (*installRecord, error) { return nil, nil }

// writeInstallRecord 在非 Windows 平台无法持久化安装记录，直接报错，
// 避免出现"文件已落盘但安装信息丢失"的静默不一致。
func writeInstallRecord(installRecord) error {
	return errors.New("当前平台不支持安装 KfuPet")
}

func clearInstallRecord() error { return nil }
