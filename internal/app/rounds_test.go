package app

import (
	"context"
	"testing"

	"github.com/xiaokentrl/github-net/internal/task"
)

// roundBudget 手动塞一个轮的余量，给用例精确控制。
func roundBudget(r *roundBudgets, id string, n int) {
	r.mu.Lock()
	v := int64(n)
	r.byID[id] = &v
	r.mu.Unlock()
}

func TestRoundBudgetExhaustedMarksSkipped(t *testing.T) {
	exec := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	lookup := &detectLookup{cands: detectCandidates()}
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))
	exec.rounds = newRoundBudgets()
	roundBudget(exec.rounds, "r1", 0) // 这一轮的预算已经被前面的项目用光

	out := exec.Execute(context.Background(), task.Task{
		ID: "t1", Kind: task.KindDetect, ItemID: "api", RoundID: "r1", StrategyID: detectMethodGitHub,
	}, func(string, string, int) {})

	if out.Status != task.StatusSkipped || out.Message == "" {
		t.Fatalf("预算耗尽的检测该以 skipped 收场并给原因，实际 %+v", out)
	}
}

func TestRoundBudgetTakenAndReleased(t *testing.T) {
	exec := newDetectExecutorForTest(t, detectStore(t, nil), availableProbe)
	lookup := &detectLookup{cands: detectCandidates()}
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))
	exec.rounds = newRoundBudgets()
	roundBudget(exec.rounds, "r1", 100)

	out := exec.Execute(context.Background(), task.Task{
		ID: "t1", Kind: task.KindDetect, ItemID: "api", RoundID: "r1", StrategyID: detectMethodGitHub,
	}, func(string, string, int) {})

	if out.Status != task.StatusDone {
		t.Fatalf("预算充足时该正常跑完，实际 %+v", out)
	}
	exec.rounds.mu.Lock()
	remaining := *exec.rounds.byID["r1"]
	exec.rounds.mu.Unlock()
	// availableProbe 会真的发起探测；跑完没用完的部分必须还回轮上，
	// 剩余 = 100 - 实际启动的探测数。断言「还回去了一些」而不是精确数（探测数由假探测决定）。
	if remaining >= 100 {
		t.Fatalf("跑完该扣减或归还预算，剩余 %d 说明没走轮账本", remaining)
	}
}

func TestRoundBudgetTakeAndReleaseMath(t *testing.T) {
	r := newRoundBudgets()
	// 轮 ID 由队列发号，账本这边只按 ID 登记预算，所以用例直接给一个 ID 进去。
	id := r.put("r1", 10)
	if got := r.take(id, 4); got != 4 {
		t.Fatalf("take(4) 该给 4，实际 %d", got)
	}
	if got := r.take(id, 8); got != 6 {
		t.Fatalf("take(8) 只剩 6 可给，实际 %d", got)
	}
	if got := r.take(id, 1); got != 0 {
		t.Fatalf("预算耗尽该给 0，实际 %d", got)
	}
	r.release(id, 2)
	if got := r.take(id, 5); got != 2 {
		t.Fatalf("归还 2 后再取该得 2，实际 %d", got)
	}
	// 没有记录的轮按「不受限」处理：旧任务没有轮 ID 时不能被预算卡死。
	if got := r.take("no-such-round", 7); got != 7 {
		t.Fatalf("未知轮该不受限，实际 %d", got)
	}
}

func TestDetectItemsAssignsSharedRoundID(t *testing.T) {
	svc := newApplyService(t, nil)
	if _, err := svc.SelectStrategy(detectScopeGitHub, "live.one", 0, "m1"); err != nil {
		t.Fatalf("先选方法: %v", err)
	}
	svc.rounds = newRoundBudgets()

	tasks, err := svc.DetectItems([]string{"api", "bare"}, "")
	if err != nil {
		t.Fatalf("批量检测: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("该排 2 条任务，实际 %d", len(tasks))
	}
	if tasks[0].RoundID == "" || tasks[0].RoundID != tasks[1].RoundID {
		t.Fatalf("同一批任务该共享同一个轮 ID: %+v", tasks)
	}
	// 轮账本里要有这一轮，且预算来自配置（省流档 2 候选/域名、上限 108）。
	svc.rounds.mu.Lock()
	_, ok := svc.rounds.byID[tasks[0].RoundID]
	svc.rounds.mu.Unlock()
	if !ok {
		t.Fatal("轮账本里没有这一轮")
	}
}

// 轮的 ID 只有一个来源（队列发号）：任务带的、账本记的、快照里那一条必须是同一个。
func TestDetectItemsRoundComesFromQueue(t *testing.T) {
	svc := newApplyService(t, nil)
	if _, err := svc.SelectStrategy(detectScopeGitHub, "live.one", 0, "m1"); err != nil {
		t.Fatalf("先选方法: %v", err)
	}
	svc.rounds = newRoundBudgets()

	tasks, err := svc.DetectItems([]string{"api", "bare"}, "")
	if err != nil {
		t.Fatalf("批量检测: %v", err)
	}
	snap := svc.GetTasks()
	if len(snap.Rounds) != 1 {
		t.Fatalf("一次入口调用开一轮，实际 %+v", snap.Rounds)
	}
	round := snap.Rounds[0]
	if round.ID == "" || round.ID != tasks[0].RoundID {
		t.Fatalf("任务带的轮 ID 就是队列新开的这一个: %q vs %+v", tasks[0].RoundID, round)
	}
	if round.Name != "代码与下载" || round.Kind != task.KindDetect || round.ScopeID != detectScopeGitHub {
		t.Errorf("根节点要显示后端给的分类名: %+v", round)
	}
	svc.rounds.mu.Lock()
	_, ok := svc.rounds.byID[round.ID]
	svc.rounds.mu.Unlock()
	if !ok {
		t.Errorf("预算账本要记在同一个轮 ID 上: %+v", round)
	}
}

// 单项检测 = 只有一项的一轮，原型里 detectSingle 就是 detectAll([item])，根节点名同样按分类给。
func TestDetectSingleItemOpensItsOwnRound(t *testing.T) {
	svc := newApplyService(t, nil)
	if _, err := svc.SelectStrategy(detectScopeGitHub, "live.one", 0, "m1"); err != nil {
		t.Fatalf("先选方法: %v", err)
	}
	tasks, err := svc.DetectItems([]string{"api"}, "")
	if err != nil {
		t.Fatalf("单项检测: %v", err)
	}
	snap := svc.GetTasks()
	if len(snap.Rounds) != 1 || snap.Rounds[0].Name != "代码与下载" {
		t.Fatalf("单项也要有根节点，名字按分类: %+v", snap.Rounds)
	}
	if snap.Rounds[0].ID != tasks[0].RoundID {
		t.Errorf("任务归进这一轮: %q vs %+v", tasks[0].RoundID, snap.Rounds[0])
	}
}

func TestDetectRoundLabel(t *testing.T) {
	scopeName := func(id string) string {
		if id == detectScopeGitHub {
			return "代码与下载"
		}
		return ""
	}
	cases := []struct {
		what      string
		batch     []detectCandidate
		wantName  string
		wantScope string
	}{
		{"空批", nil, "批量检测", ""},
		{"同范围用分类名", []detectCandidate{{id: "api", scopeID: detectScopeGitHub}, {id: "bare", scopeID: detectScopeGitHub}}, "代码与下载", detectScopeGitHub},
		{"跨范围退成批量检测", []detectCandidate{{id: "api", scopeID: detectScopeGitHub}, {id: "steam", scopeID: detectScopeSteam}}, "批量检测", ""},
		{"范围查不到名字", []detectCandidate{{id: "x", scopeID: "no-such-scope"}}, "批量检测", "no-such-scope"},
	}
	for _, c := range cases {
		name, scopeID := detectRoundLabel(c.batch, scopeName)
		if name != c.wantName || scopeID != c.wantScope {
			t.Errorf("%s: 想要 %q/%q，实际 %q/%q", c.what, c.wantName, c.wantScope, name, scopeID)
		}
	}
}

func TestScopeName(t *testing.T) {
	svc := newApplyService(t, nil)
	if got := svc.scopeName(detectScopeGitHub); got != "代码与下载" {
		t.Errorf("按范围取分类名，实际 %q", got)
	}
	if got := svc.scopeName(detectScopeSteam); got != "商店" {
		t.Errorf("按范围取分类名，实际 %q", got)
	}
	if got := svc.scopeName("no-such-scope"); got != "" {
		t.Errorf("范围不在目录里该给空串由调用方兜底，实际 %q", got)
	}
	if got := svc.scopeName(""); got != "" {
		t.Errorf("空范围不该匹配上任何分类，实际 %q", got)
	}
	var noCatalog Service
	if got := noCatalog.scopeName(detectScopeGitHub); got != "" {
		t.Errorf("目录都没装时给空串，实际 %q", got)
	}
}
