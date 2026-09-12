//go:build windows

package winapi

import "golang.org/x/sys/windows"

// NotifyError 用系统弹窗报错。静默模式下程序既没有控制台也没有界面，
// 出错必须让用户看得见，否则表现为"点了更新却什么都没发生"。
func NotifyError(title, message string) {
	messageBox(title, message, windows.MB_ICONERROR)
}

// NotifyInfo 用系统弹窗告知成功结果。用于"界面已经退出、但操作才刚完成"的
// 交接场景（例如交棒给临时副本执行的卸载），否则用户会以为点什么都没发生。
func NotifyInfo(title, message string) {
	messageBox(title, message, windows.MB_ICONINFORMATION)
}

func messageBox(title, message string, icon uint32) {
	text, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	caption, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, text, caption,
		windows.MB_OK|windows.MB_SETFOREGROUND|icon)
}
