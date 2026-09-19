package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"kfupet-installer/internal/dotnet"
	"kfupet-installer/internal/winreg"
)

// installTimeout 单次安装允许的最长时间。
// 安装包为 70 MB 量级，远大于版本查询，因此不复用 checkTimeout。
const installTimeout = 30 * time.Minute

// installStage 描述安装流程所处的阶段，用于界面提示。
type installStage string

const (
	stageEnvDownloading installStage = "正在下载运行环境"
	stageEnvInstalling  installStage = "正在安装运行环境"
	stageDownloading    installStage = "正在下载安装包"
	stageVerifying      installStage = "正在校验安装包"
	stageExtracting     installStage = "正在解压安装包"
	stageApplying       installStage = "正在安装文件"
	stageShortcuts      installStage = "正在创建快捷方式"
	stageRegistering    installStage = "正在写入安装信息"
)

// installStages 是安装步骤的固定顺序：运行环境排在最前，装好 KfuPet 才能启动。
// 其中 stageShortcuts 只在勾选了快捷方式时才真正执行，见 stagesFor。
var installStages = []installStage{
	stageEnvDownloading, stageEnvInstalling,
	stageDownloading, stageVerifying, stageExtracting, stageApplying,
	stageShortcuts, stageRegistering,
}

// installOptions 是安装向导第二页收集的选项。
type installOptions struct {
	Desktop    bool   // 创建桌面快捷方式
	StartMenu  bool   // 创建开始菜单快捷方式
	Offline    bool   // 离线安装：使用本地已有的安装包，不下载
	Package    string // 离线安装包路径（Offline 为真时有效）
	InstallEnv bool   // 一并安装运行环境（缺少 .NET 桌面运行时时提供）
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
		// 离线安装直接用本地安装包，跳过下载阶段。
		if s == stageDownloading && opts.Offline {
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
}

type progressFunc func(installProgress)

// defaultInstallDir 返回向导预填的默认安装目录。
// 装到 %ProgramFiles% 需要管理员权限，而本程序以 requireAdministrator 运行
// （见 app.manifest），因此这里的写入不会因权限不足失败。
func defaultInstallDir() string {
	if base := os.Getenv("ProgramFiles"); base != "" {
		return filepath.Join(base, "KfuPet")
	}
	// 极少数取不到 ProgramFiles 的环境下退回用户目录。
	if base, err := os.UserCacheDir(); err == nil {
		return filepath.Join(base, "Programs", "KfuPet")
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

// validateInstallDir 校验用户选定的安装目录。
// 安装会把程序文件直接放进所选目录，因此必须排除磁盘（或网络共享）根目录，
// 否则几百个文件会散落在整块磁盘的根下。
func validateInstallDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("请先选择安装位置")
	}
	if filepath.Dir(dir) == dir {
		return errors.New("不能安装到磁盘根目录，请在根目录下新建一个文件夹后再选择")
	}
	return nil
}

// validateInstallOptions 校验向导第二页的选项：离线安装必须先选定安装包。
func validateInstallOptions(opts installOptions) error {
	if opts.Offline {
		return validatePackageFile(opts.Package)
	}
	return nil
}

// validatePackageFile 校验离线安装包路径是否可用。
func validatePackageFile(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("请先选择离线安装包")
	}
	info, err := os.Stat(path)
	if err != nil {
		return errors.New("所选离线安装包不存在或不可读")
	}
	if info.IsDir() {
		return errors.New("所选路径是文件夹，请选择安装包文件")
	}
	return nil
}

// installerSource 是本次安装使用的安装包文件来源。
type installerSource struct {
	path    string    // 安装包在磁盘上的路径
	art     *artifact // 用于校验的发布信息产物；离线且取不到发布信息时为 nil
	cleanup func()    // 用完后的清理动作（在线安装删临时文件；离线安装无操作）
}

// resolveInstallerSource 准备本次安装要用的安装包：
// 在线安装下载到临时文件（用完删除）；离线安装直接使用用户选定的本地文件。
func resolveInstallerSource(ctx context.Context, rel *releaseInfo, opts installOptions, report progressFunc) (installerSource, error) {
	if opts.Offline {
		if err := validatePackageFile(opts.Package); err != nil {
			return installerSource{}, err
		}
		// 能拿到发布信息时仍取其产物做 sha256 严格校验；断网取不到时留 nil，
		// 由 verifyArchive 退回最低限度校验。
		var art *artifact
		if rel != nil {
			if a, err := rel.artifactFor(); err == nil {
				art = a
			}
		}
		return installerSource{path: opts.Package, art: art, cleanup: func() {}}, nil
	}

	if rel == nil {
		return installerSource{}, errors.New("缺少发布信息，无法在线安装")
	}
	art, err := rel.artifactFor()
	if err != nil {
		return installerSource{}, err
	}
	path, err := downloadArtifact(ctx, art, report)
	if err != nil {
		return installerSource{}, err
	}
	return installerSource{path: path, art: art, cleanup: func() { os.Remove(path) }}, nil
}

// versionPattern 用于从安装包文件名中提取形如 1.2.3 的版本号。
var versionPattern = regexp.MustCompile(`\d+(?:\.\d+)+`)

// versionForInstall 返回本次安装要记录的版本号。
// 在线安装取自发布信息；离线安装若拿不到发布信息（断网），
// 退回从安装包文件名解析（如 KfuPet-v0.0.9-windows-amd64.zip），再不行记"未知"。
func versionForInstall(rel *releaseInfo, pkgPath string) string {
	if rel != nil && rel.Version != "" {
		return normalizeVersion(rel.Version)
	}
	if m := versionPattern.FindString(filepath.Base(pkgPath)); m != "" {
		return m
	}
	return "未知"
}

// ensureDesktopRuntime 下载并静默安装运行环境；本机已装好时直接返回。
// 下载地址全部失败时返回错误，由调用方决定跳过，不阻断主流程。
func ensureDesktopRuntime(ctx context.Context, report progressFunc) error {
	if dotnet.Detect().Present {
		return nil
	}

	path, err := downloadEnvInstaller(ctx, report)
	if err != nil {
		return err
	}
	defer os.Remove(path)

	// 备用地址是第三方文件站，失败时可能落地一个 HTML 错误页；执行前先确认是可执行文件。
	if !dotnet.LooksLikeExecutable(path) {
		return fmt.Errorf("下载到的运行环境安装包不可用")
	}

	reportStage(report, stageEnvInstalling)
	return dotnet.InstallSilent(path)
}

// downloadEnvInstaller 依次尝试各候选地址下载运行环境安装包，返回落地的临时文件路径。
func downloadEnvInstaller(ctx context.Context, report progressFunc) (string, error) {
	var fails []string
	for _, u := range dotnet.DownloadURLs {
		req := downloadRequest{url: u, suffix: ".exe", stage: stageEnvDownloading}
		path, err := downloadWithRetry(ctx, req, report)
		if err == nil {
			return path, nil
		}
		fails = append(fails, err.Error())
		if ctx.Err() != nil {
			break
		}
	}
	return "", fmt.Errorf("运行环境下载失败：%s", strings.Join(fails, "；"))
}

// installResult 是一次安装的产物。
type installResult struct {
	state      installState // 安装后的状态
	envSkipped bool         // 运行环境未能自动装好（下载地址均失败），需引导用户手动安装
}

// installKfuPet 把发布版安装到 installDir（升级走同一套流程）：
// 安装运行环境 → 获取安装包（在线下载 / 离线取本地文件）→ 校验 → 解压 →
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
	if opts.InstallEnv {
		if err := ensureDesktopRuntime(ctx, report); err != nil {
			envSkipped = true
		}
	}

	// 取到本次要用的安装包：在线下载，或离线直接使用用户选定的本地文件。
	src, err := resolveInstallerSource(ctx, rel, opts, report)
	if err != nil {
		return installResult{}, err
	}
	defer src.cleanup()

	if err := verifyArchive(src.path, src.art, report); err != nil {
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

	// 把自身复制进安装目录常驻：卸载入口与 KfuPet 的「检查更新」都依赖这个
	// 固定位置，用户删掉当初下载的 updater 也不影响后续卸载与升级。
	if err := copySelf(filepath.Join(installDir, updaterName)); err != nil {
		return installResult{}, fmt.Errorf("放置更新程序失败：%w", err)
	}

	if opts.wantsShortcuts() {
		reportStage(report, stageShortcuts)
		if err := createShortcuts(installDir, opts); err != nil {
			return installResult{}, err
		}
	}

	reportStage(report, stageRegistering)
	rec := winreg.InstallRecord{InstallPath: installDir, DisplayVersion: versionForInstall(rel, src.path)}

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
)

// downloadRequest 描述一次下载：地址、完整性预期、临时文件后缀与所属阶段。
type downloadRequest struct {
	url      string       // 下载地址
	expected int64        // 预期字节数；>0 时校验下载完整性，0 表示未知
	suffix   string       // 临时文件后缀（如 ".zip" / ".exe"）
	stage    installStage // 汇报进度时使用的阶段
}

// downloadArtifact 把安装包下载到临时文件，返回其路径。
func downloadArtifact(ctx context.Context, art *artifact, report progressFunc) (string, error) {
	return downloadWithRetry(ctx, downloadRequest{
		url:      art.DownloadURL,
		expected: art.Size,
		suffix:   ".zip",
		stage:    stageDownloading,
	}, report)
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

	resp, err := (&http.Client{Timeout: installTimeout}).Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("下载安装包失败：%w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载安装包失败：HTTP %d", resp.StatusCode)
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
// 有发布信息时：GitHub 给出 sha256 摘要就比对，没有摘要则退回校验文件大小；
// 离线安装拿不到发布信息时（art 为 nil），退回最低限度校验——确认是可打开的 zip。
func verifyArchive(path string, art *artifact, report progressFunc) error {
	reportStage(report, stageVerifying)

	if art != nil {
		if want := sha256Hex(art.Digest); want != "" {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, copyErr := io.Copy(h, f)
			f.Close()
			if copyErr != nil {
				return copyErr
			}
			if got := hex.EncodeToString(h.Sum(nil)); got != want {
				return fmt.Errorf("安装包校验失败：sha256 与发布信息不符")
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
