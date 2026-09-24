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

// 国内镜像仓库：https://gitee.com/lrht/kfu-pet
// 该仓库只同步发行版附件（不含源码），用于 GitHub 不可达时兜底。
const (
	giteeOwner = "lrht"
	giteeRepo  = "kfu-pet"
)

// GitHub 源的超时收得比 Gitee 紧：国内直连 GitHub 常被阻断，
// 干等下去只会拖慢回退到 Gitee 的速度；链路正常时这个值足够完成握手与接口响应。
const gitHubTimeout = 5 * time.Second

// gitHubSource 从 GitHub Releases 获取最新版本信息。
type gitHubSource struct {
	client *http.Client
}

func newGitHubSource() *gitHubSource {
	return &gitHubSource{
		client: &http.Client{Timeout: gitHubTimeout},
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

// giteeSource 从 Gitee Releases 获取最新版本信息。
// Gitee 的 v5 接口读取公开仓库无需鉴权，字段与 GitHub 大体一致，
// 差异有三处：发布页地址需自行拼接、发布时间字段是 created_at、
// 附件不提供 size 与 digest（下载侧对两者为 0/空都有兜底）。
type giteeSource struct {
	client *http.Client
}

func newGiteeSource() *giteeSource {
	return &giteeSource{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *giteeSource) fetchLatest(ctx context.Context) (*releaseInfo, error) {
	apiURL := fmt.Sprintf("https://gitee.com/api/v5/repos/%s/%s/releases/latest",
		giteeOwner, giteeRepo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "KfuPetUpdate-Updater")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gitee API 返回状态码 %d", resp.StatusCode)
	}

	var data struct {
		TagName   string `json:"tag_name"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
		Assets    []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if data.TagName == "" {
		return nil, fmt.Errorf("Gitee 响应中缺少 tag_name")
	}

	rel := &releaseInfo{
		Version: data.TagName,
		// Gitee 的发布版对象不含 html_url，按 tag 拼发布页地址。
		ReleasePageURL: fmt.Sprintf("https://gitee.com/%s/%s/releases/tag/%s",
			giteeOwner, giteeRepo, data.TagName),
		ReleaseNotes: data.Body,
	}
	if t, err := time.Parse(time.RFC3339, data.CreatedAt); err == nil {
		rel.PublishedAt = t
	}
	for _, a := range data.Assets {
		if a.Name == "" || a.BrowserDownloadURL == "" {
			continue
		}
		rel.Artifacts = append(rel.Artifacts, artifact{
			Name:        a.Name,
			DownloadURL: a.BrowserDownloadURL,
		})
	}
	return rel, nil
}

// namedSource 是带名称的更新源，失败时用于输出可读的错误信息。
type namedSource struct {
	name   string
	source updateSource
}

// updateChecker 依次尝试多个更新源：GitHub 优先，失败时回退到 Gitee 国内镜像。
type updateChecker struct {
	sources []namedSource
}

func newUpdateChecker() *updateChecker {
	return &updateChecker{
		sources: []namedSource{
			{name: "GitHub", source: newGitHubSource()},
			{name: "Gitee", source: newGiteeSource()},
		},
	}
}

// check 返回第一个可用更新源的结果。
// 某个源返回 (nil, nil) 视为该源暂无数据，跳过继续尝试下一个；
// 所有源都失败时，返回包含各自失败原因的错误。
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
		// 只有一个源报错，直接透出它的失败原因（默认即网络类错误），不额外包裹文字。
		return nil, fmt.Errorf("%s", fails[0])
	default:
		return nil, fmt.Errorf("所有更新源均不可用（%s）", strings.Join(fails, "；"))
	}
}
