// Package updater 查询 GitHub 最新稳定版并和当前版本比较。
//
// 边界（技术规划 §7）：只读 release 元数据，不自动下载、不自动安装；
// 下载和安装属于安全更新流程，那时才谈签名校验。找不到稳定版、限流、
// 网络失败、数据不合法都是明确的结果或错误，绝不把检查失败说成「已是最新」。
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 生产查询地址（技术规划 §7 固定这一条）；测试用 httptest 换掉。
const ReleasesAPI = "https://api.github.com/repos/xiaokentrl/github-net/releases/latest"

// 检查是三次请求里最轻的一种：8 秒拿不到就当网络失败，不让用户盯着转圈。
const checkTimeout = 8 * time.Second

// 四类失败，前端按码分支、给不同的话：
//
//	ErrNoStableRelease —— 仓库还没有任何正式发布：如实说，不算错误页。
//	ErrRateLimited     —— GitHub 匿名限流（每小时 60 次）：说明原因，建议稍后再试。
//	ErrNetwork         —— 超时、断网、DNS 失败等传输层问题。
//	ErrBadData         —— 应答不是预期形状（tag 不合法、JSON 坏）。
var (
	ErrNoStableRelease = errors.New("还没有已发布的稳定版")
	ErrRateLimited     = errors.New("GitHub 限流了这次检查")
	ErrNetwork         = errors.New("网络检查失败")
	ErrBadData         = errors.New("GitHub 返回的数据不认识")
)

// Info 是一次检查的结论。LatestVersion 只要查到就填（「已是最新」时界面要能显示最新是多少）；
// 「没查到」不走这里——那是 ErrNoStableRelease，界面上是另一种话。
// Assets 是这次发布附的文件清单（名字、直链、字节大小），给「下载并安装」用：
// 界面在下载前把来源和大小亮给用户（技术规划 §7 原话），数据从这里来。
type Info struct {
	CurrentVersion  string        `json:"currentVersion"`
	LatestVersion   string        `json:"latestVersion,omitempty"`
	UpdateAvailable bool          `json:"updateAvailable"`
	ReleaseURL      string        `json:"releaseUrl,omitempty"`
	PublishedAt     string        `json:"publishedAt,omitempty"`
	CheckedAt       string        `json:"checkedAt"`
	Assets          []UpdateAsset `json:"assets,omitempty"`
}

// UpdateAsset 是发布附的一个文件。
type UpdateAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

// Client 查 GitHub releases。baseURL 可注入（测试）；生产用 ReleasesAPI。
type Client struct {
	baseURL string
	http    *http.Client
	now     func() time.Time
}

func NewClient() *Client {
	return &Client{
		baseURL: ReleasesAPI,
		http:    &http.Client{Timeout: checkTimeout},
		now:     time.Now,
	}
}

// SetBaseURL 只给测试用：把查询地址指到假服务器；生产代码一律 NewClient 的默认值。
func (c *Client) SetBaseURL(url string) { c.baseURL = url }

// release 是 GitHub releases/latest 应答里本包关心的几个字段。
type release struct {
	TagName     string         `json:"tag_name"`
	HTMLURL     string         `json:"html_url"`
	PublishedAt string         `json:"published_at"`
	Draft       bool           `json:"draft"`
	Prerelease  bool           `json:"prerelease"`
	Assets      []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// Check 查最新稳定版并与 currentVersion 比较。currentVersion 形如 0.1.0（不带 v 也认）。
func (c *Client) Check(ctx context.Context, currentVersion string) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return Info{}, fmt.Errorf("%w：%v", ErrNetwork, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Info{}, fmt.Errorf("%w：%v", ErrNetwork, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Info{}, ErrNoStableRelease
	case resp.StatusCode == http.StatusForbidden:
		return Info{}, ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return Info{}, fmt.Errorf("%w：HTTP %d", ErrNetwork, resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Info{}, fmt.Errorf("%w：读应答失败：%v", ErrNetwork, err)
	}
	var rel release
	if err := json.Unmarshal(raw, &rel); err != nil {
		return Info{}, fmt.Errorf("%w：不是合法 JSON", ErrBadData)
	}
	// releases/latest 本来就只回非草稿、非预发布；字段还是真来了预发布就当坏数据拒，
	// 不把预发布版当升级目标（技术规划原话）。
	if rel.Prerelease || rel.Draft {
		return Info{}, fmt.Errorf("%w：端点返回了草稿或预发布版", ErrBadData)
	}
	latest, ok := parseTag(rel.TagName)
	if !ok {
		return Info{}, fmt.Errorf("%w：版本标签 %q 不是 v主版本.次版本.修订号 形状", ErrBadData, rel.TagName)
	}

	cur := normalizeVersion(currentVersion)
	info := Info{
		CurrentVersion: cur,
		LatestVersion:  latest,
		ReleaseURL:     rel.HTMLURL,
		PublishedAt:    rel.PublishedAt,
		CheckedAt:      c.now().UTC().Format(time.RFC3339),
	}
	for _, a := range rel.Assets {
		info.Assets = append(info.Assets, UpdateAsset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size})
	}
	info.UpdateAvailable = compareVersion(latest, cur) > 0
	return info, nil
}

// parseTag 收 v1.2.3 形状的标签（v 可省），只认三段数字——预发布后缀（-beta.1）一律不算稳定版。
func parseTag(tag string) (string, bool) {
	t := strings.TrimPrefix(strings.TrimSpace(tag), "v")
	if t == "" || strings.ContainsAny(t, "-+") {
		return "", false
	}
	parts := strings.Split(t, ".")
	if len(parts) != 3 {
		return "", false
	}
	for _, p := range parts {
		if _, err := strconv.Atoi(p); err != nil {
			return "", false
		}
	}
	return t, true
}

// normalizeVersion 把本地版本号整理成和 parseTag 相同的形状；解析不了就如实返回原文，
// 让「当前版本不认识」表现为「有更新可看」而不是静默判等。
func normalizeVersion(v string) string {
	t := strings.TrimPrefix(strings.TrimSpace(v), "v")
	if _, ok := parseTag(t); ok {
		return t
	}
	return strings.TrimSpace(v)
}

// compareVersion 按主.次.修订逐段比数值；段数不齐时短的一方按 0 补。
// 返回 >0 / 0 / <0。只用于稳定版三段号，不处理预发布顺序那种完整语义化版本的边角。
func compareVersion(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		ai, bi := segment(as, i), segment(bs, i)
		if ai != bi {
			if ai > bi {
				return 1
			}
			return -1
		}
	}
	return 0
}

func segment(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}
