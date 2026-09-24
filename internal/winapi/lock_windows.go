//go:build windows

// Package winapi 封装本程序用到的 Windows 系统能力：单实例锁与忙闲标记、
// 系统弹窗、进程启动与等待。
package winapi

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

// AcquireInstanceLock 获取单实例锁，最长等待 timeout。
// 同一时刻只允许一个 updater 实例运行：两个实例同时安装/卸载会争抢同一个
// 安装目录，轻则报错、重则留下删了一半的目录；此外常驻副本被自身占用时，
// 别的实例既替换不了也删不掉安装目录。
// 临时副本接力时原进程会先放锁，所以这里必须留一段等待来覆盖交棒的空档。
func AcquireInstanceLock(timeout time.Duration) error {
	name, err := windows.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		return err
	}
	h, err := windows.CreateMutex(nil, false, name)
	// 互斥体已存在（上一次实例创建、还没退干净）时 CreateMutex 会返回
	// ERROR_ALREADY_EXISTS，句柄仍然有效——那不是失败，正是"有别的实例在跑"的常态。
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return fmt.Errorf("创建互斥体失败：%w", err)
	}
	if h == 0 {
		return errors.New("创建互斥体失败")
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

// ReleaseInstanceLock 放掉单实例锁。没持有或重复调用都没有副作用。
// 交棒给临时副本前必须调用：副本要接着干活，得先拿得到这把锁。
func ReleaseInstanceLock() {
	if currentLock == nil {
		return
	}
	_ = windows.ReleaseMutex(currentLock.handle)
	_ = windows.CloseHandle(currentLock.handle)
	currentLock = nil
}

// busyEventName 是"本实例正在干活"的命名事件：置位表示正在安装/修复/卸载/升级，
// 复位表示只是个开着没干活的窗口。别的实例据此判断能不能直接结束本进程。
// 用事件而不是互斥体：互斥体归线程所有，而 goroutine 会在 OS 线程之间迁移，
// 收尾时未必轮到当初那个线程放锁，容易放出"没人持有"的假象。
// 手动复位：置位后一直有效，直到显式复位；进程一退，内核连对象一起收走，不留残迹。
const busyEventName = `Local\KfuPetUpdate-Busy`

// busyHandle 是本进程的忙事件句柄；为 0 表示还没创建过。
var busyHandle windows.Handle

// busyEvent 返回本进程的忙事件句柄，必要时创建。
// 事件已存在时 CreateEvent 会返回 ERROR_ALREADY_EXISTS，那不是失败。
func busyEvent() (windows.Handle, error) {
	if busyHandle != 0 {
		return busyHandle, nil
	}
	name, err := windows.UTF16PtrFromString(busyEventName)
	if err != nil {
		return 0, err
	}
	h, err := windows.CreateEvent(nil, 1 /*手动复位*/, 0 /*初始复位：空闲*/, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return 0, err
	}
	if h == 0 {
		return 0, errors.New("创建忙事件失败")
	}
	busyHandle = h
	return h, nil
}

// MarkBusy 声明本实例正在干活，别的实例不会来结束本进程。
func MarkBusy() {
	if h, err := busyEvent(); err == nil {
		_ = windows.SetEvent(h)
	}
}

// MarkIdle 撤回上面的声明，表示本实例已回到空闲（只是开着窗口）。
func MarkIdle() {
	if h, err := busyEvent(); err == nil {
		_ = windows.ResetEvent(h)
	}
}

// IsOtherInstanceIdle 判断占着单实例锁的另一个实例是否闲着（没在改动安装目录）：
// 事件不存在（对方还没创建）或处于复位状态都算闲着。
// 判断不出来时按"在忙"处理——宁可不结束它，也不能误杀一个正在安装的实例。
func IsOtherInstanceIdle() bool {
	name, err := windows.UTF16PtrFromString(busyEventName)
	if err != nil {
		return false
	}
	h, err := windows.CreateEvent(nil, 1, 0, name)
	if err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return false
	}
	if h == 0 {
		return false
	}
	defer windows.CloseHandle(h)

	event, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		return false
	}
	// 未置位 = 空闲；置位 = 正在干活。
	return event == uint32(windows.WAIT_TIMEOUT)
}
