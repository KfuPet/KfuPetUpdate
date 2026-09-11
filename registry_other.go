//go:build !windows

package main

// 非 Windows 平台没有注册表，安装状态一律视为未安装。

func readInstallRecord() (*installRecord, error) { return nil, nil }

func clearInstallRecord() error { return nil }
