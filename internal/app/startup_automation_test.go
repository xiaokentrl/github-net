package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 这条链的判定全部走假件：不碰配置目录、不发网络、不写 Hosts。
// 真接缝（读配置、按菜单展开项目、筛可用候选、数队列）另开一组用例，用真实目录形状验。

type automationHarness struct {
	plan    automationPlan
	planErr error

	// 四个钩子留 nil 就用默认答案：一个菜单一个轮、等待立刻返回、
	// 应用把全部项目排进一个轮、这一轮应用成功一项。
	onDetect func(itemIDs []string) ([]string, error)
	onApply  func(itemIDs []string) ([]string, int, error)
	onWait   func(roundID string) error
	onReport func(roundIDs []string) automationReport

	detects  [][]string
	applies  [][]string
	waits    []string
	reports  [][]string
	statuses []event.AutomationStatus
	quits    int
	roundSeq int
}

func (h *automationHarness) deps() automationDeps {
	return automationDeps{
		plan: func() (automationPlan, error) { return h.plan, h.planErr },
		detect: func(ids []string) ([]string, error) {
			h.detects = append(h.detects, ids)
			if h.onDetect != nil {
				return h.onDetect(ids)
			}
			h.roundSeq++
			return []string{fmt.Sprintf("detect%d", h.roundSeq)}, nil
		},
		apply: func(ids []string) ([]string, int, error) {
			h.applies = append(h.applies, ids)
			if h.onApply != nil {
				return h.onApply(ids)
			}
			return []string{"apply"}, len(ids), nil
		},
		wait: func(_ context.Context, id string) error {
			h.waits = append(h.waits, id)
			if h.onWait != nil {
				return h.onWait(id)
			}
			return nil
		},
		report: func(ids []string) automationReport {
			h.reports = append(h.reports, ids)
			if h.onReport != nil {
				return h.onReport(ids)
			}
			return automationReport{AppliedOK: 1}
		},
		quit:   func() { h.quits++ },
		status: func(s event.AutomationStatus) { h.statuses = append(h.statuses, s) },
	}
}

func (h *automationHarness) run(ctx context.Context) { runStartupAutomation(ctx, h.deps()) }

func (h *automationHarness) stages() []string {
	out := make([]string, 0, len(h.statuses))
	for _, s := range h.statuses {
		out = append(out, s.Stage)
	}
	return out
}

func (h *automationHarness) last() event.AutomationStatus {
	if len(h.statuses) == 0 {
		return event.AutomationStatus{}
	}
	return h.statuses[len(h.statuses)-1]
}

// autoPlan 按「开关 + 每个菜单要测哪些项目」拼出开局计划。
// 参数用字符串是为了让每条用例写在一行里，读的时候只看这一条测了什么。
func autoPlan(apply, closeAfter bool, menus ...string) automationPlan {
	plan := automationPlan{Startup: config.StartupConfig{
		AutoDetectMenus:     menus,
		AutoApplyOnStartup:  apply,
		AutoCloseAfterApply: closeAfter,
	}}
	for _, menu := range menus {
		plan.Batches = append(plan.Batches, automationBatch{
			MenuID:  menu,
			ItemIDs: []string{menu + "-item"},
		})
	}
	return plan
}

func TestAutomationStaysSilentWhenNoMenuIsSelected(t *testing.T) {
	h := &automationHarness{plan: autoPlan(true, true)}
	h.run(context.Background())
	if len(h.statuses) != 0 || len(h.detects) != 0 || h.quits != 0 {
		t.Fatalf("一个菜单都没勾＝这条没开，不该发状态也不该排队：%+v %v 退出 %d", h.statuses, h.detects, h.quits)
	}
}

func TestAutomationSkipsWhenSelectedMenusHaveNoItems(t *testing.T) {
	h := &automationHarness{plan: automationPlan{
		Startup: config.StartupConfig{AutoDetectMenus: []string{"custom"}, AutoApplyOnStartup: true},
	}}
	h.run(context.Background())
	if len(h.detects) != 0 {
		t.Fatalf("没有可检测的项目不该排队：%v", h.detects)
	}
	last := h.last()
	if last.Stage != event.AutomationStageSkipped || last.Reason != event.AutomationReasonNoItems {
		t.Errorf("该发一句「跳过的、原因是没有可检测项目」，实际 %+v", last)
	}
	if strings.Join(last.Menus, ",") != "custom" {
		t.Errorf("跳过也要说清是哪些菜单，实际 %v", last.Menus)
	}
}

func TestAutomationDetectThenApplyThenQuits(t *testing.T) {
	h := &automationHarness{plan: autoPlan(true, true, "code", "store")}
	h.run(context.Background())

	if got := strings.Join(h.stages(), "|"); got != "detecting|applying|closing" {
		t.Fatalf("三句状态要按顺序推出去，实际 %s", got)
	}
	if len(h.detects) != 2 || strings.Join(h.detects[0], ",") != "code-item" {
		t.Errorf("每个菜单各自排一批（一轮一个菜单），实际 %v", h.detects)
	}
	// 等的是「排进去的那几轮」：两个检测轮 + 一个应用轮。
	if got := strings.Join(h.waits, ","); got != "detect1,detect2,apply" {
		t.Errorf("等待顺序不对：%s", got)
	}
	if got := h.statuses[0]; got.DetectCount != 2 || strings.Join(got.Menus, ",") != "code,store" {
		t.Errorf("检测那一句要报出菜单和项数，实际 %+v", got)
	}
	if got := h.statuses[1]; got.ApplyCount != 2 {
		t.Errorf("应用那一句要报出排进的项数，实际 %+v", got)
	}
	if got := h.statuses[2]; got.AppliedCount != 1 {
		t.Errorf("退出前那一句要报出应用成功几项，实际 %+v", got)
	}
	if h.quits != 1 {
		t.Errorf("三闸门都过，该正好退出一次，实际 %d 次", h.quits)
	}
	// 退出那一门的读数要把检测轮和应用轮一起算进去，否则会把自己排的检测当成别人的活。
	if got := strings.Join(h.reports[0], ","); got != "detect1,detect2,apply" {
		t.Errorf("数队列时这几轮都要报进来，实际 %s", got)
	}
}

func TestAutomationRejectedMenuDoesNotBlockOthers(t *testing.T) {
	h := &automationHarness{plan: autoPlan(false, false, "code", "store")}
	h.onDetect = func(ids []string) ([]string, error) {
		if ids[0] == "code-item" {
			return nil, errors.New("这个项目所在的服务还没选检测方法")
		}
		h.roundSeq++
		return []string{fmt.Sprintf("detect%d", h.roundSeq)}, nil
	}
	h.run(context.Background())

	first := h.statuses[0]
	if strings.Join(first.Menus, ",") != "store" || strings.Join(first.RejectedMenus, ",") != "code" {
		t.Fatalf("被拒的菜单要单独点名，没拒的照跑：%+v", first)
	}
	if first.DetectCount != 1 {
		t.Errorf("项数只数真排进去的那批，实际 %+v", first)
	}
	if len(h.applies) != 0 {
		t.Errorf("自动应用没开，不该排应用任务：%v", h.applies)
	}
	if last := h.last(); last.Stage != event.AutomationStageDone || last.Reason != "" {
		t.Errorf("只检测的收尾是一句完成，不该带原因：%+v", last)
	}
}

func TestAutomationFailsWhenEveryMenuIsRejected(t *testing.T) {
	h := &automationHarness{plan: autoPlan(true, true, "code", "store")}
	h.onDetect = func([]string) ([]string, error) { return nil, errors.New("目录里没有这个项目") }
	h.run(context.Background())

	if len(h.waits) != 0 || len(h.applies) != 0 || h.quits != 0 {
		t.Fatalf("一条都没排上就不该等、不该应用、更不该退出：%v %v %d", h.waits, h.applies, h.quits)
	}
	last := h.last()
	if last.Stage != event.AutomationStageFailed || last.Reason != event.AutomationReasonDetectRejected {
		t.Fatalf("该报「每个菜单都被拒」，实际 %+v", last)
	}
	if len(last.RejectedMenus) != 2 || !strings.Contains(last.Detail, "目录里没有这个项目") {
		t.Errorf("被拒的菜单和原话都要带出来：%+v", last)
	}
}

func TestAutomationWaitsButDoesNotApplyWhenApplyIsOff(t *testing.T) {
	h := &automationHarness{plan: autoPlan(false, false, "code")}
	h.run(context.Background())
	if len(h.applies) != 0 || h.quits != 0 {
		t.Fatalf("自动应用没开：不该排应用任务，也不该退出：%v %d", h.applies, h.quits)
	}
	if got := strings.Join(h.waits, ","); got != "detect1" {
		t.Errorf("检测那一轮还是要等收口的，实际 %s", got)
	}
	if last := h.last(); last.Stage != event.AutomationStageDone || last.ApplyCount != 0 {
		t.Errorf("收尾是一句没有应用的完成：%+v", last)
	}
}

func TestAutomationKeepsWindowWhenNoCandidateIsUsable(t *testing.T) {
	h := &automationHarness{plan: autoPlan(true, true, "code")}
	h.onApply = func([]string) ([]string, int, error) { return nil, 0, nil }
	h.run(context.Background())

	if got := strings.Join(h.stages(), "|"); got != "detecting|done" {
		t.Fatalf("没有可用候选时到完成就停，不该有 applying：%s", got)
	}
	last := h.last()
	if last.Reason != event.AutomationReasonNoCandidates || last.ApplyCount != 0 {
		t.Errorf("要说清「检测完没有可用候选」：%+v", last)
	}
	if len(h.reports) != 0 {
		t.Errorf("没排应用任务就不必去数队列：%v", h.reports)
	}
	if h.quits != 0 {
		t.Errorf("什么都没应用成，窗口留着让用户看现场，不该退出")
	}
}

func TestAutomationApplyRejectionIsVisible(t *testing.T) {
	h := &automationHarness{plan: autoPlan(true, true, "code")}
	h.onApply = func([]string) ([]string, int, error) { return nil, 0, errors.New("队列已停止") }
	h.run(context.Background())

	last := h.last()
	if last.Stage != event.AutomationStageFailed || last.Reason != event.AutomationReasonApplyRejected {
		t.Fatalf("应用排不进去要报失败：%+v", last)
	}
	if !strings.Contains(last.Detail, "队列已停止") {
		t.Errorf("失败原话要带出来：%+v", last)
	}
	if h.quits != 0 {
		t.Errorf("应用没跑成，不该退出")
	}
}

func TestAutomationCloseWaitsForForeignWork(t *testing.T) {
	h := &automationHarness{plan: autoPlan(true, true, "code")}
	h.onReport = func([]string) automationReport { return automationReport{AppliedOK: 2, OtherActive: 3} }
	h.run(context.Background())

	last := h.last()
	if last.Reason != event.AutomationReasonCloseBusy || last.OtherActive != 3 {
		t.Fatalf("队列里还有别人排的活要说清并带上条数：%+v", last)
	}
	if last.Stage != event.AutomationStageDone {
		t.Errorf("没退出时收尾还是完成（活是用户自己排的），实际 %s", last.Stage)
	}
	if h.quits != 0 {
		t.Errorf("有别人的活在跑，退出会打断它，不该退")
	}
}

func TestAutomationCloseNeedsOneAppliedSuccess(t *testing.T) {
	h := &automationHarness{plan: autoPlan(true, true, "code")}
	h.onReport = func([]string) automationReport { return automationReport{AppliedOK: 0, Active: 0} }
	h.run(context.Background())

	last := h.last()
	if last.Reason != event.AutomationReasonCloseNoResult || last.AppliedCount != 0 {
		t.Fatalf("一项都没应用成功要说清：%+v", last)
	}
	if h.quits != 0 {
		t.Errorf("全失败还关窗口，用户就再也看不到失败了，不该退")
	}
}

func TestAutomationStopsWhenQueueShutsUnderIt(t *testing.T) {
	h := &automationHarness{plan: autoPlan(true, true, "code")}
	h.onWait = func(string) error { return task.ErrQueueStopped }
	h.run(context.Background())

	last := h.last()
	if last.Stage != event.AutomationStageFailed || last.Reason != event.AutomationReasonWaitFailed {
		t.Fatalf("等不到收口要报失败：%+v", last)
	}
	if !strings.Contains(last.Detail, "队列") {
		t.Errorf("失败原话要带出来：%+v", last)
	}
	if len(h.applies) != 0 || h.quits != 0 {
		t.Errorf("检测都没等到，不该排应用更不该退出：%v %d", h.applies, h.quits)
	}
}

func TestAutomationGoesQuietWhenProcessIsLeaving(t *testing.T) {
	// 已经决定退出了：链不再发状态、不再排队，也不再把「等不到」当成一句失败推给拆掉的总线。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h := &automationHarness{plan: autoPlan(true, true, "code")}
	h.run(ctx)
	if len(h.statuses) != 0 || len(h.detects) != 0 || h.quits != 0 {
		t.Fatalf("进程在退出，这条链该立刻收声：%+v %v %d", h.statuses, h.detects, h.quits)
	}

	// 跑一半被取消：等待返回 ctx 的错误，之后不再补一句失败。
	h2 := &automationHarness{plan: autoPlan(true, true, "code")}
	ctx2, cancel2 := context.WithCancel(context.Background())
	h2.onWait = func(string) error { cancel2(); return context.Canceled }
	h2.run(ctx2)
	if got := strings.Join(h2.stages(), "|"); got != "detecting" {
		t.Errorf("取消后不该再推第二句状态，实际 %s", got)
	}
	if len(h2.applies) != 0 || h2.quits != 0 {
		t.Errorf("取消后不该继续往下走：%v %d", h2.applies, h2.quits)
	}
}

func TestAutomationPlanFailureIsReported(t *testing.T) {
	h := &automationHarness{planErr: errors.New("配置服务没准备好，请重启应用")}
	h.run(context.Background())
	last := h.last()
	if last.Stage != event.AutomationStageFailed || last.Reason != event.AutomationReasonPlanFailed {
		t.Fatalf("开局就读不到东西要说清：%+v", last)
	}
	if !strings.Contains(last.Detail, "配置服务") {
		t.Errorf("原话要带出来：%+v", last)
	}
}

// --- 下面是真接缝：配置、目录、检测结果、队列都用真的 ---

func TestItemsOfMenuSkipsItemsWithoutHostnames(t *testing.T) {
	svc := newApplyService(t, &fakePriv{})

	if got := svc.itemsOfMenu("code"); strings.Join(got, ",") != "api" {
		t.Errorf("「代码与下载」里那条没有域名的手动映射不该进检测，实际 %v", got)
	}
	if got := svc.itemsOfMenu("store"); strings.Join(got, ",") != "steam" {
		t.Errorf("按菜单展开项目不对，实际 %v", got)
	}
	if got := svc.itemsOfMenu("no-such-menu"); len(got) != 0 {
		t.Errorf("目录里没有的菜单不该展开出项目，实际 %v", got)
	}
}

func TestAutomationPlanReadsMenusFromSettings(t *testing.T) {
	svc := newApplyService(t, &fakePriv{})

	// 出厂默认一个菜单都没勾：这条链不开。
	plan, err := svc.automationPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Startup.AutoDetectMenus) != 0 || len(plan.Batches) != 0 {
		t.Fatalf("默认配置不该展开出任何一批：%+v", plan)
	}

	if _, err := svc.settings.Save(config.Patch{
		AutoDetectMenus: &[]string{"code", "store"},
	}, 0, "m1"); err != nil {
		t.Fatal(err)
	}
	plan, err = svc.automationPlan()
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Batches) != 2 || plan.Batches[0].MenuID != "code" || plan.Batches[1].MenuID != "store" {
		t.Fatalf("勾了两个菜单就该有两批，按勾选顺序：%+v", plan.Batches)
	}
	if got := strings.Join(plan.Batches[0].ItemIDs, ","); got != "api" {
		t.Errorf("第一批的项目不对：%s", got)
	}
}

func TestAutomationApplyOnlyQueuesUsableItems(t *testing.T) {
	svc := newApplyService(t, &fakePriv{})
	// api 有可用候选；steam 有检测结果，但一颗可用候选都没有。
	// ApplyItems 是整批校验的，不做这一步筛选的话一个用不了的项目会把整批一起挡下。
	putAvailableResult(svc, "api")
	svc.detectResults.put(detectScopeSteam, "steam-method", &accelerator.Result{
		ItemID: "steam",
		Candidates: []accelerator.Candidate{
			{ID: "s1", Hostname: detectHostSteam, IP: "203.0.113.99", Status: accelerator.StatusTimeout},
		},
		Hostnames: []accelerator.HostOutcome{{Hostname: detectHostSteam, Resolved: true}},
	})

	rounds, queued, err := svc.automationApply([]string{"api", "steam"})
	if err != nil {
		t.Fatalf("筛过之后不该再被整批拒绝：%v", err)
	}
	if queued != 1 || len(rounds) != 1 || rounds[0] == "" {
		t.Fatalf("只该排进 1 项、归进 1 个轮：%d %v", queued, rounds)
	}
	snap := svc.GetTasks()
	if len(snap.Tasks) != 1 || snap.Tasks[0].ItemID != "api" || snap.Tasks[0].RoundID != rounds[0] {
		t.Fatalf("队列里应是 api 那条应用任务：%+v", snap.Tasks)
	}
	if len(snap.Rounds) != 1 || snap.Rounds[0].Kind != task.KindApply {
		t.Errorf("应用轮要进快照：%+v", snap.Rounds)
	}
}

func TestAutomationApplyOpensNoRoundWhenNothingIsUsable(t *testing.T) {
	svc := newApplyService(t, &fakePriv{})

	rounds, queued, err := svc.automationApply([]string{"api"})
	if err != nil || queued != 0 || len(rounds) != 0 {
		t.Fatalf("没有可用候选时该给「0 项、没有轮」：%v %d %v", err, queued, rounds)
	}
	snap := svc.GetTasks()
	if len(snap.Tasks) != 0 || len(snap.Rounds) != 0 {
		t.Fatalf("空轮不该在界面上占一个点不开的空根节点：%+v %+v", snap.Tasks, snap.Rounds)
	}
}

func TestAutomationReportSeparatesForeignWork(t *testing.T) {
	exec := newGatedExecutor()
	exec.outcomes["okapply"] = task.Outcome{}
	svc := &Service{tasks: task.NewQueue(exec, nil), detectResults: newDetectResultStore()}
	round := svc.tasks.BeginRound(task.KindApply, "github", "应用队列 · 2 项")
	enqueue := func(kind task.Kind, itemID string, roundID string) string {
		t.Helper()
		tk, err := svc.tasks.Enqueue(task.NewRequest{Kind: kind, ItemID: itemID, ScopeID: "github", RoundID: roundID})
		if err != nil {
			t.Fatal(err)
		}
		return tk.ID
	}
	done := enqueue(task.KindApply, "okapply", round)
	foreignKey := enqueue(task.KindRestore, "hold", "")

	waitFor(t, "这一轮的应用任务跑完", func() bool {
		item, ok := findTask(svc.GetTasks(), done)
		return ok && item.Status == task.StatusDone
	})
	waitFor(t, "别人的还原任务正在跑", func() bool {
		item, ok := findTask(svc.GetTasks(), foreignKey)
		return ok && item.Status == task.StatusRestoring
	})

	rep := svc.automationReport([]string{round})
	if rep.AppliedOK != 1 || rep.Active != 0 || rep.OtherActive != 1 {
		t.Errorf("成功数、本轮在途数、别人的在途数都要分开数：%+v", rep)
	}
	// 没给轮 ID 时全部算别人的活：这条链一轮都没排上却读到东西，说明轮 ID 传错了。
	if rep := svc.automationReport(nil); rep.AppliedOK != 0 || rep.OtherActive != 1 {
		t.Errorf("空轮 ID 时正在跑的那条算别人的活：%+v", rep)
	}
	close(exec.hold)
}

func TestAutomationStatusIsStickyAndEmitted(t *testing.T) {
	svc := newApplyService(t, &fakePriv{})
	if got := svc.GetAutomationStatus(); got.Stage != "" {
		t.Fatalf("还没跑过自动化时不该有一句结论：%+v", got)
	}

	var emitted []any
	d := newAutomationDeps(svc, func() {}, func(name string, payload any) {
		if name != event.AutomationStatusName {
			t.Errorf("事件名不对：%s", name)
		}
		emitted = append(emitted, payload)
	})
	want := event.AutomationStatus{Stage: event.AutomationStageDone, Menus: []string{"code"}, DetectCount: 3}
	d.status(want)

	// AutomationStatus 里有切片，不能用 == 比：整份比一次内容。
	if got := svc.GetAutomationStatus(); !reflect.DeepEqual(got, want) {
		t.Errorf("界面补读的最近一份不对：got %+v want %+v", got, want)
	}
	if len(emitted) != 1 || !reflect.DeepEqual(emitted[0], want) {
		t.Errorf("同一份结论要往总线推一次：%v", emitted)
	}
	// emit 给 nil（还没接总线的阶段）只存不发，不 panic。
	quiet := newAutomationDeps(svc, func() {}, nil)
	quiet.status(event.AutomationStatus{Stage: event.AutomationStageDetecting})
	if got := svc.GetAutomationStatus(); got.Stage != event.AutomationStageDetecting {
		t.Errorf("没接总线也要留下最近一份：%+v", got)
	}
}

func TestAutomationWiringIsRealDepsAndNotAFrontendCommand(t *testing.T) {
	svc := newApplyService(t, &fakePriv{})
	// 真接缝必须落在真实队列上：等错对象会永久卡住，也不会由假件替它说话。
	d := newAutomationDeps(svc, func() {}, nil)
	if err := d.wait(context.Background(), "r-no-such-round"); err != nil {
		t.Errorf("没排过任务的轮 ID 应算已收口：%v", err)
	}
	if got := d.report([]string{"r-no-such-round"}); got != (automationReport{}) {
		t.Errorf("空队列读数应是三个 0：%+v", got)
	}

	// 入口必须是包级函数：Service 的每个导出方法都会被 Wails 绑成前端能调的命令，
	// 而「重跑一遍启动自动化」不能让界面点出来。
	svcType := reflect.TypeOf(&Service{})
	for _, forbidden := range []string{"StartStartupAutomation", "RunStartupAutomation", "Quit"} {
		if _, ok := svcType.MethodByName(forbidden); ok {
			t.Errorf("Service 上不该有 %s 这个方法：前端能调到它就越过边界", forbidden)
		}
	}
	if _, ok := svcType.MethodByName("GetAutomationStatus"); !ok {
		t.Error("GetAutomationStatus 这条只读命令必须绑给前端，界面挂载时要补读最近一份")
	}

	// 接线得真的接上：装配层不启动这条链的话，前面一切都只是没接电的零件。
	src := readAt(t, repoRoot(t), "main.go")
	line := ""
	for _, candidate := range strings.Split(src, "\n") {
		if strings.Contains(candidate, "appsvc.StartStartupAutomation(") {
			line = strings.TrimSpace(candidate)
		}
	}
	const want = "appsvc.StartStartupAutomation(ctx, service, platformApp.Quit, emitProgress)"
	if line != want {
		t.Errorf("main.go 里启动自动化的接线不是这一行：\n got: %s\nwant: %s", line, want)
	}
}

func TestAutomationDetectUsesTheRealDetectEntry(t *testing.T) {
	table := fakeTable(t, map[string][]strategies.Strategy{
		detectScopeGitHub: {fakeStrategy("live.one", "还在的方法", detectScopeGitHub)},
	}, map[string]strategies.Strategy{"live.one": fakeStrategy("live.one", "还在的方法", detectScopeGitHub)})
	svc := newDetectEntryService(t, table, newGatedExecutor())
	if _, err := svc.SelectStrategy(detectScopeGitHub, "live.one", 0, "m1"); err != nil {
		t.Fatalf("先给这个范围选上方法: %v", err)
	}

	rounds, err := svc.automationDetect([]string{"api"})
	if err != nil {
		t.Fatalf("排检测任务失败：%v", err)
	}
	if len(rounds) != 1 || rounds[0] == "" {
		t.Fatalf("一批检测要归进一个轮：%v", rounds)
	}
	// 「商店」这个范围没选方法：入口整批拒，这条链据此把那个菜单点成「被拒」，
	// 而不是让它把别的菜单一起拖死。
	if _, err := svc.automationDetect([]string{"steam"}); codeOf(t, err) != CodeNoStrategySelected {
		t.Errorf("范围没选方法该在入口被拒，实际 %v", err)
	}
	snap := svc.GetTasks()
	if len(snap.Tasks) != 1 || snap.Tasks[0].RoundID != rounds[0] {
		t.Errorf("被拒的那一批不该在队列里留下任务：%+v", snap.Tasks)
	}
}
