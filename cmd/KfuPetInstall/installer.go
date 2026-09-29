package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"kfupet-installer/internal/dotnet"
	"kfupet-installer/internal/winreg"
)

// installTimeout 单次安装允许的最长时间。
// 安装包为 70 MB 量级，远大于版本查询，因此不复用 checkTimeout。
// 它兜底整次流程，不适合用来快速发现"地址不可用"，连接类超时见下方下载客户端。
const installTimeout = 30 * time.Minute

// 下载客户端的连接类超时：把"地址不可用"与"网速慢"区分开。
// 整体仍由 installTimeout 兜底，因此这里只约束建连、握手与等待响应头，
// 给得比整体短得多——地址写错时几十秒内就能报错，而不是干等到整体超时。
const (
	downloadDialTimeout           = 10 * time.Second
	downloadTLSHandshakeTimeout   = 10 * time.Second
	downloadResponseHeaderTimeout = 20 * time.Second
)

// downloadClient 是下载与探测共用的 HTTP 客户端。
// 关键点是把连接类超时压短：写错的地址若域名能解析、TCP 能连上却迟迟不回，
// 默认客户端会一直等到整体超时才失败；这里靠 ResponseHeaderTimeout 快速收场。
var downloadClient = &http.Client{
	Timeout: installTimeout,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   downloadDialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   downloadTLSHandshakeTimeout,
		ResponseHeaderTimeout: downloadResponseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

// installStage 描述安装流程所处的阶段，用于界面提示。
type installStage string

const (
	stageEnvDownloading  installStage = "正在下载运行环境"
	stageEnvInstalling   installStage = "正在安装运行环境"
	stageDownloading     installStage = "正在下载安装包"
	stageVerifying       installStage = "正在校验安装包"
	stageExtracting      installStage = "正在解压安装包"
	stageApplying        installStage = "正在安装文件"
	stageUpdatingUpdater installStage = "正在更新更新程序"
	stageShortcuts       installStage = "正在创建快捷方式"
	stageRegistering     installStage = "正在写入安装信息"
)

// installStages 是安装步骤的固定顺序：运行环境排在最前，装好 KfuPet 才能启动。
// 其中 stageShortcuts 只在勾选了快捷方式时才真正执行，见 stagesFor。
var installStages = []installStage{
	stageEnvDownloading, stageEnvInstalling,
	stageDownloading, stageVerifying, stageExtracting, stageApplying,
	stageUpdatingUpdater,
	stageShortcuts, stageRegistering,
}

// installOptions 是安装向导第二页收集的选项。
type installOptions struct {
	Desktop    bool // 创建桌面快捷方式
	StartMenu  bool // 创建开始菜单快捷方式
	InstallEnv bool // 一并安装运行环境（缺少 .NET 桌面运行时时提供）
}

// wantsShortcuts 表示安装后是否需要创建快捷方式。
func (o installOptions) wantsShortcuts() bool {
	return o.Desktop || o.StartMenu
}

// stagesFor 返回本次安装实际会执行的步骤序列，供界面展示进度。
func stagesFor(opts installOptions) []installStage {
	stages := make([]installStage, 0, len(installStages))
	for _, s := range installStages {
		if s == stageShortcuts && !opts.wantsShortcuts() {
			continue
		}
		// 不需要装运行环境时，跳过对应的两个阶段。
		if (s == stageEnvDownloading || s == stageEnvInstalling) && !opts.InstallEnv {
			continue
		}
		stages = append(stages, s)
	}
	return stages
}

// stageIndex 返回阶段在给定步骤序列中的序号；找不到时返回 0。
func stageIndex(stages []installStage, stage installStage) int {
	for i, s := range stages {
		if s == stage {
			return i
		}
	}
	return 0
}

// installProgress 是一次进度汇报。
type installProgress struct {
	Stage installStage // 当前阶段
	Done  int64        // 已完成字节（仅下载阶段有效）
	Total int64        // 总字节，0 表示未知
	Speed float64      // 下载速度（字节/秒），非下载阶段为 0
	// Failed 表示该阶段失败。流程可能继续（如运行环境失败只作降级处理），
	// 但界面必须把它画成失败，而不是随流程推进画成"已完成"的对勾。
	Failed bool
}

type progressFunc func(installProgress)

// appDirName 是安装目录在所选位置下使用的文件夹名。
// 默认位置与"选了磁盘根目录时的自动填充"共用这一个名字，两处才不会各起一个名字。
const appDirName = "KfuPet"

// defaultInstallDir 返回向导预填的默认安装目录。
// 装到 %ProgramFiles% 需要管理员权限，而本程序以 requireAdministrator 运行
// （见 app.manifest），因此这里的写入不会因权限不足失败。
func defaultInstallDir() string {
	if base := os.Getenv("ProgramFiles"); base != "" {
		return filepath.Join(base, appDirName)
	}
	// 极少数取不到 ProgramFiles 的环境下退回用户目录。
	if base, err := os.UserCacheDir(); err == nil {
		return filepath.Join(base, "Programs", appDirName)
	}
	return ""
}

// pickerStartDir 返回目录选择对话框的起始位置：
// 优先当前目标目录，其次其父目录；都不存在时返回空串。
// 目标目录通常尚未创建，因而实际会停在它的父目录上。
func pickerStartDir(target string) string {
	if isDir(target) {
		return target
	}
	if parent := filepath.Dir(target); isDir(parent) {
		return parent
	}
	return ""
}

// isDir 判断路径是否为已存在的目录。
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isVolumeRoot 判断路径是否就是文件系统根（如 F:\ 或 F:）。
// 判据是"去掉尾部分隔符后，除卷名外不剩任何内容"，而不是 filepath.Dir(dir) == dir ——
// 后者对 "F:\" 与 "F:" 给出不同结果，会漏判其中一种写法。
//
// 卷名为空时一律不算，这样也顺手挡住了光秃秃的盘符 "F"：它的 Dir 是 "."（而非它自己），
// 单看 Dir 会漏判，但它同样会把文件撒在整块盘根下。网络共享根（\\server\share）
// 也会被认出来，此处的调用方需按各自的方式处理。
func isVolumeRoot(dir string) bool {
	vol := filepath.VolumeName(dir)
	if vol == "" {
		return false
	}
	return strings.TrimRight(dir[len(vol):], `\/`) == ""
}

// resolveInstallDirTarget 把用户选定的位置整理成最终的安装目录。
// 直接选了磁盘根目录时自动在其下补一层 appDirName：安装会把文件直接放进所选目录，
// 落在整块磁盘根下既乱又容易与别的目录混在一起，与其报错让用户自己再选一次，
// 不如直接给出 <盘符>:\KfuPet 让他确认或再改。
func resolveInstallDirTarget(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" || !isVolumeRoot(dir) {
		return dir
	}
	return filepath.Join(dir, appDirName)
}

// validateInstallDir 校验用户选定的安装目录。
// 安装会把程序文件直接放进所选目录，因此必须排除磁盘（或网络共享）根目录，
// 否则几百个文件会散落在整块磁盘的根下。调用方通常已用 resolveInstallDirTarget
// 做过自动填充（选了盘符根会补成 <盘符>:\KfuPet），这里兜的是它填不出来的情况：
// 网络共享根，以及所选位置恰好就叫 KfuPet 的磁盘根。
func validateInstallDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("请先选择安装位置")
	}
	if filepath.Dir(dir) == dir {
		return errors.New("不能安装到磁盘根目录，请在根目录下新建一个文件夹后再选择")
	}
	return nil
}

// installerSource 是本次安装使用的安装包文件来源。
type installerSource struct {
	path     string           // 安装包在磁盘上的路径（临时文件）
	art      *artifact        // 用于校验的发布信息产物
	manifest *releaseManifest // 发布版附带的哈希清单；取不到时为 nil，退回发布信息校验
	cleanup  func()           // 用完后的清理动作（删除临时文件）
}

// resolveInstallerSource 准备本次安装要用的安装包：下载到临时文件，用完删除。
func resolveInstallerSource(ctx context.Context, rel *releaseInfo, report progressFunc) (installerSource, error) {
	if rel == nil {
		return installerSource{}, errors.New("缺少发布信息，无法安装")
	}

	art, err := rel.artifactFor()
	if err != nil {
		return installerSource{}, err
	}
	// art 只用于校验，固定取选定源的产物（它带 sha256 摘要）；
	// 下载则按候选列表逐个尝试，可能最终来自镜像源。
	path, err := downloadArtifact(ctx, rel.artifactsFor(), kindPackage, report)
	if err != nil {
		return installerSource{}, err
	}
	// 清单以选定源为优先。镜像源（Gitee）的接口不提供摘要，正是靠清单才能校验
	// 从它下到的包；取不到清单时退回原有的逐级降级校验。
	man, _ := fetchManifest(ctx, rel)
	return installerSource{path: path, art: art, manifest: man, cleanup: func() { os.Remove(path) }}, nil
}

// versionForInstall 返回本次安装要记录的版本号。
// 取自发布信息；缺失时记"未知"，不让安装因为一个展示用的版本号而失败。
func versionForInstall(rel *releaseInfo) string {
	if rel != nil && rel.Version != "" {
		return normalizeVersion(rel.Version)
	}
	return "未知"
}

// ensureDesktopRuntime 下载并静默安装运行环境；本机已装好时直接返回。
// 下载失败时返回错误，由调用方决定跳过，不阻断主流程。
// 失败时把对应阶段标注为失败，界面据此画红叉，不再随流程推进画成对勾。
func ensureDesktopRuntime(ctx context.Context, report progressFunc) error {
	if dotnet.Detect().Present {
		return nil
	}

	path, err := downloadEnvInstaller(ctx, report)
	if err != nil {
		reportFailed(report, stageEnvDownloading)
		return err
	}
	defer os.Remove(path)

	// 下载失败时可能落地一个 HTML 错误页；执行前先确认是可执行文件。
	if !dotnet.LooksLikeExecutable(path) {
		reportFailed(report, stageEnvDownloading)
		return fmt.Errorf("下载到的运行环境安装包不可用")
	}

	reportStage(report, stageEnvInstalling)
	if err := dotnet.InstallSilent(path); err != nil {
		reportFailed(report, stageEnvInstalling)
		return err
	}
	return nil
}

// downloadEnvInstaller 从微软官方构建站下载运行环境安装包，返回落地的临时文件路径。
func downloadEnvInstaller(ctx context.Context, report progressFunc) (string, error) {
	req := downloadRequest{url: dotnet.DownloadURL, suffix: ".exe", stage: stageEnvDownloading, label: "运行环境"}
	path, err := downloadWithRetry(ctx, req, report)
	if err != nil {
		return "", fmt.Errorf("运行环境下载失败：%w", err)
	}
	return path, nil
}

// installResult 是一次安装的产物。
type installResult struct {
	state      installState // 安装后的状态
	envSkipped bool         // 运行环境未能自动装好（下载地址均失败），需引导用户手动安装
	warnings   []string     // 降级处理的问题（如快捷方式重试后仍建不出来），供界面提示
}

// installKfuPet 把发布版安装到 installDir（升级走同一套流程）：
// 安装运行环境 → 下载安装包 → 校验 → 解压 →
// 替换安装目录 → 创建快捷方式 → 写入注册表。
// 过程中任何一步失败都不会留下半成品安装：新文件先落在暂存目录，
// 全部就绪后才整体替换，替换失败会回滚旧目录。
func installKfuPet(ctx context.Context, rel *releaseInfo, installDir string, opts installOptions, report progressFunc) (installResult, error) {
	// 正在运行时安装目录内的文件被占用，无法替换，先让用户退出。
	// （优雅关闭协议尚未实现，这里只做拦截。）
	if isExecutableBusy(filepath.Join(installDir, executableName)) {
		return installResult{}, fmt.Errorf("KfuPet 正在运行，请先退出后再试")
	}

	// 自身就住在目标目录里时，整体替换会连自己一起搬走/覆盖，先拦下。
	// 正常流程中调用方已交棒给临时副本，这里只是兜底。
	if isSelfWithin(installDir) {
		return installResult{}, fmt.Errorf("更新程序正运行于安装目录内，请更换安装位置")
	}

	// 运行环境排在最前：装好 KfuPet 才跑得起来。
	// 候选地址都拿不到时只记录待办、不阻断主流程，安装结束后由界面引导用户手动安装。
	envSkipped := false
	// 降级处理的提示：只影响界面最后多一句说明，不参与成功与否的判定。
	var warnings []string
	if opts.InstallEnv {
		if err := ensureDesktopRuntime(ctx, report); err != nil {
			envSkipped = true
		}
	}

	// 取到本次要用的安装包：下载到临时文件，校验后使用。
	src, err := resolveInstallerSource(ctx, rel, report)
	if err != nil {
		return installResult{}, err
	}
	defer src.cleanup()

	if err := verifyArchive(src.path, src.art, src.manifest, report); err != nil {
		return installResult{}, err
	}

	// 暂存目录与安装目录同级，保证最后的 rename 在同一卷内，可以整体替换。
	stagingDir := installDir + ".new"
	if err := os.RemoveAll(stagingDir); err != nil {
		return installResult{}, err
	}
	defer os.RemoveAll(stagingDir)

	reportStage(report, stageExtracting)
	if err := extractZip(src.path, stagingDir); err != nil {
		return installResult{}, err
	}

	// 包结构校验：剥掉顶层目录后，程序应直接位于暂存目录根下。
	if info, err := os.Stat(filepath.Join(stagingDir, executableName)); err != nil || info.IsDir() {
		return installResult{}, fmt.Errorf("安装包结构异常：根目录下未找到 %s", executableName)
	}

	reportStage(report, stageApplying)
	if err := applyStagedDir(stagingDir, installDir); err != nil {
		return installResult{}, err
	}

	// 放置常驻更新程序：卸载入口与 KfuPet 的「立即更新」都指向这个固定位置，
	// 用户删掉当初下载的 updater 也不影响后续卸载与升级。发布版带了更新的
	// 安装器时换成发布版这一份，否则复制自身；失败只降级为警告，
	// 不因为"顺带更新更新程序"没成而把整次安装判为失败。
	warn, err := placeUpdater(ctx, rel, src.manifest, installDir, report)
	if err != nil {
		return installResult{}, err
	}
	if warn != "" {
		warnings = append(warnings, warn)
	}

	if opts.wantsShortcuts() {
		reportStage(report, stageShortcuts)
		// 快捷方式只是入口，建不出来不影响已经装好的程序本体，也不该把整次安装判为失败：
		// createShortcuts 内部已重试过，仍失败就记一条提示继续装下去（可事后用「修复」补）。
		if err := createShortcuts(installDir, opts); err != nil {
			warnings = append(warnings, "快捷方式创建失败："+err.Error()+"。可用主界面的「修复」重试。")
		}
	}

	reportStage(report, stageRegistering)
	rec := winreg.InstallRecord{InstallPath: installDir, DisplayVersion: versionForInstall(rel)}

	// 先写标准卸载入口，再写自己的安装记录：后者是"已安装"的唯一依据，
	// 放在最后写，前面的失败就不会留下"记录已存在但安装未完成"的状态。
	if err := winreg.WriteUninstallEntry(uninstallEntryFor(installDir, rec.DisplayVersion)); err != nil {
		return installResult{}, fmt.Errorf("写入卸载入口失败：%w", err)
	}
	if err := winreg.WriteInstallRecord(rec); err != nil {
		return installResult{}, fmt.Errorf("写入安装信息失败：%w", err)
	}

	return installResult{
		state:      installState{Installed: true, Path: rec.InstallPath, Version: rec.DisplayVersion},
		envSkipped: envSkipped,
		warnings:   warnings,
	}, nil
}

// launchKfuPet 启动安装目录内的 KfuPet。
// 本进程以管理员身份运行，子进程会继承同样的权限（不会再弹 UAC）。
// 只负责拉起，不等它退出。
func launchKfuPet(installDir string) error {
	cmd := exec.Command(filepath.Join(installDir, executableName))
	cmd.Dir = installDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 KfuPet 失败：%w", err)
	}
	return nil
}

// reportStage 汇报一个不带字节进度的阶段。
func reportStage(report progressFunc, stage installStage) {
	if report != nil {
		report(installProgress{Stage: stage})
	}
}

// reportFailed 汇报某个阶段失败：流程会继续，但界面应把它画成失败而非对勾。
func reportFailed(report progressFunc, stage installStage) {
	if report != nil {
		report(installProgress{Stage: stage, Failed: true})
	}
}

// applyStagedDir 用暂存目录整体替换安装目录。
// 先把旧目录改名为备份，再把暂存目录改名到位；第二步失败时回滚备份，
// 避免出现"新文件只替换了一半"的状态。
func applyStagedDir(stagingDir, installDir string) error {
	backupDir := installDir + ".old"
	if err := os.RemoveAll(backupDir); err != nil {
		return err
	}

	hasOld := false
	if _, err := os.Stat(installDir); err == nil {
		if err := os.Rename(installDir, backupDir); err != nil {
			return fmt.Errorf("备份原有安装目录失败：%w", err)
		}
		hasOld = true
	}

	if err := os.Rename(stagingDir, installDir); err != nil {
		if hasOld {
			if rbErr := os.Rename(backupDir, installDir); rbErr != nil {
				return fmt.Errorf("替换安装目录失败，且回滚未成功（原有文件仍保留在 %s）：%w",
					backupDir, err)
			}
		}
		return fmt.Errorf("替换安装目录失败：%w", err)
	}

	if hasOld {
		// 备份一般能删掉；若因文件仍被占用而残留，属于可接受的少量垃圾。
		_ = os.RemoveAll(backupDir)
	}
	return nil
}

// isExecutableBusy 判断安装目录内的程序是否正被运行占用。
// Windows 上正在运行的 exe 映像不允许以写方式打开，据此判断。
// 文件不存在或本就打不开时按"未占用"处理，让后续步骤报出真实错误。
func isExecutableBusy(exePath string) bool {
	if _, err := os.Stat(exePath); err != nil {
		return false
	}
	f, err := os.OpenFile(exePath, os.O_WRONLY, 0)
	if err != nil {
		return true
	}
	f.Close()
	return false
}

// 下载重试策略。
const (
	// downloadAttempts 是下载最多尝试的次数。
	downloadAttempts = 3
	// downloadRetryDelay 是两次尝试之间的等待，给瞬时故障一点恢复时间。
	downloadRetryDelay = 2 * time.Second
	// probeTimeout 是单个候选直链的探测超时。探测只为尽快把连不上的源排到后面，
	// 所以给得比下载短得多；超时即视为不可达。
	probeTimeout = 5 * time.Second
)

// artifactKind 描述一类发布产物的下载特征：临时文件后缀、所属阶段与出错文案里的名字。
type artifactKind struct {
	suffix string       // 临时文件后缀（如 ".zip" / ".exe"）
	stage  installStage // 汇报进度时使用的阶段
	label  string       // 出错文案里的产物名（如「安装包」「更新程序」）
}

var (
	kindPackage = artifactKind{suffix: ".zip", stage: stageDownloading, label: "安装包"}
	kindUpdater = artifactKind{suffix: ".exe", stage: stageUpdatingUpdater, label: "更新程序"}
)

// downloadRequest 描述一次下载：地址、完整性预期、临时文件后缀、所属阶段与产物名。
type downloadRequest struct {
	url      string       // 下载地址
	expected int64        // 预期字节数；>0 时校验下载完整性，0 表示未知
	suffix   string       // 临时文件后缀（如 ".zip" / ".exe"）
	stage    installStage // 汇报进度时使用的阶段
	label    string       // 出错文案里的产物名
}

// requestFor 按产物类型组装一次下载请求。
func (k artifactKind) requestFor(art artifact) downloadRequest {
	return downloadRequest{
		url:      art.DownloadURL,
		expected: art.Size,
		suffix:   k.suffix,
		stage:    k.stage,
		label:    k.label,
	}
}

// describe 返回产物名，未指定时按安装包称之。
func (r downloadRequest) describe() string {
	if r.label != "" {
		return r.label
	}
	return "安装包"
}

// downloadArtifact 把发布产物下载到临时文件，返回其路径。
// candidates 是同一个产物的候选直链（选定源在前，镜像源在后）：
// 先探测把可达的提前，再按顺序下载，一个源彻底失败就换下一个。
func downloadArtifact(ctx context.Context, candidates []artifact, kind artifactKind, report progressFunc) (string, error) {
	if len(candidates) == 0 {
		return "", errors.New("发布版中没有可用的下载地址")
	}

	var (
		fails   []string
		lastErr error
	)
	for _, art := range probeOrder(ctx, candidates) {
		path, err := downloadWithRetry(ctx, kind.requestFor(art), report)
		if err == nil {
			return path, nil
		}
		lastErr = err
		// ctx 已结束（取消或超时）时换源没有意义，直接返回。
		if ctx.Err() != nil {
			return "", err
		}
		fails = append(fails, fmt.Sprintf("%s：%v", urlHost(art.DownloadURL), err))
	}

	if len(fails) == 1 {
		// 只有一个候选，保持原有错误文案不变。
		return "", lastErr
	}
	return "", fmt.Errorf("所有下载地址均失败（%s）", strings.Join(fails, "；"))
}

// probeOrder 并发探测所有候选，返回按可达性重排后的列表：通过的在前，失败的在后。
// 并发是为了把总耗时封顶在单次 probeTimeout，而不是候选数 × probeTimeout。
// 一个都没通过时按原顺序返回——探测失败可能只是对方不接受 HEAD，
// 不足以断定下载一定失败，因此探测只用来排序，不用来淘汰。
func probeOrder(ctx context.Context, candidates []artifact) []artifact {
	if len(candidates) < 2 {
		return candidates // 只有一个候选，探测没有意义
	}

	// 每个 goroutine 只写自己那一位，无需加锁。
	reachable := make([]bool, len(candidates))
	var wg sync.WaitGroup
	for i := range candidates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reachable[i] = probeCandidate(ctx, candidates[i].DownloadURL)
		}()
	}
	wg.Wait()

	ordered := make([]artifact, 0, len(candidates))
	for i := range candidates {
		if reachable[i] {
			ordered = append(ordered, candidates[i])
		}
	}
	for i := range candidates {
		if !reachable[i] {
			ordered = append(ordered, candidates[i])
		}
	}
	return ordered
}

// probeCandidate 用 HEAD 探测单个直链是否可达。
// 必须用 HEAD 而不是"只取开头几字节"的 GET：Gitee 的发行版直链忽略 Range 头，
// 带 Range 的请求会返回 200 并把整个安装包下发，那就等于白下一次。
func probeCandidate(ctx context.Context, rawURL string) bool {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "KfuPetUpdate-Updater")

	// 两个源的直链都会 302 到实际存储，需要跟随重定向，因此用下载客户端。
	resp, err := downloadClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// urlHost 取 URL 的主机名，用于在错误信息里指明是哪个源失败。
func urlHost(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return rawURL
}

// downloadWithRetry 按重试策略下载一次请求，返回落地的临时文件路径。
// 连接被重置、握手超时这类瞬时失败很常见，因此最多尝试 downloadAttempts 次。
func downloadWithRetry(ctx context.Context, req downloadRequest, report progressFunc) (string, error) {
	var err error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		var path string
		if path, err = downloadFromURL(ctx, req, report); err == nil {
			return path, nil
		}
		// ctx 已结束（取消或超时）时再试没有意义，直接返回最后一次的失败原因。
		if ctx.Err() != nil {
			return "", err
		}
		if attempt < downloadAttempts && !sleepContext(ctx, downloadRetryDelay) {
			return "", err
		}
	}
	return "", err
}

// downloadFromURL 从指定地址下载一次，返回落地的临时文件路径。
func downloadFromURL(ctx context.Context, req downloadRequest, report progressFunc) (string, error) {
	if report != nil {
		// 每次尝试都从 0 重新汇报，重试时进度条会重走一遍。
		report(installProgress{Stage: req.stage, Total: req.expected})
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.url, nil)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("User-Agent", "KfuPetUpdate-Updater")

	resp, err := downloadClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("下载%s失败：%w", req.describe(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载%s失败：HTTP %d", req.describe(), resp.StatusCode)
	}

	total := resp.ContentLength
	if total <= 0 {
		total = req.expected
	}

	f, err := os.CreateTemp("", "KfuPetUpdate-*"+req.suffix)
	if err != nil {
		return "", err
	}
	path := f.Name()

	if err := copyWithProgress(f, resp.Body, total, req, report); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// sleepContext 等待 d，返回 false 表示等待期间 ctx 已结束。
func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// 进度汇报的触发粒度：达到字节数或时间其一即汇报，
// 保证高速下载不过度刷新界面、低速下载也能持续更新速度与百分比。
const (
	progressBytes    = 1 << 20
	progressInterval = 200 * time.Millisecond
)

// copyWithProgress 把 src 完整写入 dst，并按字节数、速度回报进度。
// req.expected 大于 0 时校验最终字节数，避免网络中断被当成下载完成。
func copyWithProgress(dst io.Writer, src io.Reader, total int64, req downloadRequest, report progressFunc) error {
	buf := make([]byte, 64*1024)
	var written, lastReported int64
	var speed float64
	lastReport := time.Now()

	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if _, err := dst.Write(buf[:n]); err != nil {
				return err
			}
			written += int64(n)

			now := time.Now()
			if report != nil && (written-lastReported >= progressBytes || now.Sub(lastReport) >= progressInterval) {
				// 用本次间隔内的平均速度做平滑，避免读数剧烈跳动。
				if elapsed := now.Sub(lastReport).Seconds(); elapsed > 0 {
					instant := float64(written-lastReported) / elapsed
					if speed == 0 {
						speed = instant
					} else {
						speed = 0.6*speed + 0.4*instant
					}
				}
				lastReported, lastReport = written, now
				report(installProgress{
					Stage: req.stage, Done: written, Total: total, Speed: speed,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	if report != nil {
		report(installProgress{
			Stage: req.stage, Done: written, Total: total, Speed: speed,
		})
	}
	if req.expected > 0 && written != req.expected {
		return fmt.Errorf("下载不完整：预期 %d 字节，实际 %d 字节", req.expected, written)
	}
	return nil
}

// verifyArchive 校验安装包。
// 拿到哈希清单时以清单里的压缩包 sha256 为准——它不依赖发布接口是否给摘要，
// 因此从镜像源下到的包也能验证；其次是发布信息的 sha256 摘要，再次是文件大小；
// 三者都没有时（发布版没带清单、接口也不给摘要）退回最低限度校验——确认是可打开的 zip。
//
// art 是发布信息里选定源的产物，在线安装路径下必定非 nil；这里的 nil 判断只是
// 防御性写法，拿它当"发布信息可能缺失"的入口并不成立。
func verifyArchive(path string, art *artifact, man *releaseManifest, report progressFunc) error {
	reportStage(report, stageVerifying)

	if man != nil && man.ZipSHA256 != "" {
		// 选定源同时给出摘要时交叉核对：两者不一致说明清单与包并非同一次发布，
		// 与其照着一份对不上的清单放行，不如直接报错。
		if art != nil {
			if digest := sha256Hex(art.Digest); digest != "" && digest != man.ZipSHA256 {
				return errors.New("安装包校验失败：哈希清单与发布信息不一致")
			}
		}
		got, err := fileSHA256(path)
		if err != nil {
			return err
		}
		if got != man.ZipSHA256 {
			return errors.New("安装包校验失败：sha256 与哈希清单不符")
		}
		return nil
	}

	if art != nil {
		if want := sha256Hex(art.Digest); want != "" {
			got, err := fileSHA256(path)
			if err != nil {
				return err
			}
			if got != want {
				return errors.New("安装包校验失败：sha256 与发布信息不符")
			}
			return nil
		}

		if art.Size > 0 {
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			if info.Size() != art.Size {
				return fmt.Errorf("安装包校验失败：大小不符（预期 %d 字节，实际 %d 字节）",
					art.Size, info.Size())
			}
		}
	}

	// 没有摘要可依据时，至少确认文件是可打开的 zip，把"选错文件"挡在解压之前。
	return checkZipReadable(path)
}

// fileSHA256 计算文件的 sha256，返回小写十六进制串。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checkZipReadable 确认文件是可打开的 zip，供拿不到发布信息时做最低限度校验。
func checkZipReadable(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("安装包不可用：%w", err)
	}
	defer r.Close()
	if len(r.File) == 0 {
		return errors.New("安装包为空")
	}
	return nil
}

// sha256Hex 从 GitHub 的 digest 字段取出十六进制摘要。
// 形如 "sha256:abc..."；格式不符或为空时返回空串。
func sha256Hex(digest string) string {
	scheme, hexPart, ok := strings.Cut(digest, ":")
	if !ok || scheme != "sha256" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(hexPart))
}

// extractZip 把 zip 包解压到 dstDir，兼容两种打包结构：
//   - 扁平：条目直接位于包根；
//   - 带单一顶层目录：所有条目都位于同一个顶层目录之下（如全部在 KfuPet/ 内），
//     此时自动剥掉这层，使内容落到 dstDir 根下。
//
// 同时拒绝任何写到 dstDir 之外的条目（zip slip）。
func extractZip(zipPath, dstDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("打开安装包失败：%w", err)
	}
	defer r.Close()

	prefix := zipRootDir(r.File)

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}

	for _, f := range r.File {
		name := strings.TrimPrefix(f.Name, prefix)
		if name == "" {
			continue // 顶层目录自身
		}

		target := filepath.Join(dstDir, filepath.FromSlash(name))
		if !isWithinDir(dstDir, target) {
			return fmt.Errorf("安装包内路径越界：%s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractZipFile(f, target); err != nil {
			return err
		}
	}
	return nil
}

// zipRootDir 返回需要剥掉的顶层目录前缀（形如 "KfuPet/"）。
// 仅当所有条目都位于同一个顶层目录之下时才剥掉；只要有条目直接位于包根
// （说明包本身已是扁平结构），就返回空串。
// "."、".." 与空段不算合法的顶层目录，否则会把越界条目"洗白"进目标目录。
func zipRootDir(files []*zip.File) string {
	root := ""
	for _, f := range files {
		seg, _, ok := strings.Cut(f.Name, "/")
		if !ok || seg == "" || seg == "." || seg == ".." {
			return ""
		}
		if root == "" {
			root = seg
			continue
		}
		if seg != root {
			return ""
		}
	}
	if root == "" {
		return ""
	}
	return root + "/"
}

// isWithinDir 判断 target 是否位于 baseDir 之内。
func isWithinDir(baseDir, target string) bool {
	rel, err := filepath.Rel(baseDir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func extractZipFile(f *zip.File, target string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
