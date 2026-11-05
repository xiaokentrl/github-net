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
			c.ScopeIDs(), map[string]bool{"live.one": true}, c.CategoryIDs()),
		priv:          priv,
		detectResults: results,
		tasks:         task.NewQueue(newDispatchExecutor(newGatedExecutor(), priv, results, nil, nil), nil),
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
	if got := len(svc.GetTasks().Tasks); got != 0 {
		t.Fatalf("形状不对的清单不该在队列里留任务，实际 %d 条", got)
	}
}

// 勾了但这一会儿做不了的项不再整批拒——它们进同一轮、以「跳过」落定，原因跟着这条任务走。
// 界面据此能列出「哪几项没做、为什么没做」，不用用户对着剩下的行猜。
func TestApplyItemsSkipsItemsWithoutUsableResult(t *testing.T) {
	svc := newApplyService(t, &fakePriv{})
	// 有结果但全不可用。
	svc.detectResults.put(detectScopeGitHub, "live.one", &accelerator.Result{
		ItemID:     "api",
		Candidates: []accelerator.Candidate{{ID: "c1", Hostname: "api.github.com", IP: "203.0.113.10", Status: accelerator.StatusTimeout}},
		Hostnames:  []accelerator.HostOutcome{{Hostname: "api.github.com", Resolved: true}},
	})
	tasks, err := svc.ApplyItems([]string{"api", "bare"})
	if err != nil {
		t.Fatalf("做不了的项不该把整批挡下: %v", err)
	}
	want := map[string]string{
		// bare 从没检测过，两句原因不能混成一句：下一步动作不一样。
		"api":  "最近一轮检测没有可用的候选地址，先重新检测",
		"bare": "这个项目还没有检测结果，先检测再应用",
	}
	if len(tasks) != 2 {
		t.Fatalf("勾 2 项就该有 2 条任务，实际 %d 条: %+v", len(tasks), tasks)
	}
	for _, tk := range tasks {
		if tk.Status != task.StatusSkipped {
			t.Errorf("%s 该以「跳过」落定，实际 %s", tk.ItemID, tk.Status)
		}
		if tk.ErrorMsg != want[tk.ItemID] {
			t.Errorf("%s 的原因 = %q，期望 %q", tk.ItemID, tk.ErrorMsg, want[tk.ItemID])
		}
	}
	snap := svc.GetTasks()
	if len(snap.Rounds) != 1 || snap.Rounds[0].Name != "应用队列 · 2 项" {
		t.Fatalf("两条都该归进同一个新轮、轮名按勾选项数给: %+v", snap.Rounds)
	}
}

// 混合一批：能做的照常排进队列，做不了的以「跳过」同轮落定，两者不会互相顶掉。
func TestApplyItemsMixedBatchPutsEveryCheckedItemInOneRound(t *testing.T) {
	priv := &fakePriv{}
	svc := newApplyService(t, priv)
	putAvailableResult(svc, "api")

	tasks, err := svc.ApplyItems([]string{"api", "bare"})
	if err != nil {
		t.Fatalf("混合批次该排进队列: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("该排 2 条，实际 %d 条: %+v", len(tasks), tasks)
	}
	byID := map[string]task.Task{}
	for _, tk := range tasks {
		byID[tk.ItemID] = tk
	}
	if byID["api"].Status == task.StatusSkipped {
		t.Errorf("有可用候选的 api 不该被跳过: %+v", byID["api"])
	}
	if byID["bare"].Status != task.StatusSkipped {
		t.Errorf("没检测过的 bare 该以跳过落定，实际 %s", byID["bare"].Status)
	}
	if byID["api"].RoundID == "" || byID["api"].RoundID != byID["bare"].RoundID {
		t.Errorf("两条都该归进同一轮: api=%q bare=%q", byID["api"].RoundID, byID["bare"].RoundID)
	}
	// 跳过的那条永远不会被执行器领走：领走的判据是「等待」，它一进来就是终态。
	for _, tk := range svc.GetTasks().Tasks {
		if tk.ItemID == "bare" && tk.Status != task.StatusSkipped {
			t.Errorf("跳过的任务被队列改成了 %s，它不该被领走: %+v", tk.Status, tk)
		}
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

// hasAppliedMark 看这个项目最近一轮里还有没有「当前生效」标记。
func hasAppliedMark(s *Service, itemID string) bool {
	view, ok := s.detectResults.view(itemID)
	if !ok {
		return false
	}
	for _, h := range view.Hosts {
		for _, c := range h.Candidates {
			if c.Applied {
				return true
			}
		}
	}
	return false
}

// 全部还原是一笔不进队列的命令，没有执行器来重标「当前生效」。
// 成功后必须把内存里所有项目的标记清掉：界面还原后会重拉结果，
// 标记留着就等于把已经还原掉的 IP 继续说成「当前写入」。
func TestRestoreAllHostsClearsAppliedMarks(t *testing.T) {
	// 服务回 0 项也算「这笔命令成功、系统里已经没有本工具写的映射」，一样要清。
	for _, restored := range []int{2, 0} {
		svc := newApplyService(t, &fakePriv{restoreAll: restored})
		putAvailableResult(svc, "api")
		putAvailableResult(svc, "bare")
		// 先摆出应用成功后的现场：两个项目各有候选带着「当前生效」。
		svc.detectResults.markApplied("api", map[string][]string{"api.github.com": {"203.0.113.11"}})
		svc.detectResults.markApplied("bare", map[string][]string{"api.github.com": {"203.0.113.10"}})
		if !hasAppliedMark(svc, "api") || !hasAppliedMark(svc, "bare") {
			t.Fatalf("前提不成立：服务回 %d 项时，应用过之后该有「当前生效」标记", restored)
		}

		n, err := svc.RestoreAllHosts()
		if err != nil || n != restored {
			t.Fatalf("全量还原该回 (%d, nil)，实际 (%d, %v)", restored, n, err)
		}
		if hasAppliedMark(svc, "api") || hasAppliedMark(svc, "bare") {
			t.Fatalf("服务回 %d 项：还原成功后所有项目的「当前生效」标记都该清掉", restored)
		}
	}
}

// 还原没成就是没成，现场一个字都不动：失败时不许把标记清掉，界面该继续显示还原前的样子。
func TestRestoreAllHostsFailureKeepsAppliedMarks(t *testing.T) {
	svc := newApplyService(t, &fakePriv{err: privilege.ErrHelperUnavailable})
	putAvailableResult(svc, "api")
	svc.detectResults.markApplied("api", map[string][]string{"api.github.com": {"203.0.113.11"}})

	if _, err := svc.RestoreAllHosts(); codeOf(t, err) != CodeHelperUnavailable {
		t.Fatal("服务不可用该带 helper_unavailable 码")
	}
	if !hasAppliedMark(svc, "api") {
		t.Fatal("还原失败时不该动现场：「当前生效」标记要留着")
	}
}
