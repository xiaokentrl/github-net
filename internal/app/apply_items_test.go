package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/hosts"
	"github.com/xiaokentrl/github-net/internal/privilege"
	"github.com/xiaokentrl/github-net/internal/task"
)

// fakePriv 记下特权服务收到了什么，或按用例需要返回失败。
type fakePriv struct {
	appliedID  string
	applied    []hosts.Mapping
	restoredID string
	restoreAll int
	err        error
	// written 是 State 的应答：默认零值即「没写过」。
	written hosts.Written
}

func (f *fakePriv) Ping() error { return f.err }

func (f *fakePriv) Apply(itemID string, mappings []hosts.Mapping) error {
	if f.err != nil {
		return f.err
	}
	f.appliedID = itemID
	f.applied = mappings
	return nil
}

func (f *fakePriv) RestoreItem(itemID string) error {
	if f.err != nil {
		return f.err
	}
	f.restoredID = itemID
	return nil
}

func (f *fakePriv) RestoreAll() (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.restoreAll, nil
}

func (f *fakePriv) State() (hosts.Written, error) {
	if f.err != nil {
		return hosts.Written{}, f.err
	}
	return f.written, nil
}

// newApplyService 建一个入口+执行器都齐的服务：队列执行器是分发台
// （检测走可按住的假执行器，应用/恢复走注入的假特权客户端），结果存储由用例预填。
func newApplyService(t *testing.T, priv privilege.Caller) *Service {
	t.Helper()
	c := detectCatalog()
	results := newDetectResultStore()
	table := fakeTable(t, map[string][]strategies.Strategy{
		detectScopeGitHub: {fakeStrategy("live.one", "还在的方法", detectScopeGitHub)},
	}, map[string]strategies.Strategy{"live.one": fakeStrategy("live.one", "还在的方法", detectScopeGitHub)})
	return &Service{
		productCatalog: c,
		strategyTable:  table,
		settings: config.NewStore(
			filepath.Join(t.TempDir(), "config", "config.json"),
			c.ScopeIDs(), map[string]bool{"live.one": true}),
		priv:          priv,
		detectResults: results,
		tasks:         task.NewQueue(newDispatchExecutor(newGatedExecutor(), priv, results), nil),
	}
}

// putAvailableResult 给项目预存一轮「有可用候选」的结论，延迟差别用于验证选址。
func putAvailableResult(s *Service, itemID string) {
	s.detectResults.put(detectScopeGitHub, "live.one", &accelerator.Result{
		ItemID:    itemID,
		Available: 2,
		Candidates: []accelerator.Candidate{
			{ID: "c1", Hostname: "api.github.com", IP: "203.0.113.10", Status: accelerator.StatusAvailable, LatencyMS: 100},
			{ID: "c2", Hostname: "api.github.com", IP: "203.0.113.11", Status: accelerator.StatusAvailable, LatencyMS: 40},
			{ID: "c3", Hostname: "codeload.github.com", IP: "203.0.113.12", Status: accelerator.StatusUnavailable},
		},
		Hostnames: []accelerator.HostOutcome{
			{Hostname: "api.github.com", Resolved: true},
			{Hostname: "codeload.github.com", Resolved: true},
		},
	})
}

func TestApplyItemsEntryValidation(t *testing.T) {
	svc := newApplyService(t, &fakePriv{})

	if _, err := svc.ApplyItems(nil); codeOf(t, err) != CodeInvalidItemID {
		t.Fatalf("空清单该被挡下，实际 %v", err)
	}
	if _, err := svc.ApplyItems([]string{"no-such"}); codeOf(t, err) != CodeInvalidItemID {
		t.Fatalf("未知项目该被挡下，实际 %v", err)
	}
	if _, err := svc.ApplyItems([]string{"api"}); codeOf(t, err) != CodeNoDetectResult {
		t.Fatalf("没检测过的项目该在入口被挡下（no_detect_result），实际 %v", err)
	}
	// 有结果但全不可用：同样挡在入口。
	svc.detectResults.put(detectScopeGitHub, "live.one", &accelerator.Result{
		ItemID:     "api",
		Candidates: []accelerator.Candidate{{ID: "c1", Hostname: "api.github.com", IP: "203.0.113.10", Status: accelerator.StatusTimeout}},
		Hostnames:  []accelerator.HostOutcome{{Hostname: "api.github.com", Resolved: true}},
	})
	if _, err := svc.ApplyItems([]string{"api"}); codeOf(t, err) != CodeNoDetectResult {
		t.Fatalf("没有可用候选该被挡下，实际 %v", err)
	}
	if got := len(svc.GetTasks().Tasks); got != 0 {
		t.Fatalf("被拒的请求不该在队列里留任务，实际 %d 条", got)
	}
}

func TestApplyTaskRunsAndPicksBestCandidates(t *testing.T) {
	priv := &fakePriv{}
	svc := newApplyService(t, priv)
	putAvailableResult(svc, "api")

	tasks, err := svc.ApplyItems([]string{"api"})
	if err != nil {
		t.Fatalf("有可用候选的 apply 该排进队列: %v", err)
	}
	if tasks[0].Kind != task.KindApply {
		t.Fatalf("任务类型该是 apply: %+v", tasks[0])
	}

	// 放行 gated 执行器让任务跑起来，等它终态。
	waitFor(t, "应用任务完成", func() bool {
		item, ok := findTask(svc.GetTasks(), tasks[0].ID)
		return ok && task.IsTerminal(item.Status)
	})
	item, _ := findTask(svc.GetTasks(), tasks[0].ID)
	if item.Status != task.StatusDone {
		t.Fatalf("应用任务该完成，实际 %+v", item)
	}
	// 选址：api.github.com 两个可用候选里取延迟最低的 40ms 那颗；
	// codeload.github.com 只有不可用候选，不该出现在映射里。
	if priv.appliedID != "api" || len(priv.applied) != 1 {
		t.Fatalf("特权服务该收到 1 组映射: %+v", priv.applied)
	}
	if priv.applied[0].IP != "203.0.113.11" || len(priv.applied[0].Hostnames) != 1 || priv.applied[0].Hostnames[0] != "api.github.com" {
		t.Fatalf("选址该取延迟最低的可用候选: %+v", priv.applied)
	}
}

func TestRestoreTaskRunsThroughPrivilege(t *testing.T) {
	priv := &fakePriv{}
	svc := newApplyService(t, priv)

	tasks, err := svc.RestoreItems([]string{"api"})
	if err != nil {
		t.Fatalf("还原该排进队列: %v", err)
	}
	waitFor(t, "还原任务完成", func() bool {
		item, ok := findTask(svc.GetTasks(), tasks[0].ID)
		return ok && task.IsTerminal(item.Status)
	})
	item, _ := findTask(svc.GetTasks(), tasks[0].ID)
	if item.Status != task.StatusRestored {
		t.Fatalf("还原任务该以 restored 收场，实际 %+v", item)
	}
	if priv.restoredID != "api" {
		t.Fatalf("特权服务该收到还原请求，实际 %q", priv.restoredID)
	}
}

// 队列根节点的名字由后端按原型规则给：应用「应用队列 · N 项」、恢复单个「恢复 · 项目名」、
// 恢复多个「恢复队列 · N 项」。界面上不再自己拼第二套。
func TestApplyAndRestoreRoundNames(t *testing.T) {
	cases := []struct {
		what     string
		enqueue  func(svc *Service) ([]task.Task, error)
		wantName string
		wantKind task.Kind
		count    int
	}{
		{
			what:     "应用一批",
			enqueue:  func(svc *Service) ([]task.Task, error) { return svc.ApplyItems([]string{"api", "bare"}) },
			wantName: "应用队列 · 2 项",
			wantKind: task.KindApply,
			count:    2,
		},
		{
			what:     "恢复单个",
			enqueue:  func(svc *Service) ([]task.Task, error) { return svc.RestoreItems([]string{"api"}) },
			wantName: "恢复 · API",
			wantKind: task.KindRestore,
			count:    1,
		},
		{
			what:     "恢复一批",
			enqueue:  func(svc *Service) ([]task.Task, error) { return svc.RestoreItems([]string{"api", "bare"}) },
			wantName: "恢复队列 · 2 项",
			wantKind: task.KindRestore,
			count:    2,
		},
	}
	for _, c := range cases {
		// 每个用例一台新服务：上一批任务还在途时会去重，根节点计数就对不上。
		svc := newApplyService(t, &fakePriv{})
		putAvailableResult(svc, "api")
		putAvailableResult(svc, "bare")

		tasks, err := c.enqueue(svc)
		if err != nil {
			t.Fatalf("%s 该排进队列: %v", c.what, err)
		}
		snap := svc.GetTasks()
		if len(snap.Rounds) != 1 {
			t.Fatalf("%s 该只开一轮: %+v", c.what, snap.Rounds)
		}
		round := snap.Rounds[0]
		if round.Name != c.wantName || round.Kind != c.wantKind {
			t.Errorf("%s: 根节点该叫 %q（%s），实际 %+v", c.what, c.wantName, c.wantKind, round)
		}
		if len(tasks) != c.count {
			t.Fatalf("%s 该排 %d 条，实际 %d", c.what, c.count, len(tasks))
		}
		for _, tk := range snap.Tasks {
			if tk.RoundID != round.ID {
				t.Errorf("%s: 这条任务没归进新开的轮: %+v vs %+v", c.what, tk, round)
			}
		}
	}
}

func TestApplyRoundLabelAcrossScopes(t *testing.T) {
	// api（github）+ steam（steam）跨范围：整批同属一个范围才记范围，跨了就留空。
	svc := newApplyService(t, &fakePriv{})
	putAvailableResult(svc, "api")
	svc.detectResults.put(detectScopeSteam, "steam-method", &accelerator.Result{
		ItemID:    "steam",
		Available: 1,
		Candidates: []accelerator.Candidate{
			{ID: "s1", Hostname: detectHostSteam, IP: "203.0.113.30", Status: accelerator.StatusAvailable, LatencyMS: 10},
		},
		Hostnames: []accelerator.HostOutcome{{Hostname: detectHostSteam, Resolved: true}},
	})
	if _, err := svc.ApplyItems([]string{"api", "steam"}); err != nil {
		t.Fatalf("跨范围应用该能排: %v", err)
	}
	snap := svc.GetTasks()
	if len(snap.Rounds) != 1 || snap.Rounds[0].ScopeID != "" {
		t.Fatalf("跨范围的一批不该记上某个范围: %+v", snap.Rounds)
	}
	if snap.Rounds[0].Name != "应用队列 · 2 项" {
		t.Errorf("名字照条数给: %+v", snap.Rounds[0])
	}
}

func TestApplyTaskFailsWhenHelperUnavailable(t *testing.T) {
	priv := &fakePriv{err: privilege.ErrHelperUnavailable}
	svc := newApplyService(t, priv)
	putAvailableResult(svc, "api")

	tasks, err := svc.ApplyItems([]string{"api"})
	if err != nil {
		t.Fatalf("入口只验形状，服务不可用在执行时才暴露: %v", err)
	}
	waitFor(t, "应用任务失败", func() bool {
		item, ok := findTask(svc.GetTasks(), tasks[0].ID)
		return ok && task.IsTerminal(item.Status)
	})
	item, _ := findTask(svc.GetTasks(), tasks[0].ID)
	if item.Status != task.StatusFailed || !strings.Contains(item.ErrorMsg, "特权辅助服务没在运行") {
		t.Fatalf("服务不可用的失败文案要给修复方向，实际 %+v", item)
	}
}

func TestRestoreAllHostsDirectCommand(t *testing.T) {
	priv := &fakePriv{restoreAll: 3}
	svc := newApplyService(t, priv)
	n, err := svc.RestoreAllHosts()
	if err != nil || n != 3 {
		t.Fatalf("全量还原该回 (3, nil)，实际 (%d, %v)", n, err)
	}

	priv2 := &fakePriv{err: privilege.ErrHelperUnavailable}
	svc2 := newApplyService(t, priv2)
	if _, err := svc2.RestoreAllHosts(); codeOf(t, err) != CodeHelperUnavailable {
		t.Fatalf("服务不可用该带 helper_unavailable 码，实际 %v", err)
	}
}
