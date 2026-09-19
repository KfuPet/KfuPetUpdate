package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// 查询整个流程允许的最长时间（覆盖所有更新源）。
const checkTimeout = 30 * time.Second

// artifact 是发布版中的一个可下载产物（安装包等）。
type artifact struct {
	Name        string // 附件名
	DownloadURL string // 下载直链
	Size        int64  // 字节数
	Digest      string // GitHub 提供的摘要，形如 "sha256:..."，可能为空
}

type releaseInfo struct {
	Version        string     // 远端版本号
	ReleasePageURL string     // 发布页地址
	ReleaseNotes   string     // 更新说明
	PublishedAt    time.Time  // 发布时间（UTC），未知时为零值
	Artifacts      []artifact // 发布版中的可下载产物
}

// updateSource 更新源接口：获取远端最新发布信息。
type updateSource interface {
	fetchLatest(ctx context.Context) (*releaseInfo, error)
}

// 目标仓库：https://github.com/KfuPet/KfuPet
const (
	githubOwner = "KfuPet"
	githubRepo  = "KfuPet"
)

// gitHubSource 从 GitHub Releases 获取最新版本信息。
type gitHubSource struct {
	client *http.Client
}

func newGitHubSource() *gitHubSource {
	return &gitHubSource{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *gitHubSource) fetchLatest(ctx context.Context) (*releaseInfo, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest",
		githubOwner, githubRepo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	// GitHub API 要求携带 User-Agent，否则返回 403
	req.Header.Set("User-Agent", "KfuPetUpdate-Updater")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 返回状态码 %d", resp.StatusCode)
	}

	var data struct {
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
		Assets      []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Size               int64  `json:"size"`
			Digest             string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if data.TagName == "" {
		return nil, fmt.Errorf("GitHub 响应中缺少 tag_name")
	}

	rel := &releaseInfo{
		Version:        data.TagName,
		ReleasePageURL: data.HTMLURL,
		ReleaseNotes:   data.Body,
	}
	if t, err := time.Parse(time.RFC3339, data.PublishedAt); err == nil {
		rel.PublishedAt = t
	}
	for _, a := range data.Assets {
		if a.Name == "" || a.BrowserDownloadURL == "" {
			continue
		}
		rel.Artifacts = append(rel.Artifacts, artifact{
			Name:        a.Name,
			DownloadURL: a.BrowserDownloadURL,
			Size:        a.Size,
			Digest:      a.Digest,
		})
	}
	return rel, nil
}

// assetSuffix 返回当前平台安装包应有的文件名后缀，如 "-windows-amd64.zip"。
func assetSuffix() string {
	return fmt.Sprintf("-%s-%s.zip", runtime.GOOS, runtime.GOARCH)
}

// artifactFor 挑出当前平台对应的安装包。
// 必须按后缀匹配：GitHub 的 Release 除自上传附件外还会附带
// "Source code (zip)" 等源码包，用"第一个 zip"之类的宽泛规则会挑错。
func (r *releaseInfo) artifactFor() (*artifact, error) {
	suffix := assetSuffix()
	for i := range r.Artifacts {
		if strings.HasSuffix(r.Artifacts[i].Name, suffix) {
			return &r.Artifacts[i], nil
		}
	}
	return nil, fmt.Errorf("发布版 %s 未提供当前平台（%s-%s）的安装包",
		r.Version, runtime.GOOS, runtime.GOARCH)
}

// normalizeVersion 去掉版本号的 v 前缀，注册表中统一存不带前缀的形式。
func normalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

// serverSource 自建服务器更新源（空壳占位）。
type serverSource struct{}

func newServerSource() *serverSource { return &serverSource{} }

func (*serverSource) fetchLatest(context.Context) (*releaseInfo, error) {
	// 尚未接入服务器（空壳占位）：返回 (nil, nil)，表示跳过该源，不视为失败。
	// 真实接入后，网络/接口异常应返回具体 error，便于上层汇总提示。
	return nil, nil
}

// namedSource 是带名称的更新源，失败时用于输出可读的错误信息。
type namedSource struct {
	name   string
	source updateSource
}

// updateChecker 依次尝试多个更新源：GitHub 优先，失败时回退到自建服务器。
type updateChecker struct {
	sources []namedSource
}

func newUpdateChecker() *updateChecker {
	return &updateChecker{
		sources: []namedSource{
			{name: "GitHub", source: newGitHubSource()},
			{name: "自建服务器", source: newServerSource()},
		},
	}
}

// check 返回第一个可用更新源的结果。
// 只有空壳/占位性质的源（返回 nil 且无错误）会被跳过，不算失败；
// 所有真实源都失败时，返回包含各自失败原因的错误。
func (c *updateChecker) check(ctx context.Context) (*releaseInfo, error) {
	var fails []string
	for _, ns := range c.sources {
		rel, err := ns.source.fetchLatest(ctx)
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s源：%v", ns.name, err))
			continue
		}
		if rel != nil {
			return rel, nil // 当前源成功，直接返回
		}
	}

	switch len(fails) {
	case 0:
		return nil, fmt.Errorf("暂无可用更新源")
	case 1:
		// 当前自建服务器还是空壳，实际只有 GitHub 一个真实源，
		// 此时直接透出它的失败原因（默认即网络类错误）。
		return nil, fmt.Errorf("%s", fails[0])
	default:
		return nil, fmt.Errorf("所有更新源均不可用（%s）", strings.Join(fails, "；"))
	}
}
