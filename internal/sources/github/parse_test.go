package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const sampleText = `# comment title
140.82.121.4 github.com api.github.com

# 另一段
140.82.121.4 github.com
2606:50c0:8000::153 github.com
timeout
not-an-ip example.com
1.2.3.4
1.2.3.4 API.GitHub.COM.
`

func TestParse(t *testing.T) {
	got := Parse(sampleText)

	if rows := got["github.com"]; len(rows) != 2 {
		t.Fatalf("github.com 该有 2 条（去重 + 大小写归并），实际 %v", rows)
	} else if rows[0].IP != "140.82.121.4" || rows[0].IPv6 {
		t.Errorf("第一条该是 140.82.121.4 的 v4，实际 %+v", rows[0])
	} else if rows[1].IP != "2606:50c0:8000::153" || !rows[1].IPv6 {
		t.Errorf("第二条该是那条 v6，实际 %+v", rows[1])
	}

	// 一行多主机名：api.github.com 拿到同一地址；末行的大写带尾点写法
	// 规范化后归并到同一个键，补进 1.2.3.4。
	if rows := got["api.github.com"]; len(rows) != 2 || rows[0].IP != "140.82.121.4" || rows[1].IP != "1.2.3.4" {
		t.Errorf("api.github.com 该归并成 2 条，实际 %v", rows)
	}

	// 大小写和尾点归并到同一个键；写法规范化成小写。
	if rows := got["api.github.com."]; rows != nil {
		t.Errorf("尾点该被规范化掉，不该另有键")
	}
	if _, ok := got["1.2.3.4"]; ok {
		t.Errorf("只有 IP 没有主机名的行不该产出条目")
	}
	for _, dirty := range []string{"timeout", "not-an-ip"} {
		if _, ok := got[dirty]; ok {
			t.Errorf("脏行 %q 不该进结果", dirty)
		}
	}
}

func TestParseEmpty(t *testing.T) {
	for _, text := range []string{"", "\n\n", "# only comment"} {
		if got := Parse(text); len(got) != 0 {
			t.Errorf("空输入 %q 该给空映射，实际 %v", text, got)
		}
	}
}

func newSourceServer(t *testing.T, status int, body string, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetcherCachesWithinTTL(t *testing.T) {
	var hits atomic.Int64
	srv := newSourceServer(t, http.StatusOK, "140.82.121.4 github.com\n", &hits)
	f := NewFetcher(srv.Client(), time.Hour)
	src := Source{ID: "t", Name: "t", URL: srv.URL}

	for i := 0; i < 3; i++ {
		data, err := f.Hosts(context.Background(), src)
		if err != nil || len(data["github.com"]) != 1 {
			t.Fatalf("第 %d 次该命中缓存（err=%v，data=%v）", i+1, err, data)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("TTL 内三次调用该只拉一次，实际 %d 次", hits.Load())
	}
}

func TestFetcherFailureNotCached(t *testing.T) {
	var hits atomic.Int64
	srv := newSourceServer(t, http.StatusInternalServerError, "", &hits)
	f := NewFetcher(srv.Client(), time.Hour)
	src := Source{ID: "t", Name: "t", URL: srv.URL}

	if _, err := f.Hosts(context.Background(), src); err == nil {
		t.Fatal("源返回 500 该报错")
	}
	if _, err := f.Hosts(context.Background(), src); err == nil {
		t.Fatal("失败不缓存，第二次还是该报错")
	}
	if hits.Load() != 2 {
		t.Fatalf("两次失败该是两次真实请求，实际 %d 次", hits.Load())
	}
}

func TestFetcherRefetchAfterTTL(t *testing.T) {
	var hits atomic.Int64
	srv := newSourceServer(t, http.StatusOK, "140.82.121.4 github.com\n", &hits)
	f := NewFetcher(srv.Client(), time.Nanosecond) // TTL 为 0 的写法：立刻过期
	src := Source{ID: "t", Name: "t", URL: srv.URL}

	_, _ = f.Hosts(context.Background(), src)
	time.Sleep(time.Millisecond)
	_, _ = f.Hosts(context.Background(), src)
	if hits.Load() != 2 {
		t.Fatalf("TTL 过后该重拉，实际 %d 次", hits.Load())
	}
}

func TestFetcherBodyLimit(t *testing.T) {
	big := make([]byte, MaxBodyBytes+10)
	for i := range big {
		big[i] = '\n'
	}
	srv := newSourceServer(t, http.StatusOK, string(big), nil)
	f := NewFetcher(srv.Client(), time.Hour)
	if _, err := f.Hosts(context.Background(), Source{URL: srv.URL}); err == nil {
		t.Fatal("超限的源文件该被拒收")
	}
}

func TestMerge(t *testing.T) {
	a := Parse("1.1.1.1 github.com\n2.2.2.2 api.github.com\n")
	b := Parse("2.2.2.2 github.com\n1.1.1.1 api.github.com\n3.3.3.3 codeload.github.com\n")

	got := Merge(a, nil, b)
	if rows := got["github.com"]; len(rows) != 2 || rows[0].IP != "1.1.1.1" || rows[1].IP != "2.2.2.2" {
		t.Errorf("github.com 按源顺序拼接去重，实际 %v", rows)
	}
	if rows := got["api.github.com"]; len(rows) != 2 || rows[0].IP != "2.2.2.2" || rows[1].IP != "1.1.1.1" {
		t.Errorf("api.github.com 第一份源在前，实际 %v", rows)
	}
	if rows := got["codeload.github.com"]; len(rows) != 1 || rows[0].IP != "3.3.3.3" {
		t.Errorf("只有备源有的域名也该进来，实际 %v", rows)
	}
}

func TestAllPriorityOrder(t *testing.T) {
	all := All()
	if len(all) != 3 || all[0].ID != "gitcdn" || all[1].ID != "hellogithub" || all[2].ID != "gitlab" {
		t.Fatalf("三份源的优先序不对: %v", all)
	}
}
