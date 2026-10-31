package app

import (
	"path/filepath"
	"testing"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/store"
)

// newServiceForPoolTest 建一个带临时候选库和迷你目录的最小 Service：
// 不走 NewService，免得碰真实的用户配置目录。
func newServiceForPoolTest(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "pool.db"))
	if err != nil {
		t.Fatalf("开临时库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	cat := &catalog.Catalog{Categories: []catalog.Category{
		{ID: "github", Name: "GitHub", ScopeID: "github", Items: []catalog.Item{
			{ID: "gh-main", Name: "主站与 API", Hostnames: []string{"github.com", "codeload.github.com"}},
			{ID: "gh-mirror", Name: "镜像", Hostnames: []string{"github.com"}},
		}},
	}}
	return &Service{productCatalog: cat, candidateStore: db}
}

// GetCandidatePool：池行带二级菜单归属、按延迟升序、没延迟的垫底。
func TestGetCandidatePoolSortsAndAttributes(t *testing.T) {
	svc := newServiceForPoolTest(t)

	// 两个域名、三个地址：github.com 的 140.82.121.4（延迟 120）、codeload 的 9.9.9.9（延迟 30）、
	// 还有 github.com 一条没延迟的。
	result := &accelerator.Result{
		ItemID: "gh-main",
		Hostnames: []accelerator.HostOutcome{
			{Hostname: "github.com", Resolved: true},
			{Hostname: "codeload.github.com", Resolved: true},
		},
		Candidates: []accelerator.Candidate{
			{ID: "c1", Hostname: "github.com", IP: "140.82.121.4", Status: accelerator.StatusAvailable, LatencyMS: 120},
			{ID: "c2", Hostname: "codeload.github.com", IP: "9.9.9.9", Status: accelerator.StatusAvailable, LatencyMS: 30},
			{ID: "c3", Hostname: "github.com", IP: "20.205.243.166", Status: accelerator.StatusQueued},
		},
	}
	if err := svc.candidateStore.SaveRound("r", "github", "gitcdn", result); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}

	pool, err := svc.GetCandidatePool()
	if err != nil {
		t.Fatalf("读候选池失败: %v", err)
	}
	if len(pool) != 3 {
		t.Fatalf("池该有 3 行，实际 %d：%+v", len(pool), pool)
	}
	// 延迟升序，没延迟的垫底；同段按域名+地址。
	if pool[0].IP != "9.9.9.9" || pool[0].LatencyMS != 30 {
		t.Errorf("最快的该排第一，实际 %+v", pool[0])
	}
	if pool[1].IP != "140.82.121.4" || pool[2].IP != "20.205.243.166" {
		t.Errorf("有延迟的在前、没延迟的垫底，实际 %+v / %+v", pool[1], pool[2])
	}
	// 归属：github.com 在目录里同属两个项目，归属给数组；codeload 只属主站。
	if got := pool[1].ItemNames; len(got) != 2 || got[0] != "主站与 API" || got[1] != "镜像" {
		t.Errorf("github.com 该归两个二级菜单，实际 %v", got)
	}
	if got := pool[0].ItemNames; len(got) != 1 || got[0] != "主站与 API" {
		t.Errorf("codeload 该只归主站，实际 %v", got)
	}
}

func TestGetCandidatePoolWithoutStore(t *testing.T) {
	svc := newServiceForPoolTest(t)
	svc.candidateStore = nil
	pool, err := svc.GetCandidatePool()
	if err != nil || len(pool) != 0 {
		t.Fatalf("没接库该给空列表不是错误，实际 %d（err=%v）", len(pool), err)
	}
}
