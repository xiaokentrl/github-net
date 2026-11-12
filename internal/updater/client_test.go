package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testClient(url string) *Client {
	c := NewClient()
	c.baseURL = url
	return c
}

func TestCheckFindsUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.0","html_url":"https://github.com/xiaokentrl/github-net/releases/tag/v0.2.0","published_at":"2026-10-30T00:00:00Z"}`))
	}))
	defer srv.Close()

	info, err := testClient(srv.URL).Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatalf("检查不该失败: %v", err)
	}
	if !info.UpdateAvailable || info.LatestVersion != "0.2.0" || info.CurrentVersion != "0.1.0" {
		t.Fatalf("结论不对: %+v", info)
	}
	if info.ReleaseURL == "" || info.CheckedAt == "" {
		t.Fatalf("发布链接和检查时间该带上: %+v", info)
	}
}

func TestCheckUpToDateAndVersionShapes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.0","html_url":"https://x"}`))
	}))
	defer srv.Close()

	// 本地版本带不带 v 都认：一样高就是「没有更新」。
	for _, current := range []string{"0.1.0", "v0.1.0"} {
		info, err := testClient(srv.URL).Check(context.Background(), current)
		if err != nil {
			t.Fatalf("检查不该失败: %v", err)
		}
		if info.UpdateAvailable || info.LatestVersion != "0.1.0" {
			t.Fatalf("同版本该是「没有更新」且带出最新版本号，实际 %+v", info)
		}
	}
}

func TestCheckOlderRemoteIsNoUpdate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v0.0.9","html_url":"https://x"}`))
	}))
	defer srv.Close()

	info, err := testClient(srv.URL).Check(context.Background(), "0.1.0")
	if err != nil {
		t.Fatalf("检查不该失败: %v", err)
	}
	if info.UpdateAvailable {
		t.Fatalf("远端更旧不该提示升级: %+v", info)
	}
}

func TestCheckFailureModesAreDistinct(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"仓库还没有发布", http.StatusNotFound, `{}`, ErrNoStableRelease},
		{"匿名限流", http.StatusForbidden, `{"message":"API rate limit exceeded"}`, ErrRateLimited},
		{"服务端错误", http.StatusInternalServerError, `oops`, ErrNetwork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			if _, err := testClient(srv.URL).Check(context.Background(), "0.1.0"); !errors.Is(err, tc.wantErr) {
				t.Fatalf("该归为 %v，实际 %v", tc.wantErr, err)
			}
		})
	}
}

func TestCheckRejectsBadData(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"不是 JSON", `<!doctype html>`},
		{"预发布版不当稳定版", `{"tag_name":"v0.2.0-beta.1","prerelease":true,"html_url":"https://x"}`},
		{"标签形状不认识", `{"tag_name":"release-2026","html_url":"https://x"}`},
		{"带构建后缀不认", `{"tag_name":"v0.2.0+build.7","html_url":"https://x"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			if _, err := testClient(srv.URL).Check(context.Background(), "0.1.0"); !errors.Is(err, ErrBadData) {
				t.Fatalf("坏数据该归为 ErrBadData，实际 %v", err)
			}
		})
	}
}

func TestCheckNetworkFailureAndTimeout(t *testing.T) {
	// 端点直接关掉：传输层失败归 ErrNetwork。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()
	if _, err := testClient(srv.URL).Check(context.Background(), "0.1.0"); !errors.Is(err, ErrNetwork) {
		t.Fatalf("连接失败该归为 ErrNetwork，实际 %v", err)
	}

	// 挂住不回：8 秒上限（测试里不等真 8 秒，把超时压短验证路径存在）。
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer hang.Close()
	c := testClient(hang.URL)
	c.http.Timeout = 100 * time.Millisecond
	if _, err := c.Check(context.Background(), "0.1.0"); !errors.Is(err, ErrNetwork) {
		t.Fatalf("超时该归为 ErrNetwork，实际 %v", err)
	}
}

func TestCheckHonorsContextCancellation(t *testing.T) {
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer hang.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := testClient(hang.URL).Check(ctx, "0.1.0"); !errors.Is(err, ErrNetwork) {
		t.Fatalf("取消该归为 ErrNetwork，实际 %v", err)
	}
}

func TestParseTagShapes(t *testing.T) {
	ok := map[string]string{"v1.2.3": "1.2.3", "0.1.0": "0.1.0", " v10.0.2 ": "10.0.2"}
	for in, want := range ok {
		if got, valid := parseTag(in); !valid || got != want {
			t.Fatalf("parseTag(%q) = %q,%v，想要 %q", in, got, valid, want)
		}
	}
	bad := []string{"", "v1", "v1.2", "1.2.3-beta.1", "v1.2.3+build", "release-2026", "v01.x.3"}
	for _, in := range bad {
		if _, valid := parseTag(in); valid {
			t.Fatalf("parseTag(%q) 不该认", in)
		}
	}
}

func TestCompareVersionSegments(t *testing.T) {
	if compareVersion("0.2.0", "0.1.9") <= 0 {
		t.Fatal("0.2.0 应大于 0.1.9")
	}
	if compareVersion("1.0.0", "0.99.99") <= 0 {
		t.Fatal("主版本位该优先")
	}
	if compareVersion("0.1.0", "0.1.0") != 0 {
		t.Fatal("相等该是 0")
	}
	// 本地版本不是三段数（比如开发构建）：按 0 补齐比，不 panic。
	if compareVersion("0.2.0", "dev") <= 0 {
		t.Fatal("无法解析的本地版本按 0 补，0.2.0 应大于它")
	}
}

// 资产来源白名单（安全边界方案）：上游应答被污染时，资产可指向任意域。
// 默认白名单全是 https 的 GitHub 域，http 的假域必须被当场拒掉，不发任何请求。
func TestDownloadRejectsOffWhitelistOrigin(t *testing.T) {
	c := NewClient()
	_, err := c.Download(context.Background(), UpdateAsset{
		Name: "github-net_0.2.0_amd64.deb",
		URL:  "http://evil.example/github-net_0.2.0_amd64.deb",
		Size: 10,
	}, t.TempDir())
	if !errors.Is(err, ErrBadData) {
		t.Fatalf("非白名单来源该归为 ErrBadData，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "不在白名单") {
		t.Fatalf("错误里该写明白名单原因，实际 %v", err)
	}
}

// 重定向逐跳复验：白名单域 302 到任意域必须被拦，不白给「白名单挡下载哪」开口子。
func TestDownloadRejectsCrossOriginRedirect(t *testing.T) {
	var inner, outer *httptest.Server
	inner = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, outer.URL+"/pkg.deb", http.StatusFound)
	}))
	defer inner.Close()
	outer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("跨域重定向不该真的跟过来")
		w.WriteHeader(http.StatusOK)
	}))
	defer outer.Close()

	c := NewClient()
	c.SetAllowedAssetOrigins([]string{inner.URL})
	_, err := c.Download(context.Background(), UpdateAsset{
		Name: "github-net_0.2.0_amd64.deb",
		URL:  inner.URL + "/github-net_0.2.0_amd64.deb",
		Size: 10,
	}, t.TempDir())
	if !errors.Is(err, ErrBadData) || !strings.Contains(err.Error(), "不在白名单") {
		t.Fatalf("跨域重定向该被白名单拦下（ErrBadData），实际 %v", err)
	}
}

// 版本比较是数值逐段比，不是字符串比：两位数的次版本号排在一位数之后
// （字符串比较会把 "0.10" 排在 "0.9" 前面，更新提示就错乱）。三段不齐按 0 补。
func TestCompareVersionNumericOrder(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.9", "0.10", -1},
		{"0.10", "0.9", 1},
		{"0.10", "0.10", 0},
		{"1.0", "0.99", 1},
		{"0.1.9", "0.2", -1},
		{"1.2", "1.2.0", 0},
	}
	for _, c := range cases {
		if got := compareVersion(c.a, c.b); got != c.want {
			t.Errorf("compareVersion(%q, %q) = %d，该是 %d", c.a, c.b, got, c.want)
		}
	}
}
