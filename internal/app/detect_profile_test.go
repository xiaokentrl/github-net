package app

import (
	"path/filepath"
	"testing"

	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 档位弹框选的是「这一次」：命令带上档位后，轮账本记的是它，设置里那一档一字不改；
// 预算预估也要按选中档位算，否则「还差多少次」和真正开跑的那轮对不上。
func newDetectProfileService(t *testing.T) *Service {
	t.Helper()
	c := detectCatalog()
	table := fakeTable(t, map[string][]strategies.Strategy{
		detectScopeGitHub: {fakeStrategy("live.one", "还在的方法", detectScopeGitHub)},
	}, map[string]strategies.Strategy{"live.one": fakeStrategy("live.one", "还在的方法", detectScopeGitHub)})
	svc := &Service{
		productCatalog: c,
		strategyTable:  table,
		settings: config.NewStore(
			filepath.Join(t.TempDir(), "config", "config.json"),
			c.ScopeIDs(), map[string]bool{"live.one": true}, c.CategoryIDs()),
		tasks:         task.NewQueue(pendingExecutor{}, nil),
		detectResults: newDetectResultStore(),
		rounds:        newRoundBudgets(),
	}
	if _, err := svc.SelectStrategy(detectScopeGitHub, "live.one", 0, "m1"); err != nil {
		t.Fatalf("先选上方法: %v", err)
	}
	return svc
}

func TestDetectItemsSelectedProfileAppliesOnlyToThisRound(t *testing.T) {
	svc := newDetectProfileService(t)
	before, err := svc.settings.Snapshot()
	if err != nil {
		t.Fatalf("读设置失败: %v", err)
	}
	if before.Network.Profile != config.ProfileLow {
		t.Fatalf("出厂默认该是省流，实际 %q", before.Network.Profile)
	}

	tasks, err := svc.DetectItems([]string{"api"}, "", config.ProfileFull)
	if err != nil {
		t.Fatalf("指定完整档检测该排进队列: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("该只排一条任务，实际 %d 条", len(tasks))
	}
	roundID := tasks[0].RoundID
	if got := svc.rounds.profileOf(roundID); got != config.ProfileFull {
		t.Errorf("轮上记的档位该是完整档，实际 %q", got)
	}
	after, err := svc.settings.Snapshot()
	if err != nil {
		t.Fatalf("再读设置失败: %v", err)
	}
	if after.Network.Profile != config.ProfileLow {
		t.Error("选了本次档位，设置里的档位不该跟着变")
	}
	// api 有两个去重域名；完整档每域名 16 个候选，本轮预算该是 32 而不是省流的 4。
	if got := svc.rounds.take(roundID, 1000); got != 32 {
		t.Errorf("本轮预算该按完整档算成 32，实际 %d", got)
	}
}

func TestDetectBudgetUsesSelectedProfile(t *testing.T) {
	svc := newDetectProfileService(t)
	low, err := svc.DetectBudget([]string{"api"}, config.ProfileLow)
	if err != nil {
		t.Fatalf("省流档预估失败: %v", err)
	}
	full, err := svc.DetectBudget([]string{"api"}, config.ProfileFull)
	if err != nil {
		t.Fatalf("完整档预估失败: %v", err)
	}
	if low.Needed != 4 || full.Needed != 32 {
		t.Errorf("两个域名：省流该要 4 次、完整档该要 32 次，实际 %d / %d", low.Needed, full.Needed)
	}
	if full.Budget <= low.Budget {
		t.Errorf("完整档的可用预算该比省流多，实际 %d / %d", full.Budget, low.Budget)
	}
}

func TestDetectItemsRejectsUnknownProfile(t *testing.T) {
	svc := newDetectProfileService(t)
	if _, err := svc.DetectItems([]string{"api"}, "", "turbo"); codeOf(t, err) != CodeInvalidSettings {
		t.Fatalf("不认识的档位该整批拒绝（invalid_settings），实际 %v", err)
	}
	if got := len(svc.GetTasks().Tasks); got != 0 {
		t.Fatalf("被拒的请求不该在队列里留下任务，实际 %d 条", got)
	}
}
