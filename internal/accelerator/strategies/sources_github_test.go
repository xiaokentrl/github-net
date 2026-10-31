package strategies

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	github "github.com/xiaokentrl/github-net/internal/sources/github"
)

// stubTransport 按完整 URL 发 canned 应答：注册表里的方法闭包认的是真实源 URL，
// 测试把 Default 换成带这个 transport 的拉源器，就能不碰网络地喂三个源。
type stubTransport struct {
	responses map[string]stubResponse
}

type stubResponse struct {
	status int
	body   string
}

func (s stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, ok := s.responses[req.URL.String()]
	if !ok {
		resp = stubResponse{status: http.StatusNotFound}
	}
	return &http.Response{
		StatusCode: resp.status,
		Body:       io.NopCloser(strings.NewReader(resp.body)),
		Header:     http.Header{},
		Request:    req,
	}, nil
}

func swapFetcher(t *testing.T, responses map[string]stubResponse) {
	t.Helper()
	old := github.Default
	github.Default = github.NewFetcher(&http.Client{Transport: stubTransport{responses}}, time.Hour)
	t.Cleanup(func() { github.Default = old })
}

const (
	gitcdnText = `# gitcdn
140.82.121.4 github.com
20.205.243.166 github.com
140.82.121.4 api.github.com
`
	hellogithubText = `# hellogithub
20.205.243.166 github.com
180.97.162.77 github.com
`
	gitlabText = `# gitlab
timeout
140.82.121.3 codeload.github.com
`
)

func sourceStubResponses() map[string]stubResponse {
	return map[string]stubResponse{
		github.GitCDN.URL:      {status: 200, body: gitcdnText},
		github.HelloGitHub.URL: {status: 200, body: hellogithubText},
		github.GitLab.URL:      {status: 200, body: gitlabText},
	}
}

func TestSourceStrategiesRegisteredForGitHubScope(t *testing.T) {
	for _, err := range Errors() {
		t.Fatalf("init 注册不该有错: %v", err)
	}
	for _, id := range []string{StrategyIDGitCDN, StrategyIDHelloGitHub, StrategyIDGitLab, StrategyIDAllSources} {
		s, ok := Get(id)
		if !ok {
			t.Fatalf("方法 %s 没注册进表", id)
		}
		if len(s.ScopeIDs) != 1 || s.ScopeIDs[0] != scopeGitHub {
			t.Errorf("%s 该只声明 github 范围，实际 %v", id, s.ScopeIDs)
		}
		if s.Caps.has(CapabilitySystemDNSOnly) {
			t.Errorf("%s 不碰解析器，不该带 SystemDNSOnly 能力位", id)
		}
	}
}

func TestSingleSourceLookup(t *testing.T) {
	swapFetcher(t, sourceStubResponses())
	s, _ := Get(StrategyIDGitCDN)

	out, err := s.Lookup(context.Background(), Input{Hostname: "github.com", Max: 8})
	if err != nil {
		t.Fatalf("gitcdn 里明明有 github.com: %v", err)
	}
	// 同源内去重：140.82.121.4 和 20.205.243.166 两条。
	if len(out.Candidates) != 2 || out.Candidates[0].IP != "140.82.121.4" || out.Candidates[1].IP != "20.205.243.166" {
		t.Errorf("候选对不上: %+v", out.Candidates)
	}
	if out.Truncated {
		t.Errorf("没裁剪就不该标 truncated")
	}

	// Max 裁剪要如实标记。
	out, err = s.Lookup(context.Background(), Input{Hostname: "github.com", Max: 1})
	if err != nil || len(out.Candidates) != 1 || !out.Truncated {
		t.Errorf("Max=1 该裁到 1 条并标 truncated，实际 %+v（err=%v）", out, err)
	}

	// 源里没有的域名是「没有条目」，不是失败。
	_, err = s.Lookup(context.Background(), Input{Hostname: "example.com", Max: 8})
	var lookupErr *LookupError
	if !errors.As(err, &lookupErr) || lookupErr.Kind != KindNoRecords {
		t.Errorf("没有条目该报 KindNoRecords，实际 %v", err)
	}
}

func TestAllSourcesLookupMergesAndDedupes(t *testing.T) {
	swapFetcher(t, sourceStubResponses())
	s, _ := Get(StrategyIDAllSources)

	out, err := s.Lookup(context.Background(), Input{Hostname: "github.com", Max: 8})
	if err != nil {
		t.Fatalf("合并查找失败: %v", err)
	}
	// gitcdn 两条在前，hellogithub 补进 180.97.162.77，重复的 20.205.243.166 只留 gitcdn 那次。
	want := []string{"140.82.121.4", "20.205.243.166", "180.97.162.77"}
	if len(out.Candidates) != len(want) {
		t.Fatalf("合并后该是 %d 条，实际 %+v", len(want), out.Candidates)
	}
	for i, ip := range want {
		if out.Candidates[i].IP != ip {
			t.Errorf("第 %d 条该是 %s，实际 %s", i, ip, out.Candidates[i].IP)
		}
	}
}

func TestAllSourcesToleratesOneBrokenSource(t *testing.T) {
	responses := sourceStubResponses()
	responses[github.HelloGitHub.URL] = stubResponse{status: 500}
	swapFetcher(t, responses)
	s, _ := Get(StrategyIDAllSources)

	// 一份源挂了不挡合并：gitcdn 和 gitlab 的条目照常进候选。
	out, err := s.Lookup(context.Background(), Input{Hostname: "codeload.github.com", Max: 8})
	if err != nil || len(out.Candidates) != 1 || out.Candidates[0].IP != "140.82.121.3" {
		t.Errorf("部分源挂掉该照常合并，实际 %+v（err=%v）", out.Candidates, err)
	}
}

func TestAllSourcesFailsWhenAllSourcesDown(t *testing.T) {
	responses := map[string]stubResponse{}
	for _, src := range github.All() {
		responses[src.URL] = stubResponse{status: 500}
	}
	swapFetcher(t, responses)
	s, _ := Get(StrategyIDAllSources)

	_, err := s.Lookup(context.Background(), Input{Hostname: "github.com", Max: 8})
	var lookupErr *LookupError
	if !errors.As(err, &lookupErr) || lookupErr.Kind != KindOther {
		t.Errorf("三份全挂该报「取不回来」，实际 %v", err)
	}
}

func TestSourceLookupRejectsBadInput(t *testing.T) {
	swapFetcher(t, sourceStubResponses())
	s, _ := Get(StrategyIDGitCDN)
	if _, err := s.Lookup(context.Background(), Input{Hostname: "", Max: 8}); !errors.Is(err, ErrBadInput) {
		t.Errorf("空主机名该是 ErrBadInput，实际 %v", err)
	}
	if _, err := s.Lookup(context.Background(), Input{Hostname: "github.com", Max: 0}); !errors.Is(err, ErrBadInput) {
		t.Errorf("Max=0 该是 ErrBadInput，实际 %v", err)
	}
}
