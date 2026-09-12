//go:build !windows

package main

import (
	"fmt"
	"os"
)

// notifyError 非 Windows 平台写到标准错误，仅保证可编译。
func notifyError(title, message string) {
	fmt.Fprintf(os.Stderr, "%s：%s\n", title, message)
}

// notifyInfo 同上。
func notifyInfo(title, message string) {
	fmt.Fprintf(os.Stdout, "%s：%s\n", title, message)
}
