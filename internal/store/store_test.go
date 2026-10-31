package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "detect.db"))
	if err != nil {
		t.Fatalf("开库失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sampleResult() *accelerator.Result {
	return &accelerator.Result{
		ItemID: "gh-main",
		Hostnames: []accelerator.HostOutcome{
			{Hostname: "github.com", Resolved: true},
		},
		Candidates: []accelerator.Candidate{
			{ID: "c1", Hostname: "github.com", IP: "140.82.121.4", Status: accelerator.StatusAvailable, LatencyMS: 120, TLSMS: 300},
			{ID: "c2", Hostname: "github.com", IP: "20.205.243.166", IPv6: false, Status: accelerator.StatusTimeout, Reason: "探测超时"},
			{ID: "c3", Hostname: "github.com", IP: "2606:50c0:8000::153", IPv6: true, Status: accelerator.StatusAvailable, LatencyMS: 90, TLSMS: 250, Applied: true},
		},
		ProbesUsed: 3, DNSQueries: 2, Available: 2,
		StartedAt:  time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 10, 31, 8, 0, 5, 0, time.UTC),
	}
}

func TestSaveAndReadBack(t *testing.T) {
	s := newTestStore(t)
	in := sampleResult()
	if err := s.SaveRound("round-7", "github", "dns", in); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}

	rounds, err := s.Rounds(0)
	if err != nil || len(rounds) != 1 {
		t.Fatalf("轮数 = %d（err=%v），期望 1", len(rounds), err)
	}
	r := rounds[0]
	if r.TaskRound != "round-7" || r.ItemID != "gh-main" || r.ScopeID != "github" || r.StrategyID != "dns" {
		t.Errorf("轮头字段对不上: %+v", r)
	}
	if r.Available != 2 || r.ProbesUsed != 3 || r.DNSQueries != 2 {
		t.Errorf("轮结论数字对不上: %+v", r)
	}
	if r.StartedAt != "2026-10-31T08:00:00Z" || r.FinishedAt != "2026-10-31T08:00:05Z" {
		t.Errorf("时间没按 UTC RFC3339 落: %q / %q", r.StartedAt, r.FinishedAt)
	}

	cands, err := s.Candidates(r.ID)
	if err != nil || len(cands) != 3 {
		t.Fatalf("候选数 = %d（err=%v），期望 3", len(cands), err)
	}
	if cands[0].IP != "140.82.121.4" || cands[0].Status != "available" || cands[0].LatencyMS != 120 || cands[0].TLSMS != 300 {
		t.Errorf("第一条候选对不上: %+v", cands[0])
	}
	if cands[1].Status != "timeout" || cands[1].Reason != "探测超时" {
		t.Errorf("第二条候选对不上: %+v", cands[1])
	}
	if !cands[2].IPv6 || !cands[2].Applied {
		t.Errorf("IPv6/当前生效两个布尔丢了: %+v", cands[2])
	}
}

func TestSaveEmptyCandidateRound(t *testing.T) {
	s := newTestStore(t)
	in := sampleResult()
	in.Candidates = nil
	if err := s.SaveRound("", "github", "dns", in); err != nil {
		t.Fatalf("空候选轮也要能落盘: %v", err)
	}
	rounds, _ := s.Rounds(0)
	cands, err := s.Candidates(rounds[0].ID)
	if err != nil || len(cands) != 0 {
		t.Fatalf("候选数 = %d（err=%v），期望 0", len(cands), err)
	}
}

func TestSaveNilResultRejected(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveRound("", "", "", nil); err == nil {
		t.Fatalf("空结果该被拒收")
	}
}

func TestPruneKeepsNewestRounds(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < keepRounds+30; i++ {
		if err := s.SaveRound("", "github", "dns", sampleResult()); err != nil {
			t.Fatalf("第 %d 轮落盘失败: %v", i, err)
		}
	}
	rounds, err := s.Rounds(0)
	if err != nil {
		t.Fatalf("读轮列表失败: %v", err)
	}
	if len(rounds) != keepRounds {
		t.Fatalf("清理后轮数 = %d，期望 %d", len(rounds), keepRounds)
	}
	// 最老的一批被删了，它们的候选必须跟着走（级联），不留孤儿行。
	var orphans int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM detect_candidates
		 WHERE round_row NOT IN (SELECT id FROM detect_rounds)`).Scan(&orphans); err != nil {
		t.Fatalf("数孤儿行失败: %v", err)
	}
	if orphans != 0 {
		t.Fatalf("还有 %d 条孤儿候选，级联删除没生效", orphans)
	}
}

func TestCandidatesOfMissingRound(t *testing.T) {
	s := newTestStore(t)
	cands, err := s.Candidates(9999)
	if err != nil || len(cands) != 0 {
		t.Fatalf("不存在的轮该给空列表，实际 %d（err=%v）", len(cands), err)
	}
}

// 候选池按「主机名 + IP」去重：同一个 IP 跨轮再写只更新那一行，不堆积；
// 轮底账则照实每轮一行，两份数据各管各的。
func TestPoolDedupesAcrossRounds(t *testing.T) {
	s := newTestStore(t)
	first := sampleResult()
	if err := s.SaveRound("r1", "github", "gitcdn", first); err != nil {
		t.Fatalf("第一轮落盘失败: %v", err)
	}

	// 第二轮：同一个 IP 又出现，延迟变了，另有一个新 IP。
	second := sampleResult()
	second.Candidates[0].LatencyMS = 88
	second.Candidates = append(second.Candidates,
		accelerator.Candidate{ID: "c4", Hostname: "github.com", IP: "4.4.4.4", Status: accelerator.StatusAvailable})
	if err := s.SaveRound("r2", "github", "all-sources", second); err != nil {
		t.Fatalf("第二轮落盘失败: %v", err)
	}

	pool, err := s.Pool("github.com")
	if err != nil {
		t.Fatalf("读候选池失败: %v", err)
	}
	// 140.82.121.4 / 2606:...（v6）/ 20.205.243.166 / 4.4.4.4，重复的 140.82.121.4 只有一行。
	if len(pool) != 4 {
		t.Fatalf("候选池该是 4 行（去重后），实际 %d：%v", len(pool), pool)
	}
	var updated *PoolRow
	for i := range pool {
		if pool[i].IP == "140.82.121.4" {
			updated = &pool[i]
		}
	}
	if updated == nil || updated.LatencyMS != 88 || updated.SourceID != "all-sources" {
		t.Errorf("重复 IP 该被第二轮的结果更新，实际 %+v", updated)
	}
	if updated.UpdatedAt != "2026-10-31T08:00:05Z" {
		t.Errorf("更新时间该取这轮收尾时刻，实际 %q", updated.UpdatedAt)
	}

	// 池按主机名过滤：只有 github.com 的查询不给别的域名的行。
	for _, p := range pool {
		if p.Hostname != "github.com" {
			t.Fatalf("按主机名过滤后不该混进其他域名：%+v", p)
		}
	}

	// 底账不受池的去重影响：两轮各自的候选行都在。
	rounds, _ := s.Rounds(0)
	if len(rounds) != 2 {
		t.Fatalf("底账该有 2 轮，实际 %d", len(rounds))
	}
	all, err := s.Pool("")
	if err != nil || len(all) != 4 {
		t.Fatalf("不过滤的池该是同 4 行（err=%v）", err)
	}
}

// 池里的状态是最后一次探测的结论：同一 IP 从可用变超时，池里要跟着变。
func TestPoolTracksLatestStatus(t *testing.T) {
	s := newTestStore(t)
	good := sampleResult()
	if err := s.SaveRound("r1", "github", "gitcdn", good); err != nil {
		t.Fatal(err)
	}
	bad := sampleResult()
	for i := range bad.Candidates {
		bad.Candidates[i].Status = accelerator.StatusTimeout
	}
	if err := s.SaveRound("r2", "github", "gitcdn", bad); err != nil {
		t.Fatal(err)
	}
	pool, _ := s.Pool("github.com")
	for _, p := range pool {
		if p.Status != string(accelerator.StatusTimeout) {
			t.Errorf("%s 最后一次是超时，池里该记 timeout，实际 %s", p.IP, p.Status)
		}
	}
}
