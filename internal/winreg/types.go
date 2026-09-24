package winreg

// InstallRecord 是从注册表读到的原始安装记录。
type InstallRecord struct {
	InstallPath    string // 软件安装目录完整路径
	DisplayVersion string // 本地版本号，不带 v 前缀
}

// UninstallEntry 是写进 Windows 标准卸载入口的信息，
// 用于让「设置 → 应用和功能」能列出并卸载本程序。
type UninstallEntry struct {
	DisplayName     string // 显示名称
	DisplayVersion  string // 显示版本
	UninstallString string // 点「卸载」时执行的命令
	DisplayIcon     string // 图标来源
	Publisher       string // 发布者

	// EstimatedSize 是安装目录体积（单位 KB），「应用和功能」用它显示占用空间；
	// 为 0 表示算不出来，此时不写这一项。必须是 DWORD，写成字符串会被忽略。
	// 只写不读：它随目录内容变化，与安装信息是否完整无关，不参与修复时的逐项比对。
	EstimatedSize uint32
}
