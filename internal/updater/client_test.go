package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
