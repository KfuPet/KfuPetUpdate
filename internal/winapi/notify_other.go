//go:build !windows

package winapi

import (
	"fmt"
	"os"
)

// NotifyError 非 Windows 平台写到标准错误，仅保证可编译。
func NotifyError(title, message string) {
	fmt.Fprintf(os.Stderr, "%s：%s\n", title, message)
}

// NotifyInfo 同上。
func NotifyInfo(title, message string) {
	fmt.Fprintf(os.Stdout, "%s：%s\n", title, message)
}
