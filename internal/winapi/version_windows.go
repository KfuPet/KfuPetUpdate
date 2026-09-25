//go:build windows

package winapi

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// version.dll 的三个入口。x/sys/windows 没有收录版本资源 API，这里按需自取。
var (
	versionDLL                  = windows.NewLazySystemDLL("version.dll")
	procGetFileVersionInfoSizeW = versionDLL.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW     = versionDLL.NewProc("GetFileVersionInfoW")
	procVerQueryValueW          = versionDLL.NewProc("VerQueryValueW")
)

// fixedFileInfoMinBytes 是 VS_FIXEDFILEINFO 里用到的部分：签名、结构版本、
// 文件版本 MS/LS 各 4 字节。
const fixedFileInfoMinBytes = 16

// FileVersion 读取 PE 文件资源里的文件版本（VERSIONINFO 的 FILEVERSION），
// 返回形如 "2.0.0.0" 的字符串；文件没有版本资源、或资源读取失败时返回空串。
//
// 取的是与语言无关的固定信息块，因此不受 StringFileInfo 里语言/代码页的影响——
// 清单里的 installer.version 也取自同一处资源，两边同源。
func FileVersion(path string) string {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}

	var handle uint32
	size, _, _ := procGetFileVersionInfoSizeW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&handle)),
	)
	if size == 0 {
		return ""
	}

	buf := make([]byte, size)
	ok, _, _ := procGetFileVersionInfoW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(size),
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if ok == 0 {
		return ""
	}

	// "\" 是固定信息块的固定写法，与语言无关。
	rootPtr, err := windows.UTF16PtrFromString(`\`)
	if err != nil {
		return ""
	}
	// 用 unsafe.Pointer 接 API 写回的指针（而不是 uintptr）：从 uintptr 转回指针
	// 会让 vet 的 unsafeptr 检查报"possible misuse of unsafe.Pointer"。
	var block unsafe.Pointer
	var blockLen uint32
	ok, _, _ = procVerQueryValueW.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&block)),
		uintptr(unsafe.Pointer(&blockLen)),
	)
	if ok == 0 || block == nil || blockLen < fixedFileInfoMinBytes {
		return ""
	}

	// block 指向 buf 内部，buf 在本函数存活期间不会被回收。
	raw := unsafe.Slice((*byte)(block), blockLen)
	ms := binary.LittleEndian.Uint32(raw[8:12])
	ls := binary.LittleEndian.Uint32(raw[12:16])
	return fmt.Sprintf("%d.%d.%d.%d", ms>>16, ms&0xffff, ls>>16, ls&0xffff)
}
