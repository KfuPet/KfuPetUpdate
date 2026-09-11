//go:build !windows

package main

// createShortcuts 在非 Windows 平台没有快捷方式的概念。
// 安装本身也会因无法写注册表而失败，这里仅保证跨平台可编译。
func createShortcuts(string, installOptions) error { return nil }
