package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/detector"
	"github.com/xiaokentrl/github-net/internal/hosts"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 这一层全部跑在假接缝里：取候选和验候选都是注入的假件，一次真实 DNS 查询、
// 一个真实 TCP/TLS 连接都不会发出去。候选地址一律用文档保留段。

const (
	detectScopeGitHub  = "github"
	detectScopeSteam   = "steam"
	detectMethodGitHub = "fake-detect-github"
	detectMethodBoth   = "fake-detect-both"
	detectMethodSteam  = "fake-detect-steam"

	detectHostAPI      = "api.github.com"
	detectHostCodeload = "codeload.github.com"
	detectHostSteam    = "store.example.test"
)

// detectCatalog 是这一层要用的目录形状：一个项目把同一个域名写成三种样子（去重要在这里生效），
// 一个项目没配域名，另一个项目在别的加速范围里。
func detectCatalog() *catalog.Catalog {
	return &catalog.Catalog{Categories: []catalog.Category{
		{ID: "code", Name: "代码与下载", ScopeID: detectScopeGitHub, Items: []catalog.Item{
			{ID: "api", Name: "API", Hostnames: []string{"API.GitHub.com.", "api.github.com", " codeload.github.com "}},
			{ID: "bare", Name: "没有域名的项目", Hostnames: nil},
		}},
		{ID: "store", Name: "商店", ScopeID: detectScopeSteam, Items: []catalog.Item{
			{ID: "steam", Name: "商店", Hostnames: []string{detectHostSteam}},
		}},
	}}
}

// detectLookup 是取候选的假件：记下每个方法被谁调过、拿到了什么候选上限，返回预设的候选。
type detectLookup struct {
	mu       sync.Mutex
	hosts    []string
	methods  []string
	maxSeen  []int
	cands    map[string][]strategies.Candidate
	failWith error
}

func (l *detectLookup) strategy(id string, scopes []string) strategies.Strategy {
	return strategies.Strategy{
		ID:          id,
		ScopeIDs:    scopes,
		Name:        "假方法",
		Description: "只用于测试：不发查询，只交出预设的候选地址。",
		Caps: strategies.CapabilityCandidateLookup |
			strategies.CapabilityKeepsTLSHostname |
			strategies.CapabilitySystemDNSOnly,
		Lookup: func(_ context.Context, in strategies.Input) (strategies.Output, error) {
			l.mu.Lock()
			l.hosts = append(l.hosts, in.Hostname)
			l.methods = append(l.methods, id)
			l.maxSeen = append(l.maxSeen, in.Max)
			l.mu.Unlock()
			if l.failWith != nil {
				return strategies.Output{}, l.failWith
			}
			all := l.cands[in.Hostname]
			out := strategies.Output{Candidates: all}
			if len(all) > in.Max {
				out.Candidates = all[:in.Max]
				out.Truncated = true
			}
			return out, nil
		},
	}
}

// table 把这几个假方法组成一张只给测试用的解析表。
func (l *detectLookup) table(items ...strategies.Strategy) func(string) (strategies.Strategy, bool) {
	return func(id string) (strategies.Strategy, bool) {
		for _, item := range items {
			if item.ID == id {
				return item, true
			}
		}
		return strategies.Strategy{}, false
	}
}

func (l *detectLookup) calls() (hosts, methods []string, maxes []int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.hosts...), append([]string(nil), l.methods...), append([]int(nil), l.maxSeen...)
}

func (l *detectLookup) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hosts, l.methods, l.maxSeen = nil, nil, nil
}

// detectReports 收队列那一行的进度汇报。
type detectReports struct {
	mu    sync.Mutex
	calls []detectReportCall
}

type detectReportCall struct {
	Phase   string
	Text    string
	Percent int
}

func (r *detectReports) report(phase, text string, percent int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, detectReportCall{Phase: phase, Text: text, Percent: percent})
}

func (r *detectReports) snapshot() []detectReportCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]detectReportCall(nil), r.calls...)
}

// availableProbe 把每条候选都判成可用：连上并且证书对这个主机名有效。
func availableProbe(_ context.Context, _ detector.Target) (detector.Result, error) {
	return detector.Result{OK: true, Verified: true, LatencyMS: 12, TLSMS: 20}, nil
}

// refusedProbe 把每条候选都判成连不上，用来跑「一轮跑完了但一个可用地址都没有」。
func refusedProbe(_ context.Context, _ detector.Target) (detector.Result, error) {
	return detector.Result{Kind: detector.FailRefused, Detail: "对端明确拒绝连接"}, nil
}

func detectStore(t *testing.T, defaults map[string]string) *config.Store {
	t.Helper()
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"),
		map[string]bool{detectScopeGitHub: true, detectScopeSteam: true},
		map[string]bool{detectMethodGitHub: true, detectMethodBoth: true, detectMethodSteam: true})
	if defaults != nil {
		if _, err := store.Save(config.Patch{DefaultStrategyByScope: &defaults}, 0, "detect-test-defaults"); err != nil {
			t.Fatalf("写入默认加速方法失败：%v", err)
		}
	}
	return store
}

// newDetectExecutorForTest 用真实构造函数建，再由用例把方法解析换成测试自己的表：
// 假方法不进全局注册表，免得重复注册把装配层那道注册门照脏。
// 结果存储每个用例新给一份；事件发布器给 nil，测试不用总线；已知地址默认也给 nil（不补「当前生效」那一行），
// 要用例关心它时再改 exec.appliedIPs。
func newDetectExecutorForTest(t *testing.T, store *config.Store, probe accelerator.ProbeFunc) *detectExecutor {
	t.Helper()
	return newDetectExecutor(detectCatalog(), store, probe, newDetectResultStore(), nil, nil, nil, nil, nil)
}

func detectCandidates() map[string][]strategies.Candidate {
	return map[string][]strategies.Candidate{
		detectHostAPI:      {{IP: "203.0.113.10"}, {IP: "203.0.113.11"}},
		detectHostCodeload: {{IP: "203.0.113.12"}, {IP: "203.0.113.13"}},
		detectHostSteam:    {{IP: "203.0.113.20"}},
	}
}

func TestDetectExecutorRunsOneRoundAndReportsProgress(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates()}
	exec := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	exec.strategyFor = lookup.table(
		lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}),
		lookup.strategy(detectMethodBoth, []string{detectScopeGitHub, detectScopeSteam}),
	)
	reports := &detectReports{}

	out := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodGitHub},
		reports.report)

	if out.Status != task.StatusDone {
		t.Fatalf("一轮跑完且查到了可用地址，该收在完成，实际 %+v", out)
	}
	if out.Message != "" {
		t.Errorf("成功不该带文案，免得界面显示成噪音，实际 %q", out.Message)
	}

	// 同一个域名的三种写法只解析一次，且解析按规范化后的写法发起。
	hosts, methods, maxes := lookup.calls()
	if strings.Join(hosts, ",") != detectHostAPI+","+detectHostCodeload {
		t.Errorf("去重后应该只解析这两个域名，实际 %v", hosts)
	}
	if len(methods) != 2 || methods[0] != detectMethodGitHub || methods[1] != detectMethodGitHub {
		t.Errorf("两次解析都该走任务上指定的方法，实际 %v", methods)
	}
	// 每主机名候选上限来自设置档位：默认省流档是 2。
	limit, ok := config.ProfileCandidateLimit(config.ProfileLow)
	if !ok {
		t.Fatal("省流档没有每主机名候选上限")
	}
	for _, got := range maxes {
		if got != limit {
			t.Errorf("传给加速方法的候选上限该等于档位的 %d，实际 %v", limit, maxes)
			break
		}
	}

	calls := reports.snapshot()
	if len(calls) == 0 {
		t.Fatal("一轮检测至少要往任务那一行汇报进度")
	}
	if calls[0].Phase != accelerator.PhaseResolving {
		t.Errorf("第一条进度该是解析段，实际 %+v", calls[0])
	}
	if last := calls[len(calls)-1]; last.Percent != 100 {
		t.Errorf("所有候选都定格后进度该收在 100，实际 %+v", last)
	}
	var sawProbing bool
	for i, call := range calls {
		if call.Percent < 0 || call.Percent > 100 {
			t.Fatalf("进度比例越界：%+v", call)
		}
		if i > 0 && call.Percent < calls[i-1].Percent {
			t.Fatalf("进度比例不该倒退：%+v 在 %+v 之后", call, calls[i-1])
		}
		if call.Phase == accelerator.PhaseProbing {
			sawProbing = true
		}
	}
	if !sawProbing {
		t.Error("四条候选都定过格，进度里却没有探测段")
	}
	// 任务这一行说的话不带域名和候选地址：它会进界面，域名与地址只在候选结果那一侧出现。
	for _, call := range calls {
		assertNoSensitive(t, call.Text)
	}
	assertNoSensitive(t, out.Message)
}

// 跑完的一轮要交到底账接缝上：轮 ID、范围、方法、结果四样都不许丢。
// 接缝给 nil 就不调（底账打不开的场景），这里顺带验一下不炸。
func TestDetectExecutorHandsRoundToLedger(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates()}
	var saved []string
	save := func(taskRound, scopeID, strategyID string, r *accelerator.Result) {
		saved = append(saved, taskRound+"|"+scopeID+"|"+strategyID+"|"+r.ItemID)
	}
	exec := newDetectExecutor(detectCatalog(), detectStore(t, nil), availableProbe,
		newDetectResultStore(), nil, nil, nil, nil, save)
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))

	out := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub,
			StrategyID: detectMethodGitHub, RoundID: "round-9"},
		func(string, string, int) {})

	if out.Status != task.StatusDone {
		t.Fatalf("这轮该跑完，实际 %+v", out)
	}
	if len(saved) != 1 || saved[0] != "round-9|"+detectScopeGitHub+"|"+detectMethodGitHub+"|api" {
		t.Fatalf("底账该收到恰好一笔且四样齐全，实际 %v", saved)
	}

	// nil 接缝：没底账的装配照跑不落盘，不能炸。
	execNil := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	execNil.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))
	out = execNil.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodGitHub},
		func(string, string, int) {})
	if out.Status != task.StatusDone {
		t.Fatalf("没底账也要能跑完，实际 %+v", out)
	}
}

func TestDetectExecutorRejectsUnknownItemAndEmptyHostnames(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates()}
	exec := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))
	reports := &detectReports{}

	missing := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "not-in-catalog", StrategyID: detectMethodGitHub}, reports.report)
	if missing.Status != task.StatusFailed || missing.Message == "" {
		t.Errorf("目录里没有这个项目要判失败并给一句原因，实际 %+v", missing)
	}

	empty := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "bare", StrategyID: detectMethodGitHub}, reports.report)
	if empty.Status != task.StatusSkipped || empty.Message == "" {
		t.Errorf("没配域名的项目该判跳过并说清为什么，实际 %+v", empty)
	}

	if hosts, _, _ := lookup.calls(); len(hosts) != 0 {
		t.Errorf("这两条任务都不该发起任何解析，实际 %v", hosts)
	}
	if len(reports.snapshot()) != 0 {
		t.Errorf("两轮都没开始，不该有进度汇报，实际 %+v", reports.snapshot())
	}
}

func TestDetectExecutorResolvesStrategyFromTaskThenSettings(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates()}
	store := detectStore(t, map[string]string{detectScopeGitHub: detectMethodGitHub})
	exec := newDetectExecutorForTest(t, store, availableProbe)
	exec.strategyFor = lookup.table(
		lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}),
		lookup.strategy(detectMethodBoth, []string{detectScopeGitHub, detectScopeSteam}),
	)

	// 任务上带了方法就用它，不回头取设置里的默认。
	taskBoth := task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodBoth}
	if out := exec.Execute(context.Background(), taskBoth, (&detectReports{}).report); out.Status != task.StatusDone {
		t.Fatalf("任务指定了方法，该正常跑完，实际 %+v", out)
	}
	_, methods, _ := lookup.calls()
	for _, got := range methods {
		if got != detectMethodBoth {
			t.Fatalf("任务上指定的方法该压过设置里的默认，实际用了 %v", methods)
		}
	}

	// 任务没带方法就用这个范围的默认。
	lookup.reset()
	noStrategy := task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub}
	outcome := exec.Execute(context.Background(), noStrategy, (&detectReports{}).report)
	_, methods, _ = lookup.calls()
	if outcome.Status != task.StatusDone {
		t.Fatalf("用默认方法也该正常收口，实际 %+v", outcome)
	}
	if len(methods) == 0 || methods[0] != detectMethodGitHub {
		t.Errorf("该用设置里这个范围的默认方法，实际 %v", methods)
	}

	// 两边都没有：明确拒绝，绝不从注册表里随便挑一个。
	bare := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	bare.strategyFor = exec.strategyFor
	refused := bare.Execute(context.Background(), noStrategy, (&detectReports{}).report)
	if refused.Status != task.StatusFailed || !strings.Contains(refused.Message, "还没选检测方法") {
		t.Errorf("没选过方法要拒绝并指出去哪儿补，实际 %+v", refused)
	}
}

func TestDetectExecutorRejectsStrategyThatDoesNotFitScope(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates()}
	exec := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	exec.strategyFor = lookup.table(
		lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}),
		lookup.strategy(detectMethodSteam, []string{detectScopeSteam}),
	)

	// 商店项目带着「只适用代码范围」的方法：不能因为它总归能查点东西就照跑。
	out := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "steam", ScopeID: detectScopeSteam, StrategyID: detectMethodGitHub},
		(&detectReports{}).report)
	if out.Status != task.StatusFailed || !strings.Contains(out.Message, "不适用") {
		t.Errorf("方法与范围不匹配要拒绝并说明，实际 %+v", out)
	}
	if hosts, _, _ := lookup.calls(); len(hosts) != 0 {
		t.Errorf("方法不适用范围时一次解析都不该发，实际 %v", hosts)
	}

	// 任务带着注册表里已经没有的方法：同样拒绝，并告诉用户要重选。
	gone := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "steam", ScopeID: detectScopeSteam, StrategyID: "removed-method"},
		(&detectReports{}).report)
	if gone.Status != task.StatusFailed || !strings.Contains(gone.Message, "重新选一个方法") {
		t.Errorf("方法已不在注册表时要拒绝并让用户重选，实际 %+v", gone)
	}
}

func TestDetectExecutorFailsWhenSettingsUnreadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// 高版本配置按规划拒读拒改，这里要的就是那个拒读。
	if err := os.WriteFile(path, []byte(`{"schemaVersion":99,"revision":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := config.NewStore(path, map[string]bool{detectScopeGitHub: true}, map[string]bool{detectMethodGitHub: true})
	lookup := &detectLookup{cands: detectCandidates()}
	exec := newDetectExecutorForTest(t, store, availableProbe)
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))

	out := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodGitHub},
		(&detectReports{}).report)
	if out.Status != task.StatusFailed || !strings.Contains(out.Message, "读不到当前设置") {
		t.Errorf("设置读不到就不能假装跑过一轮，实际 %+v", out)
	}
	if hosts, _, _ := lookup.calls(); len(hosts) != 0 {
		t.Errorf("设置都没读到，一次解析都不该发，实际 %v", hosts)
	}
}

func TestDetectExecutorReportsCancellationAsFailure(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates()}
	var probes int
	var mu sync.Mutex
	exec := newDetectExecutorForTest(t, detectStore(t, nil), func(_ context.Context, _ detector.Target) (detector.Result, error) {
		mu.Lock()
		probes++
		mu.Unlock()
		return availableProbe(context.Background(), detector.Target{})
	})
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := exec.Execute(ctx,
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodGitHub},
		(&detectReports{}).report)

	// 队列在取消那一刻已把任务定成「已取消」，这里的结论不会再改写它；
	// 但单独调这一层的调用方必须拿到不撒谎的结论：取消不是成功。
	if out.Status != task.StatusFailed || !strings.Contains(out.Message, "已取消") {
		t.Errorf("取消的一轮该报失败并说明，实际 %+v", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if probes != 0 {
		t.Errorf("上层已经取消，不该再探测任何候选，实际探了 %d 次", probes)
	}
}

func TestDetectExecutorTreatsNoAvailableCandidateAsConclusion(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates()}
	exec := newDetectExecutorForTest(t, detectStore(t, nil), refusedProbe)
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))
	reports := &detectReports{}

	out := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodGitHub},
		reports.report)
	if out.Status != task.StatusDone {
		t.Errorf("一条可用地址都没查到是这一轮的结论，不是失败，实际 %+v", out)
	}
	calls := reports.snapshot()
	if last := calls[len(calls)-1]; last.Percent != 100 {
		t.Errorf("全部候选都定过格，进度该收在 100，实际 %+v", last)
	}
}

func TestDetectExecutorDoesNotFakeProgressWhenNothingResolves(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates(), failWith: errors.New("解析器出错")}
	exec := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))
	reports := &detectReports{}

	out := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodGitHub},
		reports.report)
	if out.Status != task.StatusDone {
		t.Fatalf("解析全失败也是一轮跑完，实际 %+v", out)
	}
	calls := reports.snapshot()
	if len(calls) == 0 {
		t.Fatal("解析段也要汇报进度，否则界面上这一行是空的")
	}
	for _, call := range calls {
		if call.Percent != 0 {
			t.Fatalf("一个候选都没列出来时不许给出任何比例，实际 %+v", call)
		}
		if strings.Contains(call.Text, "已完成") {
			t.Fatalf("没有候选时不该说已完成，实际 %+v", call)
		}
	}
}

func TestDetectExecutorLeavesApplyAndRestoreUnwired(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates()}
	exec := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))

	for _, kind := range []task.Kind{task.KindApply, task.KindRestore} {
		out := exec.Execute(context.Background(),
			task.Task{Kind: kind, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodGitHub},
			(&detectReports{}).report)
		if out.Status != task.StatusFailed || out.Message != pendingExecutorMessage {
			t.Errorf("%s 任务要给出「还没接上执行动作」那句原话，实际 %+v", kind, out)
		}
	}
	if hosts, _, _ := lookup.calls(); len(hosts) != 0 {
		t.Errorf("应用和恢复一类都不该发起解析，实际 %v", hosts)
	}
}

func TestDetectPhaseTextAndPercent(t *testing.T) {
	cases := []struct {
		progress accelerator.Progress
		phase    string
		text     string
		percent  int
	}{
		{progress: accelerator.Progress{Phase: accelerator.PhaseResolving, Hostname: detectHostAPI},
			phase: accelerator.PhaseResolving, text: "正在解析域名", percent: 0},
		{progress: accelerator.Progress{CandidateID: "x", Status: accelerator.StatusQueued, Total: 0},
			phase: accelerator.PhaseProbing, text: "正在列出候选地址", percent: 0},
		{progress: accelerator.Progress{Phase: accelerator.PhaseProbing, CandidateID: "x", Status: accelerator.StatusProbing, Done: 1, Total: 4},
			phase: accelerator.PhaseProbing, text: "正在检测候选地址（已完成 1/4）", percent: 25},
		{progress: accelerator.Progress{CandidateID: "x", Status: accelerator.StatusAvailable, Done: 4, Total: 4},
			phase: accelerator.PhaseProbing, text: "正在检测候选地址（已完成 4/4）", percent: 100},
	}
	for i, tc := range cases {
		if got := detectPhase(tc.progress); got != tc.phase {
			t.Errorf("第 %d 条阶段不对：%q，要 %q", i+1, got, tc.phase)
		}
		if got := detectPhaseText(tc.progress); got != tc.text {
			t.Errorf("第 %d 条那句话不对：%q，要 %q", i+1, got, tc.text)
		}
		if got := detectPercent(tc.progress); got != tc.percent {
			t.Errorf("第 %d 条比例不对：%d，要 %d", i+1, got, tc.percent)
		}
	}
}

func TestDetectUniqueHostnamesDedupesBeforeBudget(t *testing.T) {
	// 本层去重不是为了少发一次查询（检测编排自己也会去重），而是为了预算算得对：
	// BudgetFor 拿到的域名数决定「这一轮理论上要探几条」和「当前上限盖不盖得住」那个标记，
	// 同一个域名的三种写法数成三条，界面就会误报一轮全量盖不住、要求用户多确认一次。
	got := uniqueHostnames([]string{"API.GitHub.com.", "api.github.com", " codeload.github.com ", "", "   .   ", "api.github.com."})
	if strings.Join(got, ",") != detectHostAPI+","+detectHostCodeload {
		t.Errorf("规范化去重后应该只剩两条、且保持原顺序，实际 %v", got)
	}
}

// appliedFor 从管理区段里挑出这些域名当前写在系统里的地址，作为本轮的已知地址。
// 三种「没有可信答案」都要给 nil：区段判读不了、区段里根本没有这些域名、连区段都没有。
func TestAppliedForReadsManagedBlock(t *testing.T) {
	content := strings.Join([]string{
		"127.0.0.1 localhost",
		hosts.MarkerBegin(),
		"203.0.113.99 " + detectHostAPI,
		"203.0.113.98 " + detectHostCodeload + " " + detectHostSteam,
		hosts.MarkerEnd(),
		"",
	}, "\n")

	got := appliedFor(hosts.Parse(content), []string{detectHostAPI, strings.ToUpper(detectHostCodeload) + "."})
	if len(got) != 2 {
		t.Fatalf("两个域名各一条已知地址，实得 %v", got)
	}
	if strings.Join(got[detectHostAPI], ",") != "203.0.113.99" {
		t.Errorf("%s 的已知地址应为 203.0.113.99，实得 %v", detectHostAPI, got[detectHostAPI])
	}
	if strings.Join(got[detectHostCodeload], ",") != "203.0.113.98" {
		t.Errorf("域名要先规范化再比较：大写带尾点的写法也该对上，实得 %v", got)
	}
	if _, ok := got[detectHostSteam]; ok {
		t.Errorf("本轮没查 %s，它在区段里的地址不该掺进来", detectHostSteam)
	}

	// 区段被外部改过（结构判读不了）：宁可不补这一行，也不给一条可能错的东西打上「当前生效」。
	broken := hosts.Parse(hosts.MarkerBegin() + "\n" + hosts.MarkerBegin() + "\n" +
		"203.0.113.99 " + detectHostAPI + "\n" + hosts.MarkerEnd())
	if out := appliedFor(broken, []string{detectHostAPI}); out != nil {
		t.Errorf("区段判读不了要返回空，实得 %v", out)
	}
	// 判读不了但手里确实攥着几条记录的那种形状（将来别的读路径可能给）：Usable 这一道门要拦得住，
	// 不能因为「反正有记录」就当可信答案用出去。
	carrying := hosts.Block{Present: true, Anomaly: hosts.AnomalyBadRecord, Reason: "区段被外部改过。", Version: hosts.ManagedVersion,
		Records: []hosts.Record{{IP: "203.0.113.99", Hostnames: []string{detectHostAPI}}}}
	if out := appliedFor(carrying, []string{detectHostAPI}); out != nil {
		t.Errorf("区段判读不了、哪怕带着一排记录也不能用，实得 %v", out)
	}
	if out := appliedFor(hosts.Parse("127.0.0.1 localhost"), []string{detectHostAPI}); out != nil {
		t.Errorf("没有管理区段要返回空，实得 %v", out)
	}
	if out := appliedFor(hosts.Parse(content), nil); out != nil {
		t.Errorf("一个域名都没查时不该造出任何已知地址，实得 %v", out)
	}
}

// Service.appliedIPsFor 是线上那一条读路径：只读打开 Hosts，读到就按区段挑，
// 读不到（文件没了、没权限）不报错也不假装，只给空——那种时候界面少一行「当前生效」，不是检测跑不动。
func TestAppliedIPsForReadsHostsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hosts")
	s := &Service{hostsPath: path}

	if got := s.appliedIPsFor([]string{detectHostAPI}); got != nil {
		t.Errorf("Hosts 文件还不存在时不该给出已知地址，实得 %v", got)
	}

	write := func(t *testing.T, lines ...string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(strings.Join(append(lines, ""), "\n")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(t, "127.0.0.1 localhost", hosts.MarkerBegin(), "203.0.113.99 "+detectHostAPI, hosts.MarkerEnd())
	if got := s.appliedIPsFor([]string{detectHostAPI}); strings.Join(got[detectHostAPI], ",") != "203.0.113.99" {
		t.Errorf("区段里那条地址要作为已知地址给出，实得 %v", got)
	}

	write(t, hosts.MarkerBegin(), hosts.MarkerBegin(), "203.0.113.99 "+detectHostAPI, hosts.MarkerEnd())
	if got := s.appliedIPsFor([]string{detectHostAPI}); got != nil {
		t.Errorf("区段判读不了时不给已知地址（宁缺毋滥），实得 %v", got)
	}
}

// 执行器在一轮开始那一刻现读一次已知地址并交给检测编排：
// 界面上那一行「当前生效」由此而来，而且它占名额、照样被探测。
func TestDetectExecutorFeedsAppliedIPsIntoRound(t *testing.T) {
	lookup := &detectLookup{cands: detectCandidates()}
	exec := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))
	var asked []string
	exec.appliedIPs = func(hostnames []string) map[string][]string {
		asked = append([]string(nil), hostnames...)
		return map[string][]string{detectHostAPI: {"203.0.113.99"}}
	}

	out := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodGitHub},
		(&detectReports{}).report)
	if out.Status != task.StatusDone {
		t.Fatalf("一轮跑完该收在完成，实际 %+v", out)
	}
	if strings.Join(asked, ",") != detectHostAPI+","+detectHostCodeload {
		t.Errorf("读已知地址要用去重后的域名，否则同一个域名的三种写法各占一行，实得 %v", asked)
	}

	view, ok := exec.results.view("api")
	if !ok {
		t.Fatal("这一轮的结果该存进结果存储供详情页取")
	}
	var appliedRows, total int
	byHost := map[string]int{}
	for _, h := range view.Hosts {
		for _, c := range h.Candidates {
			total++
			if c.Applied {
				appliedRows++
				byHost[c.Hostname]++
				if c.Status == "" {
					t.Errorf("已生效那一行也要有探测结论，实得 %+v", c)
				}
			}
		}
	}
	if appliedRows != 1 || byHost[detectHostAPI] != 1 {
		t.Errorf("已知地址该有 1 行「当前生效」，实得 %d（%v）", appliedRows, byHost)
	}
	if total != 4 {
		t.Errorf("候选总数 = 已生效 1 条 + 解析 3 条 = 4，实得 %d", total)
	}
	// 名额跟着减：已生效占了 1 格，那次解析的上限从档位的 2 变成 1；没被占的域名仍是 2。
	limit, ok := config.ProfileCandidateLimit(config.ProfileLow)
	if !ok {
		t.Fatal("省流档没有每主机名候选上限")
	}
	hostsCalled, _, maxes := lookup.calls()
	seen := map[string]int{}
	for i, h := range hostsCalled {
		seen[h] = maxes[i]
	}
	if got := seen[detectHostAPI]; got != limit-1 {
		t.Errorf("被已生效占了一格后，传给方法的上限应是 %d，实得 %d", limit-1, got)
	}
	if got := seen[detectHostCodeload]; got != limit {
		t.Errorf("没有已知地址的域名上限不该少，应是 %d，实得 %d", limit, got)
	}

	// 反向：接缝不给已知地址时，一行「当前生效」都不该冒出来。
	lookup.reset()
	bare := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	bare.strategyFor = exec.strategyFor
	if out := bare.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub, StrategyID: detectMethodGitHub},
		(&detectReports{}).report); out.Status != task.StatusDone {
		t.Fatalf("没有已知地址也照样该跑完一轮，实际 %+v", out)
	}
	if _, _, maxes := lookup.calls(); maxes[0] != limit || maxes[1] != limit {
		t.Errorf("没有已生效行时两个域名的上限都该是档位的 %d，实得 %v", limit, maxes)
	}
	bareView, ok := bare.results.view("api")
	if !ok {
		t.Fatal("这一轮的结果也该存进结果存储")
	}
	for _, h := range bareView.Hosts {
		for _, c := range h.Candidates {
			if c.Applied {
				t.Errorf("没给已知地址却冒出了「当前生效」的行：%+v", c)
			}
		}
	}
}

// assertNoSensitive 盯住「往界面去的话不带域名和地址」这条：
// 这两类内容只在候选结果那一侧出现，任务行和失败原因里都不该有。
func assertNoSensitive(t *testing.T, text string) {
	t.Helper()
	for _, forbidden := range []string{detectHostAPI, detectHostCodeload, detectHostSteam, "203.0.113", "github.com"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("界面文案里出现了不该有的内容 %q：%q", forbidden, text)
		}
	}
}
