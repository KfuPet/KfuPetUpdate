package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kfupet-installer/internal/version"
	"kfupet-installer/internal/winapi"
)

// installerAssetName 是发布版里安装器附件的缺省名。
// 清单里的 installer.name 优先，缺名时用它兜底（GitHub 与 Gitee 两处同名）。
const installerAssetName = "KfuPetInstall.exe"

// updaterOwnVersion 返回本程序自身的版本，读的是 exe 资源里的版本信息
// （app.rc 的 FILEVERSION），读不到时返回空串。
//
// 用资源而不是代码里的常量：清单里的 installer.version 由 gen-manifest.ps1 取自
// 同一个资源，两处同源才不会因为忘同步常量而把版本判断做错。
func updaterOwnVersion() string {
	self, err := selfPath()
	if err != nil {
		return ""
	}
	return winapi.FileVersion(self)
}

// updaterVersionText 把版本号整理成展示文案：
// 资源里的 FILEVERSION 是四段（2.0.0.0），清单里记的是文件版本字符串（2.0.0），
// 去掉末尾的 0 段后两者写法一致，不会让同一版本看起来像两个。
func updaterVersionText(v string) string {
	parts := strings.Split(strings.TrimSpace(v), ".")
	for len(parts) > 1 && parts[len(parts)-1] == "0" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, ".")
}

// installerAssetFor 返回清单里安装器的附件名；清单没记名字时用缺省名。
func installerAssetFor(art *installerDigest) string {
	if art != nil && art.Name != "" {
		return art.Name
	}
	return installerAssetName
}

// installerCandidates 返回发布版里本安装器的候选直链：选定源在前，镜像源按源顺序追加。
func (r *releaseInfo) installerCandidates(name string) []artifact {
	if name == "" {
		name = installerAssetName
	}
	var out []artifact
	appendMatching := func(list []artifact) {
		for i := range list {
			if list[i].Name == name {
				out = append(out, list[i])
			}
		}
	}
	appendMatching(r.Artifacts)
	appendMatching(r.Mirrors)
	return out
}

// updaterPackageNeeded 报告是否要从发布版下载安装器。
//
// 先比哈希定"是不是同一份文件"（本机运行的这份与清单里那份逐字节相同时不需要，
// 常驻副本正是由它复制出来的），再比版本定方向：发布版比本机这份旧时也不动。
// 两步缺一不可——只比版本会漏掉"版本号没变、但重建过"的发布包（发布方不 bump
// app.rc 就会出现），只比哈希则会把本机更新的构建降级成发布版那一份。
// 版本读不到时按"不旧"处理：宁可多下一次，也不让更新通道失效。
func updaterPackageNeeded(man *releaseManifest) bool {
	if man == nil || man.Installer == nil || man.Installer.SHA256 == "" {
		return false
	}
	self, err := selfPath()
	if err != nil {
		return true
	}
	sha, err := fileSHA256(self)
	if err != nil {
		return true
	}
	if strings.EqualFold(sha, man.Installer.SHA256) {
		return false
	}
	// 与修复流程"宁可不动，不能倒退"同一条准则：本机跑的可能是更新的构建
	// （自己编的、或发布回滚），照哈希换会把常驻副本降级。
	if own := updaterOwnVersion(); own != "" && man.Installer.Version != "" {
		if version.Compare(man.Installer.Version, own) < 0 {
			return false
		}
	}
	return true
}

// fetchUpdaterPackage 判断发布版是否带了与本机不同的安装器，需要时下载到临时文件。
// 返回空路径表示无需更新：发布版没带安装器信息、本机这份正是发布版那一份，
// 或发布版没提供可用地址。返回的路径由调用方用完删除。
func fetchUpdaterPackage(ctx context.Context, rel *releaseInfo, man *releaseManifest, report progressFunc) (string, error) {
	if rel == nil || !updaterPackageNeeded(man) {
		return "", nil
	}
	art := man.Installer

	name := installerAssetFor(art)
	candidates := rel.installerCandidates(name)
	if len(candidates) == 0 {
		return "", fmt.Errorf("发布版 %s 未提供安装器 %s", rel.Version, name)
	}
	// 镜像源的接口不提供附件大小，用清单里的大小补上，进度条才走得动。
	if size := art.Size; size > 0 {
		for i := range candidates {
			if candidates[i].Size <= 0 {
				candidates[i].Size = size
			}
		}
	}

	path, err := downloadArtifact(ctx, candidates, kindUpdater, report)
	if err != nil {
		return "", err
	}
	got, err := fileSHA256(path)
	if err != nil {
		os.Remove(path)
		return "", err
	}
	if !strings.EqualFold(got, art.SHA256) {
		os.Remove(path)
		return "", errors.New("下载到的安装器与哈希清单不符")
	}
	return path, nil
}

// placeUpdater 把常驻更新程序放进安装目录：发布版带了更新的安装器时换成它，否则复制自身。
// 常驻副本是卸载入口与 KfuPet「立即更新」的唯一入口，必须保证它存在且可用，
// 因此除"复制自身也失败"外，一切问题都降级为警告返回，不阻断主流程。
func placeUpdater(ctx context.Context, rel *releaseInfo, man *releaseManifest, installDir string, report progressFunc) (string, error) {
	dst := filepath.Join(installDir, updaterName)
	reportStage(report, stageUpdatingUpdater)

	pkg, err := fetchUpdaterPackage(ctx, rel, man, report)
	if err != nil {
		// 拿不到新版就退回自身：至少保证常驻副本在位、能跑。
		warn := "更新程序未能更新到最新版：" + err.Error() + "。"
		if copyErr := copySelf(dst); copyErr != nil {
			return warn, fmt.Errorf("放置更新程序失败：%w", copyErr)
		}
		return warn, nil
	}
	if pkg == "" {
		if err := copySelf(dst); err != nil {
			return "", fmt.Errorf("放置更新程序失败：%w", err)
		}
		return "", nil
	}
	defer os.Remove(pkg)

	// 本程序正以常驻副本的身份运行时（用户直接运行安装目录里的 KfuPetUpdate.exe），
	// 目标就是自己的映像，写不进去；它已经在那个位置上了，留给别的实例去换。
	if self, err := selfPath(); err == nil && samePath(dst, self) {
		return "更新程序正在使用中，本次未能更新到最新版；下次升级或修复时会补上。", nil
	}
	if err := copyFile(pkg, dst); err != nil {
		return "", fmt.Errorf("放置更新程序失败：%w", err)
	}
	return "", nil
}
