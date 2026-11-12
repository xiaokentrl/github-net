package store

import (
	"database/sql"
	"path/filepath"
	"strings"
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
	// 用例里的样本轮都收在 2026-10-31T08:00:05Z，清理用的时钟放在它之后一天多：
	// 这样「保留期限」这条不会顺手把用例刚写进去的东西删掉，期限两侧的行为由各条用例自己挪时钟验。
	s.useClock(func() time.Time { return time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) })
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

// 这一轮查出的候选全不可用的主机名，旧缓存原样保留（拍板 2026-11-03：
// 检测只是采集，旧可用地址仍是最优已知，一轮空结果不构成丢掉它的理由）。
func TestPoolKeepsStaleWhenRoundFindsNothing(t *testing.T) {
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
	if err != nil || len(pool) != 3 {
		t.Fatalf("空结果不该动旧缓存，实际 %d 行（err=%v）", len(pool), err)
	}
	// 三行原样保留：两条可用 + 一条超时，都是第一轮的原样。
	for _, want := range []struct{ ip, status string }{
		{"140.82.121.4", "available"},
		{"2606:50c0:8000::153", "available"},
		{"20.205.243.166", "timeout"},
	} {
		found := false
		for _, p := range pool {
			if p.IP == want.ip {
				found = true
				if p.Status != want.status {
					t.Errorf("%s 的状态该是 %s，实际 %s", want.ip, want.status, p.Status)
				}
			}
		}
		if !found {
			t.Errorf("第一轮的 %s 该保留在池里", want.ip)
		}
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

// 有可用候选的域名才整块替换：同域名新一轮查出可用 + 不可用混合时，池里是新一轮的全量
// （可用的排前）；全程超时的探测不写池（没有可用就不覆盖旧缓存）。
func TestPoolTracksLatestStatus(t *testing.T) {
	s := newTestStore(t)
	mixed := sampleResult()
	// 第一条改成超时，其余保留可用：该域名本轮有可用 → 整块替换。
	mixed.Candidates[0].Status = accelerator.StatusTimeout
	if err := s.SaveRound("r1", "github", "gitcdn", mixed); err != nil {
		t.Fatal(err)
	}
	pool, _ := s.Pool("github.com")
	if len(pool) != 3 {
		t.Fatalf("有可用就该整块替换成 3 行，实际 %d", len(pool))
	}
	// 顺序按 hostname+ip 排：140.82.121.4(超时) / 20.205.243.166(超时) / v6(可用)。
	if pool[0].IP != "140.82.121.4" || pool[0].Status != "timeout" {
		t.Errorf("第一条该是超时的 140.82.121.4，实际 %+v", pool[0])
	}
	if pool[2].IP != "2606:50c0:8000::153" || pool[2].Status != "available" {
		t.Errorf("v6 可用照旧记录，实际 %+v", pool[2])
	}

	// 全程超时的一轮：不覆盖旧缓存，第一轮的行原样都在。
	if err := s.SaveRound("r2", "github", "gitcdn", badRound()); err != nil {
		t.Fatal(err)
	}
	pool, _ = s.Pool("github.com")
	if len(pool) != 3 {
		t.Fatalf("全程超时不该改变池，实际 %d 行", len(pool))
	}
	for _, p := range pool {
		if p.UpdatedAt != "2026-10-31T08:00:05Z" {
			t.Errorf("全程超时的一轮不该写池，实际 %+v", p)
		}
	}
}

// badRound 造一轮全部超时的结果。
func badRound() *accelerator.Result {
	bad := sampleResult()
	for i := range bad.Candidates {
		bad.Candidates[i].Status = accelerator.StatusTimeout
	}
	return bad
}

// 保留期限这条（拍板 2026-11-05：任何缓存最多存 3 天）：超期的轮整轮删（轮下候选随外键级联一起走，
// 不留孤儿行），超期没再测过的地址行也删。样本轮收在 2026-10-31T08:00:05Z，
// 时钟放在正好三天后（一秒不差）时东西必须还在，再晚一秒就必须没了——两侧都钉住，
// 免得把期限写成两天或四天时用例还是绿的。
func TestPruneDeletesBeyondRetentionAge(t *testing.T) {
	s := newTestStore(t)
	s.useClock(func() time.Time { return time.Date(2026, 11, 3, 8, 0, 5, 0, time.UTC) })
	if err := s.SaveRound("r1", "github", "gitcdn", sampleResult()); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	if rounds, err := s.Rounds(0); err != nil || len(rounds) != 1 {
		t.Fatalf("正好 3 天的一轮不该被删，实际 %d 轮（err=%v）", len(rounds), err)
	}
	if pool, err := s.Pool("github.com"); err != nil || len(pool) != 3 {
		t.Fatalf("正好 3 天的地址行不该被删，实际 %d 行（err=%v）", len(pool), err)
	}

	s.useClock(func() time.Time { return time.Date(2026, 11, 3, 8, 0, 6, 0, time.UTC) })
	if err := s.Prune(); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if rounds, _ := s.Rounds(0); len(rounds) != 0 {
		t.Errorf("超过 3 天的轮该删干净，实际还剩 %d 轮", len(rounds))
	}
	if pool, _ := s.Pool("github.com"); len(pool) != 0 {
		t.Errorf("超过 3 天的地址行该删干净，实际还剩 %d 行", len(pool))
	}
	var left int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM detect_candidates`).Scan(&left); err != nil {
		t.Fatalf("数候选行失败: %v", err)
	}
	if left != 0 {
		t.Errorf("轮删了候选行必须跟着走，实际还剩 %d 行", left)
	}
}

// 只有超期的那批走，界内的原样留着：清理不是「一把清空重来」。
func TestPruneKeepsRoundsWithinRetentionAge(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveRound("r-old", "github", "gitcdn", sampleResult()); err != nil {
		t.Fatalf("第一轮落盘失败: %v", err)
	}
	fresh := sampleResult()
	fresh.FinishedAt = time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	fresh.StartedAt = fresh.FinishedAt
	fresh.Candidates = []accelerator.Candidate{
		{ID: "n1", Hostname: "cdn.test", IP: "9.9.9.9", Status: accelerator.StatusAvailable, LatencyMS: 11},
	}
	fresh.Hostnames = []accelerator.HostOutcome{{Hostname: "cdn.test", Resolved: true}}
	if err := s.SaveRound("r-new", "github", "gitcdn", fresh); err != nil {
		t.Fatalf("第二轮落盘失败: %v", err)
	}

	// 时钟放在 r-old 超期、r-new 还在界内的这一刻。
	s.useClock(func() time.Time { return time.Date(2026, 11, 3, 8, 59, 0, 0, time.UTC) })
	if err := s.Prune(); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	rounds, err := s.Rounds(0)
	if err != nil || len(rounds) != 1 || rounds[0].TaskRound != "r-new" {
		t.Fatalf("只该留下没超期的那一轮，实际 %+v（err=%v）", rounds, err)
	}
	if pool, _ := s.Pool("github.com"); len(pool) != 0 {
		t.Errorf("超期域名的地址行该删掉，实际还剩 %d 行", len(pool))
	}
	if pool, err := s.Pool("cdn.test"); err != nil || len(pool) != 1 || pool[0].IP != "9.9.9.9" {
		t.Errorf("界内域名的地址行该留着，实际 %v（err=%v）", pool, err)
	}
}

// 版本守卫：新程序建的库能正常重开并钉上版本；库比程序新时拒绝打开——
// 硬读只会得到 schema 对不上的怪错，不如当场说清楚原因。
func TestSchemaVersionGuard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "detect.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("开库失败: %v", err)
	}
	s.Close()

	// 重开：版本已钉到 schemaVersion，应该畅通。
	if s, err = Open(path); err != nil {
		t.Fatalf("同版本库重开失败: %v", err)
	}
	s.Close()

	// 把版本拨到将来：模拟「这台机器跑过更新的版本」，旧程序必须拒绝。
	upgrade := func() {
		db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
		if err != nil {
			t.Fatalf("拨版本开库失败: %v", err)
		}
		defer db.Close()
		if _, err := db.Exec(`PRAGMA user_version = 99`); err != nil {
			t.Fatalf("拨版本失败: %v", err)
		}
	}
	upgrade()
	if _, err := Open(path); err == nil {
		t.Fatal("库比程序新时应该拒绝打开")
	} else if !strings.Contains(err.Error(), "拒绝打开") {
		t.Fatalf("错误里该写明拒绝原因，实际 %v", err)
	}
}
