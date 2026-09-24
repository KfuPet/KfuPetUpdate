package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
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
	Mirrors        []artifact // 其它源提供的同名产物，供下载失败时换源重试
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

// GitHub 源的超时收得比 Gitee 紧：国内直连 GitHub 常被阻断，干等下去只会拖慢回退到 Gitee 的速度。
// 代价是 GitHub 可达但慢于这个值时会被判为失败、改由 Gitee 兜底——镜像若落后于主源，
// 用户会被告知"已是最新"。
const gitHubTimeout = 3 * time.Second

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

// artifactsFor 返回当前平台安装包的全部候选直链：选定源在前，镜像源按源顺序追加。
// 下载层按这个顺序逐个尝试；与 artifactFor 的区别是它不会在第一个匹配处停下。
func (r *releaseInfo) artifactsFor() []artifact {
	suffix := assetSuffix()
	var out []artifact
	appendMatching := func(list []artifact) {
		for i := range list {
			if strings.HasSuffix(list[i].Name, suffix) {
				out = append(out, list[i])
			}
		}
	}
	appendMatching(r.Artifacts)
	appendMatching(r.Mirrors)
	return out
}

// manifestName 是随发布版一起上传的哈希清单文件名（由 gen-manifest.ps1 生成）。
const manifestName = "KfuPet-manifest.json"

// manifestTimeout 是取清单的时限。清单只有 1 KB 上下，重试也便宜，
// 因此给得比安装包下载短得多：拿不到就退回发布信息里能用的校验手段。
const manifestTimeout = 10 * time.Second

// manifestMaxBytes 是清单体积上限，防止把别的文件（HTML 错误页、压缩包等）整个读进内存。
const manifestMaxBytes = 256 << 10

// fileDigest 是清单中单个文件的哈希与大小。
type fileDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// releaseManifest 是发布版附带的哈希清单：压缩包自身的 sha256，
// 以及包内根级文件的逐个哈希（后者供「修复」比对）。
type releaseManifest struct {
	Version   string       `json:"version"`
	ZipSHA256 string       `json:"zipSha256"`
	Files     []fileDigest `json:"files"`
}

// manifestCandidates 返回哈希清单的候选直链：选定源在前，镜像源按源顺序追加。
func (r *releaseInfo) manifestCandidates() []artifact {
	var out []artifact
	appendMatching := func(list []artifact) {
		for i := range list {
			if list[i].Name == manifestName {
				out = append(out, list[i])
			}
		}
	}
	appendMatching(r.Artifacts)
	appendMatching(r.Mirrors)
	return out
}

// fetchManifest 取回本次发布版的哈希清单。
// 取不到（旧发布版没带清单，或所有源都不可达）时返回错误，
// 由调用方退回原有校验，不影响安装本身。
func fetchManifest(ctx context.Context, rel *releaseInfo) (*releaseManifest, error) {
	if rel == nil {
		return nil, errors.New("缺少发布信息，无法获取哈希清单")
	}
	candidates := rel.manifestCandidates()
	if len(candidates) == 0 {
		return nil, fmt.Errorf("发布版 %s 未附带 %s", rel.Version, manifestName)
	}

	var fails []string
	for _, art := range candidates {
		man, err := downloadManifest(ctx, art)
		if err == nil {
			// 清单版本必须与本次发布一致：镜像源若挂着旧清单，
			// 会把版本不符的包判成"校验通过"。
			if want := normalizeVersion(rel.Version); man.Version != want {
				err = fmt.Errorf("清单版本 %s 与发布版 %s 不符", man.Version, want)
			} else {
				return man, nil
			}
		}
		fails = append(fails, fmt.Sprintf("%s：%v", urlHost(art.DownloadURL), err))
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("获取哈希清单失败（%s）", strings.Join(fails, "；"))
}

// downloadManifest 从单个直链取回并解析清单。
// 选定源（GitHub）会给出清单附件的摘要，据此确认取回的字节没有被截断或篡改。
func downloadManifest(ctx context.Context, art artifact) (*releaseManifest, error) {
	ctx, cancel := context.WithTimeout(ctx, manifestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, art.DownloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "KfuPetUpdate-Updater")

	// 两个源的直链都会 302 到实际存储，需跟随重定向，因此用默认 client。
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, manifestMaxBytes))
	if err != nil {
		return nil, err
	}
	if want := sha256Hex(art.Digest); want != "" {
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != want {
			return nil, errors.New("清单内容与发布信息不符")
		}
	}

	var man releaseManifest
	if err := json.Unmarshal(body, &man); err != nil {
		return nil, fmt.Errorf("清单无法解析：%w", err)
	}
	if man.ZipSHA256 == "" {
		return nil, errors.New("清单中缺少压缩包哈希")
	}
	return &man, nil
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

// check 并发查询全部更新源，返回以第一个成功源为准的发布信息。
// 并发只为省掉串行等待：选定源仍由 sources 的顺序决定，与谁先返回无关。
// 其余源的同名产物收进 Mirrors，供下载层在某个源连不上时换源；
// 版本号与选定源不一致的源直接丢弃——镜像比主源旧时若照用它的直链，
// 会下到与所报版本不符的包。
func (c *updateChecker) check(ctx context.Context) (*releaseInfo, error) {
	// 每个 goroutine 只写自己那一位，无需加锁。
	type sourceResult struct {
		rel *releaseInfo
		err error
	}
	results := make([]sourceResult, len(c.sources))
	var wg sync.WaitGroup
	for i := range c.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i].rel, results[i].err = c.sources[i].source.fetchLatest(ctx)
		}()
	}
	wg.Wait()

	// 按源优先级归集结果，而不是按返回先后。
	var (
		primary *releaseInfo
		mirrors []artifact
		fails   []string
	)
	for i, ns := range c.sources {
		if results[i].err != nil {
			fails = append(fails, fmt.Sprintf("%s源：%v", ns.name, results[i].err))
			continue
		}
		if results[i].rel == nil {
			continue // 该源暂无数据，跳过
		}
		if primary == nil {
			primary = results[i].rel // 第一个成功的源就是选定源
			continue
		}
		if results[i].rel.Version == primary.Version {
			mirrors = append(mirrors, results[i].rel.Artifacts...)
		}
	}

	if primary == nil {
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
	primary.Mirrors = mirrors
	return primary, nil
}
