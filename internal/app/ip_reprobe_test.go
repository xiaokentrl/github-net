package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/detector"
	"github.com/xiaokentrl/github-net/internal/store"
)

// 复测这条命令：假的探测器不碰外网，验证「判据来自 Verdict、结论刷回候选池、回执带菜单归属」。
func reprobeService(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "probe.db"))
	if err != nil {
		t.Fatalf("开临时库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	svc := newServiceForPoolTest(t)
	svc.candidateStore = db
	return svc
}

func putPoolRow(t *testing.T, svc *Service, hostname, ip string) {
	t.Helper()
	res := &accelerator.Result{
		ItemID: "gh-main",
		Hostnames: []accelerator.HostOutcome{
			{Hostname: hostname, Resolved: true},
		},
		Candidates: []accelerator.Candidate{
			{ID: "c1", Hostname: hostname, IP: ip, Status: accelerator.StatusAvailable, LatencyMS: 120},
		},
		Available:  1,
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}
	if err := svc.candidateStore.SaveRound("r1", "github", "dns", res); err != nil {
		t.Fatalf("准备候选池失败: %v", err)
	}
}

func TestReprobeIPWritesBackAndReturnsMenu(t *testing.T) {
	svc := reprobeService(t)
	putPoolRow(t, svc, "github.com", "140.82.121.4")
	svc.applyProbe = func(_ context.Context, _ detector.Target) (detector.Result, error) {
		return detector.Result{LatencyMS: 33}, nil // 没给 Kind/OK：Verdict 归「未检测」
	}

	row, err := svc.ReprobeIP("github.com", "140.82.121.4")
	if err != nil {
		t.Fatalf("复测失败: %v", err)
	}
	if row.Status != string(accelerator.StatusUntested) || row.LatencyMS != 33 {
		t.Errorf("状态和延迟该按假探测结果刷新，实际 %+v", row)
	}
	if row.Hostname != "github.com" || row.IP != "140.82.121.4" {
		t.Errorf("回执该是这一行，实际 %+v", row)
	}
	if len(row.ItemNames) == 0 || row.ItemNames[0] != "主站与 API" {
		t.Errorf("回执该带目录里的二级菜单归属，实际 %v", row.ItemNames)
	}
	pool, err := svc.GetCandidatePool()
	if err != nil {
		t.Fatalf("读池失败: %v", err)
	}
	if len(pool) != 1 || pool[0].Status != string(accelerator.StatusUntested) {
		t.Errorf("池里那行该被真刷掉，实际 %+v", pool)
	}
}

func TestReprobeIPAvailableVerdict(t *testing.T) {
	svc := reprobeService(t)
	putPoolRow(t, svc, "github.com", "140.82.121.4")
	svc.applyProbe = func(_ context.Context, _ detector.Target) (detector.Result, error) {
		return detector.Result{OK: true, Verified: true, LatencyMS: 12}, nil
	}
	row, err := svc.ReprobeIP("github.com", "140.82.121.4")
	if err != nil || row.Status != string(accelerator.StatusAvailable) {
		t.Fatalf("连上且证书对得上该判可用，实际 %+v err=%v", row, err)
	}
}

func TestReprobeIPRejectsBadInput(t *testing.T) {
	svc := reprobeService(t)
	for _, tc := range []struct {
		name, host, ip string
	}{
		{"空域名", "", "1.2.3.4"},
		{"空地址", "github.com", ""},
		{"不是 IP", "github.com", "github.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.ReprobeIP(tc.host, tc.ip); err == nil {
				t.Error("输入不对该报错，不能拿去发探测")
			}
		})
	}
}

// 预览口径：Domains 是去重域名数，不等于地址行数；逐轮候选和内存结论都要报出来。
func TestPreviewIPCacheReportsAllClearedData(t *testing.T) {
	svc := reprobeService(t)
	svc.detectResults = newDetectResultStore()
	// 一轮里给同一域名放两条地址：SaveRound 会先清同域名的旧池行，分两轮写只会剩一条。
	res := &accelerator.Result{
		ItemID:    "gh-main",
		Hostnames: []accelerator.HostOutcome{{Hostname: "github.com", Resolved: true}},
		Candidates: []accelerator.Candidate{
			{ID: "c1", Hostname: "github.com", IP: "140.82.121.4", Status: accelerator.StatusAvailable, LatencyMS: 10},
			{ID: "c2", Hostname: "github.com", IP: "20.205.243.166", Status: accelerator.StatusAvailable, LatencyMS: 20},
		},
		Available:  2,
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
	}
	if err := svc.candidateStore.SaveRound("r1", "github", "dns", res); err != nil {
		t.Fatalf("准备候选池失败: %v", err)
	}
	svc.detectResults.put("github", "dns", &accelerator.Result{ItemID: "gh-main"})

	preview, err := svc.PreviewIPCache()
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if preview.Addresses != 2 || preview.Domains != 1 {
		t.Errorf("2 条地址只属于 1 个域名，实际 addresses=%d domains=%d", preview.Addresses, preview.Domains)
	}
	if preview.CandidateRows == 0 {
		t.Error("逐轮候选记录会被清掉，预览必须报出来")
	}
	if preview.MemoryResults != 1 {
		t.Errorf("内存里有 1 个项目的结论会被清掉，实际 %d", preview.MemoryResults)
	}
}

// 库没接上时仍清内存，但回执要说明「底账没接上」，不能报成「清掉 0 轮 0 条」。
func TestClearAllIPCacheMarksDetachedLedger(t *testing.T) {
	svc := &Service{detectResults: newDetectResultStore()}
	res, err := svc.ClearAllIPCache()
	if err != nil {
		t.Fatalf("没底账也不该报错: %v", err)
	}
	if res.LedgerAttached {
		t.Error("底账没接上，回执该标出来")
	}
}
