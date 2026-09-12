//go:build windows

package main

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

// singleInstanceMutexName 是单实例互斥体名。
// 用 Local\ 前缀限定在当前登录会话内：安装记录与卸载入口都写 HKCU，
// 跨用户本来就不在我们的管理范围里，也就不必去抢全局命名空间。
const singleInstanceMutexName = `Local\KfuPetUpdate-Singleton`

// lockPollInterval 是抢锁时的轮询间隔。
const lockPollInterval = 200 * time.Millisecond

// currentLock 是本进程持有的单实例锁；未持有时为 nil。
var currentLock *instanceLock

// instanceLock 是进程级的单实例锁。
type instanceLock struct {
	handle windows.Handle
}

// acquireInstanceLock 获取单实例锁，最长等待 timeout。
// 同一时刻只允许一个 updater 实例运行：两个实例同时安装/卸载会争抢同一个
// 安装目录，轻则报错、重则留下删了一半的目录；此外常驻副本被自身占用时，
// 别的实例既替换不了也删不掉安装目录。
// 临时副本接力时原进程会先放锁，所以这里必须留一段等待来覆盖交棒的空档。
func acquireInstanceLock(timeout time.Duration) error {
	name, err := windows.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		return err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		return fmt.Errorf("创建互斥体失败：%w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		event, err := windows.WaitForSingleObject(h, uint32(lockPollInterval/time.Millisecond))
		if err != nil {
			windows.CloseHandle(h)
			return fmt.Errorf("等待互斥体失败：%w", err)
		}
		// WAIT_ABANDONED：上一个持有者崩溃退出，锁已归我们，同样算获取成功。
		if event == windows.WAIT_OBJECT_0 || event == windows.WAIT_ABANDONED {
			currentLock = &instanceLock{handle: h}
			return nil
		}
		if time.Now().After(deadline) {
			windows.CloseHandle(h)
			return errors.New("另一个 KfuPet 更新程序正在运行，请先关闭它再试")
		}
	}
}

// releaseInstanceLock 放掉单实例锁。没持有或重复调用都没有副作用。
// 交棒给临时副本前必须调用：副本要接着干活，得先拿得到这把锁。
func releaseInstanceLock() {
	if currentLock == nil {
		return
	}
	_ = windows.ReleaseMutex(currentLock.handle)
	_ = windows.CloseHandle(currentLock.handle)
	currentLock = nil
}
