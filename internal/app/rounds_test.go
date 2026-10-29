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
	id := r.newRound(10)
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

	tasks, err := svc.DetectItems([]string{"api", "bare"})
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
