package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// 这一组门管「每个源各记一天」这条拍板：一天只下载一次、跨天才重下、清理之后当天就重下。
// 每条都用真文件（t.TempDir）和真 HTTP 服务，只有时钟是假的。

const cacheSampleText = "140.82.121.4 github.com api.github.com\n"

// newCountingServer 起一份数着请求次数的源站。fail 为真时一直回 500，用来验「拉失败不落盘」。
func newCountingServer(t *testing.T, hits *atomic.Int32, fail bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if fail {
			http.Error(w, "源站今天不在", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, cacheSampleText)
	}))
}

func testSource(url string) Source {
	return Source{ID: "gitcdn", Name: "测试源", URL: url}
}

// fetcherOn 建一份「重开程序」的拉源器：内存是空的，只共用那一个落盘目录。
func fetcherOn(client *http.Client, cache *FileCache) *Fetcher {
	f := NewFetcher(client, CacheTTL)
	f.UseCache(cache)
	return f
}

func TestSourceDownloadedOncePerDay(t *testing.T) {
	var hits atomic.Int32
	srv := newCountingServer(t, &hits, false)
	defer srv.Close()
	src := testSource(srv.URL)

	day := time.Date(2026, 11, 2, 8, 0, 0, 0, time.Local)
	cache := NewFileCache(t.TempDir())
	cache.useClock(func() time.Time { return day })

	ctx := context.Background()
	if _, err := fetcherOn(srv.Client(), cache).Hosts(ctx, src); err != nil {
		t.Fatalf("第一次取源应当成功: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("第一次应当下载 1 次，实际 %d", got)
	}

	// 同一份拉源器再问别的域名：走内存，不再下载。
	f := fetcherOn(srv.Client(), cache)
	if _, err := f.Hosts(ctx, src); err != nil {
		t.Fatalf("第二次取源应当成功: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("当天第二次不该再下载，实际累计 %d 次", got)
	}

	// 重开程序（内存清空、落盘还在）：当天第二次启动也不该下载。
	if _, err := fetcherOn(srv.Client(), cache).Hosts(ctx, src); err != nil {
		t.Fatalf("重开程序后取源应当成功: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("重开程序当天不该重新下载，实际累计 %d 次", got)
	}

	// 跨到第二天：当天那份过期，必须重下一次。
	day = day.AddDate(0, 0, 1)
	if _, err := fetcherOn(srv.Client(), cache).Hosts(ctx, src); err != nil {
		t.Fatalf("第二天取源应当成功: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("跨天应当重新下载（累计 2 次），实际 %d 次", got)
	}
}

func TestClearCacheForcesRedownload(t *testing.T) {
	var hits atomic.Int32
	srv := newCountingServer(t, &hits, false)
	defer srv.Close()
	src := testSource(srv.URL)

	cache := NewFileCache(t.TempDir())
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.Local)
	cache.useClock(func() time.Time { return now })

	ctx := context.Background()
	f := fetcherOn(srv.Client(), cache)
	if _, err := f.Hosts(ctx, src); err != nil {
		t.Fatalf("取源应当成功: %v", err)
	}
	if !f.CacheEnabled() {
		t.Fatal("接了落盘目录，CacheEnabled 应当为真")
	}

	removed, err := f.ClearCache([]Source{src})
	if err != nil {
		t.Fatalf("清理应当成功: %v", err)
	}
	if len(removed) != 1 || removed[0] != src.ID {
		t.Errorf("应当报清掉 1 份 %v，实际 %v", []string{src.ID}, removed)
	}

	// 紧接着再清一次：今天这份已经没了，不该谎报「又清掉了一份」。
	again, err := f.ClearCache([]Source{src})
	if err != nil {
		t.Fatalf("第二次清理也应当成功: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("没有当天缓存时应当报清掉 0 份，实际 %v", again)
	}

	// 清理之后当天就得重新下载——这正是「清理后才重新下载」那条。
	if _, err := f.Hosts(ctx, src); err != nil {
		t.Fatalf("清理后取源应当成功: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("清理后当天应当重新下载，实际累计 %d 次", got)
	}
}

func TestClearCacheWithoutDiskLayerIsNoOp(t *testing.T) {
	var hits atomic.Int32
	srv := newCountingServer(t, &hits, false)
	defer srv.Close()
	src := testSource(srv.URL)

	f := NewFetcher(srv.Client(), CacheTTL)
	if f.CacheEnabled() {
		t.Fatal("没接落盘目录时 CacheEnabled 应当为假")
	}
	removed, err := f.ClearCache([]Source{src})
	if err != nil {
		t.Fatalf("没接落盘目录时清理不该报错: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("没接落盘目录时不该报清掉了东西，实际 %v", removed)
	}
}

func TestDownloadFailureLeavesNoCacheFile(t *testing.T) {
	var hits atomic.Int32
	srv := newCountingServer(t, &hits, true)
	defer srv.Close()
	src := testSource(srv.URL)
	dir := t.TempDir()
	cache := NewFileCache(dir)

	ctx := context.Background()
	if _, err := fetcherOn(srv.Client(), cache).Hosts(ctx, src); err == nil {
		t.Fatal("源站回 500 时应当报错")
	}
	if _, err := os.Stat(filepath.Join(dir, src.ID+".json")); !os.IsNotExist(err) {
		t.Errorf("拉失败不该留下缓存文件，实际 stat=%v", err)
	}

	// 下一轮还要能再试：失败没有被当成「今天已经拉过了」。
	if _, err := fetcherOn(srv.Client(), cache).Hosts(ctx, src); err == nil {
		t.Fatal("第二次仍然应当报错")
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("拉失败不该占掉当天这一次，应当各试一遍（累计 2 次），实际 %d 次", got)
	}
}

func TestCacheFileMustMatchSourceAndToday(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 2, 8, 0, 0, 0, time.Local)
	cache := NewFileCache(dir)
	cache.useClock(func() time.Time { return now })
	src := testSource("https://example.invalid/hosts")

	writeRaw := func(name string, f cacheFile) {
		t.Helper()
		raw, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("组装缓存内容失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0o600); err != nil {
			t.Fatalf("写缓存文件失败: %v", err)
		}
	}
	rows := map[string][]Entry{"github.com": {{IP: "1.1.1.1"}}}

	// 另一份源的文件、昨天的文件、内容坏掉的文件，三种都当作没有。
	writeRaw("gitcdn.json", cacheFile{Source: "gitlab", Day: now.Format(DayLayout), Hostnames: rows})
	if _, ok := cache.Load(src); ok {
		t.Error("文件里写的是别的源，不该当成本源当天的缓存")
	}
	writeRaw("gitcdn.json", cacheFile{Source: src.ID, Day: now.AddDate(0, 0, -1).Format(DayLayout), Hostnames: rows})
	if _, ok := cache.Load(src); ok {
		t.Error("日期不是今天，不该当成本天的缓存")
	}
	if err := os.WriteFile(filepath.Join(dir, "gitcdn.json"), []byte("{不是 JSON"), 0o600); err != nil {
		t.Fatalf("写坏文件失败: %v", err)
	}
	if _, ok := cache.Load(src); ok {
		t.Error("内容读不懂时不该当成本天的缓存")
	}

	// 反过来：今天、本源、内容完整，必须读得回来，且读回来的域名和地址一个字不差。
	if err := cache.Save(src, rows); err != nil {
		t.Fatalf("落盘应当成功: %v", err)
	}
	got, ok := cache.Load(src)
	if !ok {
		t.Fatal("当天本源的好文件应当读得回来")
	}
	if len(got["github.com"]) != 1 || got["github.com"][0].IP != "1.1.1.1" {
		t.Errorf("读回来的内容应当和写进去的一样，实际 %+v", got["github.com"])
	}
}
