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

// 候选池是缓存语义（拍板 2026-10-31）：新一轮覆盖到的主机名，旧地址先清再写——
// 池里只有最近一轮的结果；轮底账则照实每轮一行，历史不丢。
func TestPoolReplacesOnNewRound(t *testing.T) {
	s := newTestStore(t)
	first := sampleResult()
	if err := s.SaveRound("r1", "github", "gitcdn", first); err != nil {
		t.Fatalf("第一轮落盘失败: %v", err)
	}

	// 第二轮：老 IP 20.205.243.166 不再出现，换成 4.4.4.4；140.82.121.4 还在、延迟变了。
	second := sampleResult()
	second.Candidates[0].LatencyMS = 88
	second.Candidates = append(second.Candidates[:1],
		accelerator.Candidate{ID: "c4", Hostname: "github.com", IP: "4.4.4.4", Status: accelerator.StatusAvailable})
	if err := s.SaveRound("r2", "github", "all-sources", second); err != nil {
		t.Fatalf("第二轮落盘失败: %v", err)
	}

	pool, err := s.Pool("github.com")
	if err != nil {
		t.Fatalf("读候选池失败: %v", err)
	}
	// 只剩这一轮的 140.82.121.4 和 4.4.4.4；上一轮的 20.205.243.166 和 v6 已被清掉。
	if len(pool) != 2 {
		t.Fatalf("缓存语义下池该只有本轮 2 行，实际 %d：%v", len(pool), pool)
	}
	if pool[0].IP != "140.82.121.4" || pool[0].LatencyMS != 88 || pool[0].SourceID != "all-sources" {
		t.Errorf("留存的行该带本轮的结果，实际 %+v", pool[0])
	}
	if pool[1].IP != "4.4.4.4" {
		t.Errorf("新地址该在池里，实际 %+v", pool[1])
	}

	// 底账不受影响：两轮各自的候选行都在，历史照实记。
	rounds, _ := s.Rounds(0)
	if len(rounds) != 2 {
		t.Fatalf("底账该有 2 轮，实际 %d", len(rounds))
	}
	first1, err := s.Candidates(rounds[1].ID)
	if err != nil || len(first1) != 3 {
		t.Fatalf("第一轮底账该还有 3 行（err=%v）", err)
	}
}

// 这一轮覆盖到、但没查出候选的主机名，旧缓存同样清掉：检测过就是最新事实。
func TestPoolClearsHostnamesWithoutCandidates(t *testing.T) {
	s := newTestStore(t)
	first := sampleResult()
	if err := s.SaveRound("r1", "github", "gitcdn", first); err != nil {
		t.Fatal(err)
	}
	empty := sampleResult()
	empty.Candidates = nil
	empty.Hostnames = []accelerator.HostOutcome{{Hostname: "github.com"}}
	if err := s.SaveRound("r2", "github", "gitcdn", empty); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool("github.com")
	if err != nil || len(pool) != 0 {
		t.Fatalf("没查出候选的域名旧缓存该被清空，实际 %d 行（err=%v）", len(pool), err)
	}
}

// 没被这一轮覆盖到的主机名不受影响：清的是「对应」的旧 IP，不是全库。
func TestPoolKeepsUntouchedHostnames(t *testing.T) {
	s := newTestStore(t)
	other := sampleResult()
	other.ItemID = "gh-cdn"
	other.Hostnames = []accelerator.HostOutcome{{Hostname: "github.githubassets.com"}}
	other.Candidates = []accelerator.Candidate{{ID: "x1", Hostname: "github.githubassets.com", IP: "5.5.5.5", Status: accelerator.StatusAvailable}}
	if err := s.SaveRound("r0", "github", "gitcdn", other); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRound("r1", "github", "gitcdn", sampleResult()); err != nil {
		t.Fatal(err)
	}
	pool, err := s.Pool("github.githubassets.com")
	if err != nil || len(pool) != 1 || pool[0].IP != "5.5.5.5" {
		t.Fatalf("别的域名的缓存不该被波及，实际 %d 行（err=%v）", len(pool), err)
	}
}

// 池里的状态是这一轮探测的结论：写进去什么就是什么。
func TestPoolTracksLatestStatus(t *testing.T) {
	s := newTestStore(t)
	bad := sampleResult()
	for i := range bad.Candidates {
		bad.Candidates[i].Status = accelerator.StatusTimeout
	}
	if err := s.SaveRound("r1", "github", "gitcdn", bad); err != nil {
		t.Fatal(err)
	}
	pool, _ := s.Pool("github.com")
	for _, p := range pool {
		if p.Status != string(accelerator.StatusTimeout) {
			t.Errorf("%s 这轮是超时，池里该记 timeout，实际 %s", p.IP, p.Status)
		}
	}
}
