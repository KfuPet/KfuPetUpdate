package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"kfupet-installer/internal/version"
	"kfupet-installer/internal/winreg"
)

// 修复流程特有的阶段。
const (
	stageRepairChecking installStage = "正在检查安装完整性"
	stageRepairCore     installStage = "正在修复程序文件"
	stageRepairLocal    installStage = "正在修复安装信息"
)

// coreFileNames 是随包发布、由安装器管理的根级文件。
// 只在一个地方用到：拿不到哈希清单（无网）时的存在性检查。
// 正常情况下以清单为准，清单才是发布方说"该有哪些文件"的地方。
var coreFileNames = []string{
	executableName, "KfuPet.dll", "KfuPet.deps.json", "KfuPet.runtimeconfig.json",
}

// repairStagesFor 返回本次修复的步骤清单。
// 只修本地项时没有下载/校验/解压这几步，步骤清单也就不该把它们列出来。
func repairStagesFor(needDownload bool) []installStage {
	if !needDownload {
		return []installStage{stageRepairChecking, stageRepairLocal}
	}
	return []installStage{
		stageRepairChecking, stageDownloading, stageVerifying,
		stageExtracting, stageRepairCore, stageRepairLocal,
	}
}

// repairPlan 是一次体检的结论，同时也是本次修复要做的事。
type repairPlan struct {
	installDir string
	localVer   string // 注册表里记录的本地版本（可能为空）
	version    string // 修复完成后应写入注册表的版本号

	coreFiles []string         // 需按发布包覆盖的根级文件（缺失或哈希不符）
	updater   bool             // 常驻副本需重放
	shortcuts []shortcutTarget // 需重建的快捷方式
	registry  []string         // 需重写的注册表项（展示名）

	// 以下几项只用于体检报告，不参与执行。
	coreVerified   bool     // 是否按哈希清单逐个校验过本体文件
	coreMissing    []string // 未能校验时，仅凭存在性发现的缺失文件
	coreSkipReason string   // 非空表示本体文件本次没能校验，值为原因（用于提示文案）

	rel *releaseInfo     // 发布信息；仅本地修复时为 nil
	man *releaseManifest // 哈希清单；仅本地修复时为 nil
}

// needsWork 表示体检发现了需要修复的问题。
func (p repairPlan) needsWork() bool {
	return p.needsDownload() || p.updater || len(p.shortcuts) > 0 || len(p.registry) > 0
}

// needsDownload 表示本次修复要下载安装包——只有本体文件需要覆盖时才下载。
func (p repairPlan) needsDownload() bool { return len(p.coreFiles) > 0 }

// needsKfuPetClosed 表示本次修复要覆盖 KfuPet 自己的程序文件，因此它必须先退出——
// 正在运行的 exe/dll 映像替换不掉，改名会失败。
// 重放常驻副本、重建快捷方式、改注册表都不涉及这些文件，不该因此拦下用户。
func (p repairPlan) needsKfuPetClosed() bool { return p.needsDownload() }

// summary 列出本次修复要做（或已做）的事，供界面展示。
func (p repairPlan) summary() []string {
	var lines []string
	if len(p.coreFiles) > 0 {
		lines = append(lines, fmt.Sprintf("覆盖 %d 个程序文件：%s",
			len(p.coreFiles), strings.Join(p.coreFiles, "、")))
	}
	if p.updater {
		lines = append(lines, "补回常驻更新程序 "+updaterName)
	}
	for _, t := range p.shortcuts {
		lines = append(lines, "重建"+t.label)
	}
	for _, name := range p.registry {
		lines = append(lines, "重写"+name)
	}
	if p.versionChanged() {
		lines = append(lines, fmt.Sprintf("版本号 %s → %s", versionText(p.localVer), versionText(p.version)))
	}
	return lines
}

// versionChanged 表示修复会改变记录的版本号（修复按线上最新版进行）。
func (p repairPlan) versionChanged() bool {
	return p.version != "" && p.version != normalizeVersion(p.localVer)
}

// versionText 把版本号整理成展示文案；空值说明未知。
func versionText(v string) string {
	v = normalizeVersion(v)
	if v == "" {
		return "未知"
	}
	return "v" + v
}

// downloadSize 返回本次修复预估要下载的字节数；未知时返回 0。
func (p repairPlan) downloadSize() int64 {
	if !p.needsDownload() || p.rel == nil {
		return 0
	}
	art, err := p.rel.artifactFor()
	if err != nil {
		return 0
	}
	return art.Size
}

// inspectInstall 体检：列出安装目录与安装信息里需要修复的项。
//
// 本体文件按发布版附带的哈希清单逐个核对——这是唯一能发现"文件在但内容坏了"
// 的手段；取不到清单时（无网、或旧发布版没带清单）退回存在性检查，且不覆盖文件：
// 没有可信来源时硬写，可能把好文件写坏。
func inspectInstall(ctx context.Context, checker *updateChecker, installDir, localVer string, report progressFunc) repairPlan {
	p := repairPlan{
		installDir: installDir,
		localVer:   localVer,
		version:    normalizeVersion(localVer),
	}
	reportStage(report, stageRepairChecking)

	// 这两项不依赖网络，任何情况下都能修。
	inspectUpdater(&p)
	inspectShortcuts(&p)

	// 版本号要在查注册表之前定下来：注册表里的版本号也跟着修复走，
	// 否则桌宠那边会一直提示"有新版本"。
	man := fetchRepairManifest(ctx, checker, &p)
	inspectRegistry(&p)

	if man != nil {
		p.man = man
		p.coreVerified = true
		inspectCoreFiles(&p)
		return p
	}
	// 清单拿不到：只能看文件在不在，结果仅用于提示，不产生修复动作。
	for _, name := range coreFileNames {
		if _, err := os.Stat(filepath.Join(installDir, name)); err != nil {
			p.coreMissing = append(p.coreMissing, name)
		}
	}
	return p
}

// fetchRepairManifest 取发布信息与哈希清单，成功时顺带把修复的目标版本号定下来。
// 拿不到时把原因写进 p.coreSkipReason，供界面如实说明"程序文件本次没能校验"。
func fetchRepairManifest(ctx context.Context, checker *updateChecker, p *repairPlan) *releaseManifest {
	rel, err := checker.check(ctx)
	if err != nil || rel == nil {
		if err == nil {
			err = errors.New("未取到发布信息")
		}
		p.coreSkipReason = "未能获取线上版本信息（" + err.Error() + "）"
		return nil
	}

	// 线上版本比本地还旧（镜像滞后、或本机装的是尚未发布的构建）时不能按它校验：
	// 照做会把用户的文件降级成旧版。与升级路径同样是"宁可不动，不能倒退"。
	if version.Compare(p.localVer, rel.Version) > 0 {
		p.coreSkipReason = fmt.Sprintf("线上最新版本 %s 低于本机 %s，按它校验会把文件降级",
			versionText(rel.Version), versionText(p.localVer))
		return nil
	}

	man, err := fetchManifest(ctx, rel)
	if err != nil {
		p.coreSkipReason = "未能获取哈希清单（" + err.Error() + "）"
		return nil
	}
	p.rel = rel
	p.version = normalizeVersion(rel.Version)
	return man
}

// inspectUpdater 检查安装目录内的常驻副本。
// 只比大小：它应当与当前运行的这个程序逐字节相同（就是 copySelf 复制出来的），
// 大小不符说明是别的版本、或被杀软截断（0 字节），两种都要重放。
func inspectUpdater(p *repairPlan) {
	path := filepath.Join(p.installDir, updaterName)
	self, err := selfPath()
	if err != nil {
		return
	}
	if samePath(path, self) {
		return // 本程序就运行在这个位置，它显然在
	}
	selfInfo, err := os.Stat(self)
	if err != nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() != selfInfo.Size() {
		p.updater = true
	}
}

// inspectShortcuts 检查各安装落点的快捷方式是否都在、且指向本安装目录。
func inspectShortcuts(p *repairPlan) {
	states, err := shortcutStates(p.installDir)
	if err != nil {
		// 查不出来（PowerShell 不可用等）时不当成"要修"：宁可漏修，
		// 也不要在无法确认的情况下反复重建快捷方式。
		return
	}
	for _, t := range installShortcutTargets {
		if states[t.folder] != shortcutOK {
			p.shortcuts = append(p.shortcuts, t)
		}
	}
}

// inspectRegistry 检查安装信息是否齐全。
// 必须查机器级：修复的目标就是把记录补到 HKLM，只写在 HKCU 的早期记录同样算"要修"。
func inspectRegistry(p *repairPlan) {
	rec, err := winreg.ReadMachineInstallRecord()
	if err != nil || rec == nil ||
		!samePath(rec.InstallPath, p.installDir) ||
		normalizeVersion(rec.DisplayVersion) != p.version {
		p.registry = append(p.registry, "安装记录")
	}

	entry, err := winreg.ReadUninstallEntry()
	if err != nil || entry == nil {
		p.registry = append(p.registry, "卸载入口")
		return
	}
	want := uninstallEntryFor(p.installDir, p.version)
	if entry.DisplayName != want.DisplayName ||
		entry.UninstallString != want.UninstallString ||
		entry.DisplayIcon != want.DisplayIcon ||
		normalizeVersion(entry.DisplayVersion) != p.version {
		p.registry = append(p.registry, "卸载入口")
	}
}

// inspectCoreFiles 按清单逐个核对根级文件：缺失或哈希不符都要覆盖。
func inspectCoreFiles(p *repairPlan) {
	for _, f := range p.man.Files {
		if !isRootFileName(f.Path) {
			continue // 只认根级文件；清单本就不含子目录内容
		}
		if isFileIntact(filepath.Join(p.installDir, filepath.FromSlash(f.Path)), f.SHA256) {
			continue
		}
		p.coreFiles = append(p.coreFiles, f.Path)
	}
}

// isRootFileName 判断清单里的路径是否是安全的根级文件名。
// 清单来自网络，带分隔符或 ".." 的路径一律不认，避免被写到安装目录之外。
func isRootFileName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		!strings.ContainsAny(name, `/\`)
}

// isFileIntact 判断文件存在且内容与预期一致；预期哈希为空时只查存在性。
func isFileIntact(path, wantSHA256 string) bool {
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return false
	}
	if wantSHA256 == "" {
		return true
	}
	got, err := fileSHA256(path)
	return err == nil && strings.EqualFold(got, wantSHA256)
}

// repairResult 是一次修复的产物。
type repairResult struct {
	plan     repairPlan // 实际执行过的修复计划
	warnings []string   // 降级处理的问题（如快捷方式重试后仍重建不出来），供界面提示
}

// repairKfuPet 执行体检结论：缺什么补什么。
// 只覆盖校验不通过的文件，安装目录里的其它内容（如 Characters 角色模型）一律不动。
func repairKfuPet(ctx context.Context, p repairPlan, report progressFunc) (repairResult, error) {
	res := repairResult{plan: p}

	if p.needsDownload() {
		if err := repairCoreFiles(ctx, &p, report); err != nil {
			return res, err
		}
	}

	reportStage(report, stageRepairLocal)
	warnings, err := repairLocalParts(&p)
	res.warnings = warnings
	if err != nil {
		return res, err
	}
	res.plan = p
	return res, nil
}

// repairCoreFiles 下载发布包，把校验不通过的文件就地覆盖。
// 先解压到临时暂存目录并逐个确认哈希，再覆盖安装目录——包本身坏了、
// 或清单与包不是同一次发布时，宁可整体失败，也不要把坏文件写进安装目录。
func repairCoreFiles(ctx context.Context, p *repairPlan, report progressFunc) error {
	if p.rel == nil {
		return errors.New("缺少发布信息，无法修复程序文件")
	}
	art, err := p.rel.artifactFor()
	if err != nil {
		return err
	}

	path, err := downloadArtifact(ctx, p.rel.artifactsFor(), report)
	if err != nil {
		return err
	}
	defer os.Remove(path)

	if err := verifyArchive(path, art, p.man, report); err != nil {
		return err
	}

	reportStage(report, stageExtracting)
	staging, err := os.MkdirTemp("", tempDirPrefix+"repair-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := extractZip(path, staging); err != nil {
		return err
	}

	reportStage(report, stageRepairCore)
	want := make(map[string]string, len(p.man.Files))
	for _, f := range p.man.Files {
		want[f.Path] = f.SHA256
	}
	for _, name := range p.coreFiles {
		src := filepath.Join(staging, filepath.FromSlash(name))
		// 暂存里的文件必须先自证无误，才允许拿去覆盖安装目录。
		if !isFileIntact(src, want[name]) {
			return fmt.Errorf("发布包中的 %s 与哈希清单不符，已放弃修复", name)
		}
		if err := replaceFile(src, filepath.Join(p.installDir, filepath.FromSlash(name))); err != nil {
			return fmt.Errorf("修复 %s 失败：%w", name, err)
		}
	}
	return nil
}

// repairLocalParts 补回常驻副本、快捷方式与安装信息，返回被降级为警告的问题。
// 快捷方式重建失败不该连累安装信息——后者才是修复的主项，因此重试后仍失败时
// 记一条警告继续；同时把失败的落点从 p.shortcuts 里摘掉，完成页才不会宣称"已重建"。
func repairLocalParts(p *repairPlan) ([]string, error) {
	var warnings []string

	if p.updater {
		if err := copySelf(filepath.Join(p.installDir, updaterName)); err != nil {
			return warnings, fmt.Errorf("放置更新程序失败：%w", err)
		}
	}

	if len(p.shortcuts) > 0 {
		if err := createShortcutsAt(p.installDir, p.shortcuts); err != nil {
			warnings = append(warnings, "快捷方式重建失败："+err.Error()+"。")
			p.shortcuts = nil
		}
	}

	if len(p.registry) > 0 {
		// 与安装流程同一顺序：先写卸载入口，再写安装记录。后者是"已安装"的
		// 唯一依据，放最后写，前面的失败就不会留下"记录在、但安装未完成"的状态。
		if err := winreg.WriteUninstallEntry(uninstallEntryFor(p.installDir, p.version)); err != nil {
			return warnings, fmt.Errorf("写入卸载入口失败：%w", err)
		}
		rec := winreg.InstallRecord{InstallPath: p.installDir, DisplayVersion: p.version}
		if err := winreg.WriteInstallRecord(rec); err != nil {
			return warnings, fmt.Errorf("写入安装信息失败：%w", err)
		}
	}
	return warnings, nil
}

// replaceFile 用 src 的内容覆盖 dst。
// 先在同目录下写临时文件再改名：同卷改名是原子操作，不会留下写了一半的文件。
func replaceFile(src, dst string) error {
	dir := filepath.Dir(dst)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(dst)+".repair-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// 改名成功后这里什么都删不到，是正常情况。
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if _, err := io.Copy(tmp, in); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}
