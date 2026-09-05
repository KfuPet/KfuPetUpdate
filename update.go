package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// 查询整个流程允许的最长时间（覆盖所有更新源）。
const checkTimeout = 30 * time.Second

type releaseInfo struct {
	Version        string    // 远端版本号
	ReleasePageURL string    // 发布页地址
	ReleaseNotes   string    // 更新说明
	PublishedAt    time.Time // 发布时间（UTC），未知时为零值
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
	return rel, nil
}

// serverSource 自建服务器更新源（空壳占位）。
type serverSource struct{}

func newServerSource() *serverSource { return &serverSource{} }

func (*serverSource) fetchLatest(context.Context) (*releaseInfo, error) {
	// 尚未接入服务器，返回空结果，由 updateChecker 尝试下一个源。
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

// check 返回第一个可用更新源的结果；
// GitHub 与自建服务器都失败时，返回包含各自失败原因的错误。
func (c *updateChecker) check(ctx context.Context) (*releaseInfo, error) {
	var fails []string
	for _, ns := range c.sources {
		rel, err := ns.source.fetchLatest(ctx)
		if err == nil && rel != nil {
			return rel, nil // 当前源成功，直接返回
		}
		if err != nil {
			fails = append(fails, fmt.Sprintf("%s源：%v", ns.name, err))
		} else {
			fails = append(fails, fmt.Sprintf("%s源：暂无可用发布数据", ns.name))
		}
	}

	if len(fails) == 0 {
		return nil, fmt.Errorf("暂无可用更新源")
	}
	return nil, fmt.Errorf("GitHub 与自建服务器均不可用（%s）", strings.Join(fails, "；"))
}
