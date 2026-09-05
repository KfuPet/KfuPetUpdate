package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

// updateChecker 依次尝试多个更新源（GitHub → 自建服务器）做容错。
type updateChecker struct {
	sources []updateSource
}

func newUpdateChecker() *updateChecker {
	return &updateChecker{
		sources: []updateSource{
			newGitHubSource(),
			newServerSource(),
		},
	}
}

// check 返回第一个可用的远端发布信息；所有更新源均不可用时返回错误。
func (c *updateChecker) check(ctx context.Context) (*releaseInfo, error) {
	var lastErr error
	for _, src := range c.sources {
		rel, err := src.fetchLatest(ctx)
		if err != nil {
			lastErr = err
			continue // 当前源失败，尝试下一个源
		}
		if rel == nil {
			lastErr = fmt.Errorf("更新源暂无可用数据")
			continue
		}
		return rel, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("暂无可用更新源")
	}
	return nil, lastErr
}
