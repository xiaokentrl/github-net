package app

import (
	"path/filepath"
	"testing"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/task"
)

// newDetectEntryService 是 DetectItems/GetDetectResult 的用例底座：
// 真实目录形状（detectCatalog）+ 注入的方法表 + 注入的执行器。
// 执行器由用例给（一般是可以按住不放的假执行器），入口测试绝不放真实网络出去。
func newDetectEntryService(t *testing.T, table strategyTable, exec task.Executor) *Service {
	t.Helper()
	c := detectCatalog()
	return &Service{
		productCatalog: c,
		strategyTable:  table,
		settings: config.NewStore(
			filepath.Join(t.TempDir(), "config", "config.json"),
			c.ScopeIDs(), map[string]bool{"live.one": true}),
		tasks:         task.NewQueue(exec, nil),
		detectResults: newDetectResultStore(),
	}
}

func TestDetectItemsRejectsEmptyAndUnknownIDs(t *testing.T) {
	table := fakeTable(t, map[string][]strategies.Strategy{
		detectScopeGitHub: {fakeStrategy("live.one", "还在的方法", detectScopeGitHub)},
	}, map[string]strategies.Strategy{"live.one": fakeStrategy("live.one", "还在的方法", detectScopeGitHub)})
	svc := newDetectEntryService(t, table, pendingExecutor{})

	if _, err := svc.DetectItems([]string{"  ", ""}, ""); codeOf(t, err) != CodeInvalidItemID {
		t.Fatalf("空清单该被挡下（invalid_item_id），实际 %v", err)
	}
	if _, err := svc.DetectItems(nil, ""); codeOf(t, err) != CodeInvalidItemID {
		t.Fatalf("nil 清单该被挡下，实际 %v", err)
	}
	if _, err := svc.DetectItems([]string{"no-such-item"}, ""); codeOf(t, err) != CodeInvalidItemID {
		t.Fatalf("目录里没有的项目该被挡下，实际 %v", err)
	}
	if got := len(svc.GetTasks().Tasks); got != 0 {
		t.Fatalf("被拒的请求不该在队列里留下任务，实际 %d 条", got)
	}
}

func TestDetectItemsRejectsWithoutStrategySelected(t *testing.T) {
	table := fakeTable(t, map[string][]strategies.Strategy{
		detectScopeGitHub: {fakeStrategy("live.one", "还在的方法", detectScopeGitHub)},
	}, map[string]strategies.Strategy{"live.one": fakeStrategy("live.one", "还在的方法", detectScopeGitHub)})
	svc := newDetectEntryService(t, table, pendingExecutor{})

	if _, err := svc.DetectItems([]string{"api"}, ""); codeOf(t, err) != CodeNoStrategySelected {
		t.Fatalf("范围没选方法该在入口被挡下（no_strategy_selected），实际 %v", err)
	}
	if got := len(svc.GetTasks().Tasks); got != 0 {
		t.Fatalf("被拒的请求不该在队列里留下任务，实际 %d 条", got)
	}
}

func TestDetectItemsRejectsWhenMethodMissingOrNotApplies(t *testing.T) {
	table := fakeTable(t, map[string][]strategies.Strategy{
		detectScopeGitHub: {fakeStrategy("live.one", "还在的方法", detectScopeGitHub)},
	}, map[string]strategies.Strategy{"live.one": fakeStrategy("live.one", "还在的方法", detectScopeGitHub)})
	svc := newDetectEntryService(t, table, pendingExecutor{})
	if _, err := svc.SelectStrategy(detectScopeGitHub, "live.one", 0, "m1"); err != nil {
		t.Fatalf("先选上方法: %v", err)
	}

	// 方法从注册表里消失（配置里还留着 ID）：入口照挡，不能拿着一个点不到的 ID 去排队。
	svc.strategyTable = strategyTable{}
	if _, err := svc.DetectItems([]string{"api"}, ""); codeOf(t, err) != CodeNoStrategySelected {
		t.Fatalf("方法已不在注册表该被挡下，实际 %v", err)
	}

	// 方法还在但不适用这个范围（后来收窄了适用面）：同样挡在入口。
	svc.strategyTable = strategyTable{
		get: func(id string) (strategies.Strategy, bool) {
			return fakeStrategy("live.one", "还在的方法", detectScopeSteam), true
		},
	}
	if _, err := svc.DetectItems([]string{"api"}, ""); codeOf(t, err) != CodeNoStrategySelected {
		t.Fatalf("方法不适用该范围该被挡下，实际 %v", err)
	}
	if got := len(svc.GetTasks().Tasks); got != 0 {
		t.Fatalf("被拒的请求不该在队列里留下任务，实际 %d 条", got)
	}
}

func TestDetectItemsEnqueuesDedupesAndRejectsMixedBatch(t *testing.T) {
	table := fakeTable(t, map[string][]strategies.Strategy{
		detectScopeGitHub: {fakeStrategy("live.one", "还在的方法", detectScopeGitHub)},
	}, map[string]strategies.Strategy{"live.one": fakeStrategy("live.one", "还在的方法", detectScopeGitHub)})
	exec := newGatedExecutor()
	svc := newDetectEntryService(t, table, exec)
	if _, err := svc.SelectStrategy(detectScopeGitHub, "live.one", 0, "m1"); err != nil {
		t.Fatalf("先选上方法: %v", err)
	}

	tasks, err := svc.DetectItems([]string{"api"}, "")
	if err != nil {
		t.Fatalf("选好方法后检测该排进队列: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Kind != task.KindDetect || tasks[0].ItemID != "api" {
		t.Fatalf("返回的任务形状不对: %+v", tasks)
	}
	if tasks[0].ScopeID != detectScopeGitHub {
		t.Fatalf("任务该带上项目所属范围: %+v", tasks[0])
	}

	// 同一项目还在队列里时再点检测：拿到同一条任务，不是两条、也不是报错。
	again, err := svc.DetectItems([]string{"api"}, "")
	if err != nil {
		t.Fatalf("重复点检测不该报错: %v", err)
	}
	if len(again) != 1 || again[0].ID != tasks[0].ID {
		t.Fatalf("重复请求该返回已在队列的那条任务: %+v vs %+v", again, tasks)
	}

	// 整批校验：坏项目排在前，好项目也不会被排队；整批拒绝、什么都不留。
	if _, err := svc.DetectItems([]string{"no-such", "steam"}, ""); codeOf(t, err) != CodeInvalidItemID {
		t.Fatalf("混入未知项目该整批拒绝，实际 %v", err)
	}
}

func TestGetDetectResultEmptyThenStored(t *testing.T) {
	svc := newDetectEntryService(t, strategyTable{}, pendingExecutor{})

	// 没测过不是错误：给空形状，数组是空切片。
	view, err := svc.GetDetectResult("api")
	if err != nil {
		t.Fatalf("没有结果不该报错: %v", err)
	}
	if view.ItemID != "api" || view.Hosts == nil || len(view.Hosts) != 0 {
		t.Fatalf("空形状该带项目 ID 和空数组，实际 %+v", view)
	}
	if _, err := svc.GetDetectResult("  "); codeOf(t, err) != CodeInvalidItemID {
		t.Fatalf("空项目 ID 该被挡下，实际 %v", err)
	}

	// 存进去一轮结论，读回来的形状要逐字段对得上。
	svc.detectResults.put(detectScopeGitHub, "live.one", &accelerator.Result{
		ItemID:     "api",
		ProbesUsed: 3,
		DNSQueries: 2,
		Available:  1,
		Truncated:  true,
		Candidates: []accelerator.Candidate{
			{ID: "c1", Hostname: "api.github.com", IP: "203.0.113.10", Status: accelerator.StatusAvailable},
			{ID: "c2", Hostname: "api.github.com", IP: "203.0.113.11", Status: accelerator.StatusUnavailable},
		},
		Hostnames: []accelerator.HostOutcome{
			{Hostname: "api.github.com", Resolved: true, Candidates: 2, Available: 1},
		},
	})
	view, err = svc.GetDetectResult("api")
	if err != nil {
		t.Fatalf("存了结果再读不该失败: %v", err)
	}
	if view.StrategyID != "live.one" || view.ScopeID != detectScopeGitHub ||
		view.ProbesUsed != 3 || view.DNSQueries != 2 || view.Available != 1 || !view.Truncated {
		t.Fatalf("计数字段没对上: %+v", view)
	}
	if len(view.Hosts) != 1 || len(view.Hosts[0].Candidates) != 2 ||
		view.Hosts[0].Candidates[0].IP != "203.0.113.10" {
		t.Fatalf("候选行没按主机名归组: %+v", view.Hosts)
	}
}

func TestNewServiceWiresDetectExecutorAndReportsReady(t *testing.T) {
	svc := NewService("GitHub Net", "test", nil, nil)
	if !svc.detectReady {
		t.Fatalf("目录和设置都就绪时装配层该换上检测执行器")
	}
	status, err := svc.GetAppStatus()
	if err != nil || !status.DNSDetectionReady {
		t.Fatalf("换装后 DNSDetectionReady 该为 true，实际 %+v（err=%v）", status, err)
	}
	ShutdownTaskQueue(svc)
}
