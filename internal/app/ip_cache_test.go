package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/store"
)

// 清理全部 IP 缓存是「用户主动点的全清」：底账和内存这一份都得认得出样本、清完都查不到。
// 库没打开时也不能假装清过，但内存那轮仍必须丢掉——界面摆着旧候选就是没清干净。
func clearCacheSample() *accelerator.Result {
	return &accelerator.Result{
		ItemID:    "gh-main",
		Hostnames: []accelerator.HostOutcome{{Hostname: "github.com", Resolved: true}},
		Candidates: []accelerator.Candidate{
			{ID: "c1", Hostname: "github.com", IP: "140.82.121.4", Status: accelerator.StatusAvailable, LatencyMS: 120},
		},
		ProbesUsed: 1, DNSQueries: 1, Available: 1,
		StartedAt:  time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 11, 6, 8, 0, 5, 0, time.UTC),
	}
}

func TestClearAllIPCacheEmptiesLedgerAndMemory(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "detect.db"))
	if err != nil {
		t.Fatalf("开底账失败: %v", err)
	}
	defer db.Close()
	if err := db.SaveRound("r1", "github", "dns", clearCacheSample()); err != nil {
		t.Fatalf("准备底账失败: %v", err)
	}
	svc := &Service{candidateStore: db, detectResults: newDetectResultStore()}
	svc.detectResults.put("github", "dns", clearCacheSample())
	if _, ok := svc.detectResults.view("gh-main"); !ok {
		t.Fatal("准备阶段内存里该有一轮结论")
	}

	res, err := svc.ClearAllIPCache()
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if res.Rounds != 1 || res.Addresses == 0 {
		t.Errorf("回执该报清掉 1 轮和至少 1 条地址，实际 %+v", res)
	}
	if _, ok := svc.detectResults.view("gh-main"); ok {
		t.Error("内存里那轮结论没清掉，界面还会摆着旧候选")
	}
	again, err := svc.ClearAllIPCache()
	if err != nil || again.Rounds != 0 || again.Addresses != 0 {
		t.Errorf("底账已空，再清一次应是 0/0 且不报错，实际 %+v err=%v", again, err)
	}
}

func TestClearAllIPCacheWithoutLedgerStillClearsMemory(t *testing.T) {
	svc := &Service{detectResults: newDetectResultStore()}
	svc.detectResults.put("github", "dns", clearCacheSample())
	res, err := svc.ClearAllIPCache()
	if err != nil {
		t.Fatalf("没有底账也不该报错: %v", err)
	}
	if res != (IPCacheClearResult{}) {
		t.Errorf("没有底账就不该报清掉了行，实际 %+v", res)
	}
	if _, ok := svc.detectResults.view("gh-main"); ok {
		t.Error("库没打开，内存里那轮结论仍必须清掉")
	}
}
