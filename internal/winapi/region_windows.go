//go:build windows

package winapi

import "golang.org/x/sys/windows"

// 地理位置 API 同样没被 x/sys/windows 收录，按需自取。
var procGetUserGeoID = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserGeoID")

// GetUserGeoID 的约定值。
// 三个地区编号由 GetGeoInfoW 在本机实测确认（返回的正是对应地区名）。
const (
	geoClassNation     = 16  // GEOCLASS_NATION：取「国家或地区」，而非更大的地理区域
	geoIDMainlandChina = 45  // 中国大陆
	geoIDHongKong      = 104 // 香港
	geoIDMacao         = 151 // 澳门
)

// IsChinaRegion 报告系统「国家或地区」是否设为中国大陆、香港或澳门。
//
// 读的是本机设置：瞬时完成、不联网、不外发隐私。判定不了时（如
// GEOID_NOT_AVAILABLE）按"非国内"处理，调用方的行为因此退回原有源顺序，
// 不会因为读不到设置而出错。
func IsChinaRegion() bool {
	geoid, _, _ := procGetUserGeoID.Call(geoClassNation)
	// 返回值是 32 位的 GEOID，收窄成 int32 才能同时接住零扩展与符号扩展两种写法。
	switch int32(geoid) {
	case geoIDMainlandChina, geoIDHongKong, geoIDMacao:
		return true
	default:
		return false
	}
}
