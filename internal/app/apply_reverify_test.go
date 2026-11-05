package app

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/detector"
	"github.com/xiaokentrl/github-net/internal/store"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 这一组用例盯的是拍板 2026-11-06 那条：点「应用队列」永远不把不能正常加速的地址写进系统 Hosts。
// 探测器全是假件，一条外网请求都不发——判据（连上 + 证书对这个域名有效）由 accelerator.Verdict 定，
// 用例只喂它通或不通的结论。

// probeStub 是应用前复验用的假探测器：按 IP 给预设结论，并记下每个地址被探了几次。
type probeStub struct {
	mu    sync.Mutex
	calls map[string]int
	byIP  map[string]detector.Result
}

func (p *probeStub) probe(_ context.Context, t detector.Target) (detector.Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls == nil {
		p.calls = map[string]int{}
	}
	p.calls[t.IP]++
	if res, ok := p.byIP[t.IP]; ok {
		return res, nil
	}
	return detector.Result{OK: true, Verified: true, LatencyMS: 5}, nil
}

func (p *probeStub) count(ip string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls[ip]
}

func (p *probeStub) total() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, c := range p.calls {
		n += c
	}
	return n
}

// passProbe 给一个「全都当场验通」的探测器。
func passProbe() *probeStub { return &probeStub{} }

// failProbe 给一个「当场连不通」的探测器（连 TCP 都没过）。
func failProbe(ips ...string) *probeStub {
	by := make(map[string]detector.Result, len(ips))
	for _, ip := range ips {
		by[ip] = detector.Result{Kind: detector.FailUnreachable, Detail: "网络不可达", LatencyMS: 300}
	}
	return &probeStub{byIP: by}
}

// runApply 排一项应用并等它落终态，返回那条任务。
func runApply(t *testing.T, svc *Service, itemID string) task.Task {
	t.Helper()
	tasks, err := svc.ApplyItems([]string{itemID})
	if err != nil {
		t.Fatalf("应用该排进队列: %v", err)
	}
	waitFor(t, "应用任务落终态", func() bool {
		item, ok := findTask(svc.GetTasks(), tasks[0].ID)
		return ok && task.IsTerminal(item.Status)
	})
	item, ok := findTask(svc.GetTasks(), tasks[0].ID)
	if !ok {
		t.Fatalf("队列里找不到那条应用任务")
	}
	return item
}

// 刚测出来的结论算新鲜：点应用一次网络都不发，直接写。
func TestApplyFreshResultWritesWithoutReverify(t *testing.T) {
	priv := &fakePriv{}
	svc := newApplyService(t, priv)
	stub := passProbe()
	svc.applyProbe = stub.probe
	putAvailableResult(svc, "api")

	item := runApply(t, svc, "api")
	if item.Status != task.StatusDone {
		t.Fatalf("新鲜的结论该直接应用成功，实际 %+v", item)
	}
	if stub.total() != 0 {
		t.Errorf("窗口内不该发任何复验请求，实际探了 %d 次", stub.total())
	}
	if len(priv.applied) != 1 || priv.applied[0].IP != "203.0.113.11" {
		t.Fatalf("该写延迟最低那颗可用候选: %+v", priv.applied)
	}
}

// 放旧了的结论只是候选、不是通行证：当场复验通过才写。
func TestApplyStaleResultReverifiedBeforeWrite(t *testing.T) {
	priv := &fakePriv{}
	svc := newApplyService(t, priv)
	stub := passProbe()
	svc.applyProbe = stub.probe
	putAvailableResultAged(svc, "api", applyFreshWindow+time.Minute)

	item := runApply(t, svc, "api")
	if item.Status != task.StatusDone {
		t.Fatalf("复验通过该应用成功，实际 %+v", item)
	}
	// 每个域名只挑一条（延迟最低那条），所以只探那一个地址。
	if stub.count("203.0.113.11") != 1 {
		t.Fatalf("选中的地址该被现场复验一次，实际调用 %v", stub.calls)
	}
	if stub.count("203.0.113.10") != 0 {
		t.Errorf("没被选中的候选不该跟着探: %v", stub.calls)
	}
	if len(priv.applied) != 1 || priv.applied[0].IP != "203.0.113.11" {
		t.Fatalf("验通了才准写: %+v", priv.applied)
	}
	// 复验的新结论回写内存：定格时间刷成刚刚，界面上的候选跟着变新。
	view, ok := svc.detectResults.view("api")
	if !ok {
		t.Fatal("内存里该有这一轮结果")
	}
	for _, h := range view.Hosts {
		for _, c := range h.Candidates {
			if c.IP == "203.0.113.11" && c.Status != "available" {
				t.Errorf("验通的那条该保持可用: %+v", c)
			}
		}
	}
}

// 当场验不通：一个地址都不写，任务以失败落队列，内存里那条候选跟着改口。
func TestApplyWritesNothingWhenAllReverifyFail(t *testing.T) {
	priv := &fakePriv{}
	svc := newApplyService(t, priv)
	svc.applyProbe = failProbe("203.0.113.11").probe
	putAvailableResultAged(svc, "api", applyFreshWindow+time.Minute)

	item := runApply(t, svc, "api")
	if item.Status != task.StatusFailed {
		t.Fatalf("全都验不通该以失败落定，实际 %+v", item)
	}
	if item.ErrorMsg != "候选地址现在都连不通，先重新检测再应用" {
		t.Errorf("失败原因不对: %q", item.ErrorMsg)
	}
	if priv.appliedID != "" || len(priv.applied) != 0 {
		t.Fatalf("一个地址都不该写进系统 Hosts，实际收到 %+v（项目 %q）", priv.applied, priv.appliedID)
	}
	// 回写：被复验的那条候选当场改口，没被选中的那条不动（它这次的结论没人验过）。
	view, _ := svc.detectResults.view("api")
	for _, h := range view.Hosts {
		for _, c := range h.Candidates {
			switch c.IP {
			case "203.0.113.11":
				if c.Status == "available" {
					t.Errorf("验不通的候选还挂着可用: %+v", c)
				}
				if c.Reason == "" {
					t.Errorf("验不通的候选该留下一句原因: %+v", c)
				}
			case "203.0.113.10":
				if c.Status != "available" {
					t.Errorf("没被选中、这轮没复验过的候选不该被改口: %+v", c)
				}
			}
		}
	}
}

// 来自候选池的结论同样要回写：验不通就当场把那一行标成不可用，
// 下一次应用不会再去挑同一个废地址。
func TestApplyReverifyWritesBackPoolRow(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "detect-history.db"))
	if err != nil {
		t.Fatalf("候选底账打不开：%v", err)
	}
	defer db.Close()
	priv := &fakePriv{}
	svc := newApplyService(t, priv)
	svc.candidateStore = db
	// 内存里没有这一轮：可用的地址只存在于池里，那条 pick 必然标着 fromPool。
	if err := db.SaveRound("round-seed", detectScopeGitHub, detectMethodGitHub, &accelerator.Result{
		ItemID:     "api",
		FinishedAt: time.Now().Add(-30 * time.Minute),
		Available:  1,
		Candidates: []accelerator.Candidate{{ID: "p1", Hostname: detectHostAPI, IP: "203.0.113.11", Status: accelerator.StatusAvailable, LatencyMS: 40}},
		Hostnames:  []accelerator.HostOutcome{{Hostname: detectHostAPI, Resolved: true, Available: 1}},
	}); err != nil {
		t.Fatalf("底账写不进去：%v", err)
	}
	svc.applyProbe = failProbe("203.0.113.11").probe

	item := runApply(t, svc, "api")
	if item.Status != task.StatusFailed {
		t.Fatalf("池里的旧结论验不通该以失败落定，实际 %+v", item)
	}
	if len(priv.applied) != 0 {
		t.Fatalf("一个地址都不该写，实际 %+v", priv.applied)
	}
	rows, err := db.Pool(detectHostAPI)
	if err != nil {
		t.Fatalf("读池失败：%v", err)
	}
	if len(rows) != 1 || rows[0].Status == string(accelerator.StatusAvailable) {
		t.Fatalf("验不通的池行该改口，实际 %+v", rows)
	}
}

// 有的域名当场验通、有的验不通：只写通的，任务照样算应用成功（拍板：不做过多解释）。
func TestApplyWritesOnlyVerifiedHostnames(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "detect-history.db"))
	if err != nil {
		t.Fatalf("候选底账打不开：%v", err)
	}
	defer db.Close()
	priv := &fakePriv{}
	svc := newApplyService(t, priv)
	svc.candidateStore = db
	// 内存那一轮过期，只剩 api.github.com 一条候选；codeload.github.com 由池补一条过期候选。
	putAvailableResultAged(svc, "api", applyFreshWindow+time.Minute)
	svc.detectResults.put(detectScopeGitHub, "live.one", &accelerator.Result{
		ItemID:     "api",
		FinishedAt: time.Now().Add(-30 * time.Minute),
		Available:  1,
		Candidates: []accelerator.Candidate{
			{ID: "c2", Hostname: detectHostAPI, IP: "203.0.113.11", Status: accelerator.StatusAvailable, LatencyMS: 40},
		},
		Hostnames: []accelerator.HostOutcome{{Hostname: detectHostAPI, Resolved: true, Available: 1}},
	})
	if err := db.SaveRound("round-seed", detectScopeGitHub, detectMethodGitHub, &accelerator.Result{
		ItemID:     "api",
		FinishedAt: time.Now().Add(-40 * time.Minute),
		Available:  1,
		Candidates: []accelerator.Candidate{
			{ID: "p2", Hostname: detectHostCodeload, IP: "203.0.113.13", Status: accelerator.StatusAvailable, LatencyMS: 60},
		},
		Hostnames: []accelerator.HostOutcome{{Hostname: detectHostCodeload, Resolved: true, Available: 1}},
	}); err != nil {
		t.Fatalf("底账写不进去：%v", err)
	}
	svc.applyProbe = failProbe("203.0.113.13").probe

	item := runApply(t, svc, "api")
	if item.Status != task.StatusDone {
		t.Fatalf("有一个域名验通就该写下去并算成功，实际 %+v", item)
	}
	if len(priv.applied) != 1 || priv.applied[0].IP != "203.0.113.11" {
		t.Fatalf("只该写当场验通的那条: %+v", priv.applied)
	}
	for _, m := range priv.applied {
		for _, h := range m.Hostnames {
			if h == detectHostCodeload {
				t.Errorf("验不通的域名被写进去了: %+v", m)
			}
		}
	}
}

// 时效判定本身：窗口内算新、超窗口算旧、时间戳读不懂一律算旧（宁可多验一次）。
func TestUsablePicksFreshnessWindow(t *testing.T) {
	svc := newApplyService(t, &fakePriv{})
	now := time.Now().UTC()
	cases := []struct {
		what      string
		finished  time.Time
		wantFresh bool
	}{
		{"窗口内一刻", now.Add(-applyFreshWindow + time.Minute), true},
		{"刚好超窗口一刻", now.Add(-applyFreshWindow - time.Minute), false},
		{"时间说不清（零值）", time.Time{}, false},
	}
	for _, tc := range cases {
		svc.detectResults.put(detectScopeGitHub, "live.one", &accelerator.Result{
			ItemID:     "api",
			FinishedAt: tc.finished,
			Available:  1,
			Candidates: []accelerator.Candidate{{ID: "c1", Hostname: detectHostAPI, IP: "203.0.113.11", Status: accelerator.StatusAvailable, LatencyMS: 40}},
			Hostnames:  []accelerator.HostOutcome{{Hostname: detectHostAPI, Resolved: true, Available: 1}},
		})
		picks := svc.usablePicksAt("api", now)
		if len(picks) != 1 {
			t.Fatalf("%s：该挑出一条候选，实际 %+v", tc.what, picks)
		}
		if picks[0].fresh != tc.wantFresh {
			t.Errorf("%s：fresh=%v，期望 %v", tc.what, picks[0].fresh, tc.wantFresh)
		}
	}
}

// 池里那行的时间戳刚写完该算新鲜；一句读不懂的时间戳只能当旧，不能免检写盘。
func TestPoolPickFreshness(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "detect-history.db"))
	if err != nil {
		t.Fatalf("候选底账打不开：%v", err)
	}
	defer db.Close()
	svc := newApplyService(t, &fakePriv{})
	svc.candidateStore = db
	if err := db.SaveRound("round-seed", detectScopeGitHub, detectMethodGitHub, &accelerator.Result{
		ItemID:     "api",
		FinishedAt: time.Now(),
		Available:  1,
		Candidates: []accelerator.Candidate{{ID: "p1", Hostname: detectHostCodeload, IP: "203.0.113.13", Status: accelerator.StatusAvailable, LatencyMS: 60}},
		Hostnames:  []accelerator.HostOutcome{{Hostname: detectHostCodeload, Resolved: true, Available: 1}},
	}); err != nil {
		t.Fatalf("底账写不进去：%v", err)
	}
	var got *usablePick
	picks := svc.usablePicksAt("api", time.Now())
	for i := range picks {
		if picks[i].hostname == detectHostCodeload {
			got = &picks[i]
		}
	}
	if got == nil {
		t.Fatalf("池里的可用地址该补进候选，实际 %+v", picks)
	}
	if !got.fromPool {
		t.Errorf("这条该标出来自池: %+v", *got)
	}
	if !got.fresh {
		t.Errorf("刚写的池行该算新鲜: %+v", *got)
	}
	// 同一条池行放到窗口外：只剩「旧」，必须现场复验。
	if p := svc.usablePicksAt("api", time.Now().Add(applyFreshWindow+time.Minute)); len(p) == 1 && p[0].fresh {
		t.Errorf("超过窗口的池行不该再免检: %+v", p[0])
	}
}

// 落盘时间读不懂（空串、格式不对、只有日期）一律按不新鲜处理。
func TestWithinFreshWindowRejectsUnreadableStamps(t *testing.T) {
	now := time.Now().UTC()
	for _, stamp := range []string{"", "   ", "2026-11-06", "不是时间"} {
		if withinFreshWindow(stamp, now) {
			t.Errorf("读不懂的时间戳 %q 被判成新鲜", stamp)
		}
	}
	if !withinFreshWindow(now.Add(-time.Minute).Format(time.RFC3339), now) {
		t.Error("一分钟前的合规时间该算新鲜")
	}
	if withinFreshWindow(now.Add(-applyFreshWindow-time.Minute).Format(time.RFC3339), now) {
		t.Error("超过窗口的合规时间不该算新鲜")
	}
}
