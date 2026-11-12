package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/retention"
	"github.com/xiaokentrl/github-net/internal/sources/github"
	"github.com/xiaokentrl/github-net/internal/store"
)

// ageFile 写下这个文件并把它的修改时间摆到 age 之前，用来模拟「这份缓存已经躺了几天」。
func ageFile(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("建 %s 的目录失败: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("内容"), 0o600); err != nil {
		t.Fatalf("写 %s 失败: %v", path, err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("给 %s 摆年龄失败: %v", path, err)
	}
}

// pruneExpiredFiles：超过保留期限、且名字是本应用写下的普通文件才删；界内的留着，别人的名字不碰，
// 目录不碰、也不往子目录里钻（这个落点未必是本应用独占的，只删自己写下的文件才谈得上安全）。
func TestPruneExpiredFilesRemovesOldRegularFilesOnly(t *testing.T) {
	dir := t.TempDir()
	ageFile(t, filepath.Join(dir, "github-net_0.1.0_amd64.deb"), 4*24*time.Hour)
	// 落盘落到一半崩掉留下的临时文件：名字以 `.包名` 打头，也是本应用写下的，超期一样带走。
	ageFile(t, filepath.Join(dir, ".github-net_0.1.0_amd64.deb.123456"), 4*24*time.Hour)
	// 别人的文件：年龄再老也不归我们删——这是删除那一刻的归属门。
	ageFile(t, filepath.Join(dir, "someone-else-notes.txt"), 30*24*time.Hour)
	// 还差一分钟才到 3 天：界内，必须留着。「正好等于界限」那一格由 github 包和底账的用例钉住（那里时钟是注入的，不受用例跑过去了几微秒影响）。
	ageFile(t, filepath.Join(dir, "github-net_0.1.0_amd64.deb.sig"), retention.MaxAge-time.Minute)
	fresh := filepath.Join(dir, "github-net_0.1.1_amd64.deb")
	if err := os.WriteFile(fresh, []byte("刚下的"), 0o600); err != nil {
		t.Fatalf("写新包失败: %v", err)
	}
	inside := filepath.Join(dir, "别人的目录", "里面老文件.txt")
	ageFile(t, inside, 30*24*time.Hour)

	removed, err := pruneExpiredFiles(dir)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if removed != 2 {
		t.Errorf("只该删掉那两个超期的包（安装包 + 半截临时文件），实际删了 %d 个", removed)
	}
	for _, gone := range []string{"github-net_0.1.0_amd64.deb", ".github-net_0.1.0_amd64.deb.123456"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("超过 3 天的 %s 该被删掉，实际 stat=%v", gone, err)
		}
	}
	for _, keep := range []string{"github-net_0.1.0_amd64.deb.sig", "github-net_0.1.1_amd64.deb", "someone-else-notes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s 不该被删（界内的旧文件 / 刚写下的新文件 / 别人的文件），实际 stat=%v", keep, err)
		}
	}
	if _, err := os.Stat(inside); err != nil {
		t.Errorf("子目录里的文件不该被碰（不递归），实际 stat=%v", err)
	}

	// 目录还不存在（一次下载都没发生过）不是失败。
	if n, err := pruneExpiredFiles(filepath.Join(dir, "没有这个目录")); err != nil || n != 0 {
		t.Errorf("目录不存在时该安静地返回 0，实际 %d（err=%v）", n, err)
	}
}

// 启动那一次清理三样一起走：候选底账、公共源的落盘缓存（走注入的接缝，不碰真目录）、升级包目录。
// 界内的东西必须原样留着——清缓存清过头比不清更糟。
func TestPruneCachesSweepsThreeLayersAndKeepsFresh(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "detect.db"))
	if err != nil {
		t.Fatalf("开临时库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	justNow := time.Now().UTC()
	if err := db.SaveRound("r-new", "github", "all-sources", &accelerator.Result{
		ItemID:     "gh-main",
		Hostnames:  []accelerator.HostOutcome{{Hostname: "github.com", Resolved: true}},
		Candidates: []accelerator.Candidate{{ID: "c1", Hostname: "github.com", IP: "1.1.1.1", Status: accelerator.StatusAvailable, LatencyMS: 20}},
		StartedAt:  justNow,
		FinishedAt: justNow,
		Available:  1,
		ProbesUsed: 1,
		DNSQueries: 1,
	}); err != nil {
		t.Fatalf("写本轮失败: %v", err)
	}

	settingsDir := t.TempDir()
	settings := config.NewStore(filepath.Join(settingsDir, "cfg", "config.json"), nil, nil, nil)
	updates := filepath.Join(settingsDir, "cfg", "updates") // updatesDir = config.json 所在目录旁的 updates/
	ageFile(t, filepath.Join(updates, "github-net_0.1.0_amd64.deb"), 4*24*time.Hour)
	freshPkg := filepath.Join(updates, "github-net_0.1.1_amd64.deb")
	ageFile(t, freshPkg, time.Hour)

	srcCache := &fakeSources{enabled: true}
	s := &Service{settings: settings, candidateStore: db, sourceCache: srcCache}
	PruneCaches(s)

	if srcCache.pruneCalls != 1 {
		t.Errorf("公共源那一份落盘缓存该被清一次，实际清了 %d 次", srcCache.pruneCalls)
	}
	if _, err := os.Stat(filepath.Join(updates, "github-net_0.1.0_amd64.deb")); !os.IsNotExist(err) {
		t.Errorf("超过 3 天的升级包该被删掉，实际 stat=%v", err)
	}
	if _, err := os.Stat(freshPkg); err != nil {
		t.Errorf("刚下的包不该被删，实际 stat=%v", err)
	}
	rounds, err := db.Rounds(0)
	if err != nil || len(rounds) != 1 || rounds[0].TaskRound != "r-new" {
		t.Fatalf("界内那轮底账不该被清掉，实际 %+v（err=%v）", rounds, err)
	}
	pool, err := db.Pool("github.com")
	if err != nil || len(pool) != 1 || pool[0].IP != "1.1.1.1" {
		t.Errorf("界内的地址池行不该被清掉，实际 %v（err=%v）", pool, err)
	}

	// 三样都没接上（库没开成、源缓存没接、配置读不到）也不能炸，这只是启动时顺手做的事。
	PruneCaches(&Service{})
}

// 没注入接缝时走的是 github.Default 那一份真实落盘缓存——启动清理得真的能把超期文件带走。
func TestPruneCachesUsesSharedSourceCacheWhenSeamAbsent(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "sources-cache", "hellogithub.json")
	ageFile(t, stale, 4*24*time.Hour)

	f := github.NewFetcher(nil, github.CacheTTL)
	f.UseCache(github.NewFileCache(filepath.Join(dir, "sources-cache")))
	old := github.Default
	github.Default = f
	t.Cleanup(func() { github.Default = old })

	PruneCaches(&Service{})
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("超期的源缓存该被删掉，实际 stat=%v", err)
	}
}
