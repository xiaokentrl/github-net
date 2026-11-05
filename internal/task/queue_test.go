package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// 这一批测试全程不打网络、不碰 Hosts、不读系统状态：执行动作是假件。
// 要验的是队列自己的记账——顺序、状态流转、清理和事件序号。

// gate 是一次性放行开关：open 可以随便调多次，不会二次关闭而 panic。
type gate struct {
	ch   chan struct{}
	once sync.Once
}

func newGate() *gate { return &gate{ch: make(chan struct{})} }

func (g *gate) open() { g.once.Do(func() { close(g.ch) }) }

func (g *gate) wait(ctx context.Context) bool {
	select {
	case <-g.ch:
		return true
	case <-ctx.Done():
		return false
	}
}

type fakeExecutor struct {
	decide func(ctx context.Context, tk Task, report Report) Outcome
	// started 每领取一条就非阻塞投一份快照，用来确认「这条确实开始执行了」。
	started chan Task

	mu        sync.Mutex
	itemOrder []string
	running   int
	peak      int
}

func (f *fakeExecutor) Execute(ctx context.Context, tk Task, report Report) Outcome {
	f.mu.Lock()
	f.itemOrder = append(f.itemOrder, tk.ItemID)
	f.running++
	if f.running > f.peak {
		f.peak = f.running
	}
	f.mu.Unlock()
	select {
	case f.started <- tk:
	default:
	}
	out := Outcome{}
	if f.decide != nil {
		out = f.decide(ctx, tk, report)
	}
	f.mu.Lock()
	f.running--
	f.mu.Unlock()
	return out
}

func (f *fakeExecutor) seen() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.itemOrder, ",")
}

func (f *fakeExecutor) peakConcurrency() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.peak
}

// successExecutor 一律按任务类型成功。
func successExecutor() *fakeExecutor {
	return &fakeExecutor{started: make(chan Task, 32)}
}

// gateExecutor 让任务卡在开始处，直到放行或 ctx 被取消。
// 被取消时返回失败：应用退出时那条任务就以这个结果定稿。
func gateExecutor(g *gate) *fakeExecutor {
	return &fakeExecutor{
		started: make(chan Task, 32),
		decide: func(ctx context.Context, tk Task, report Report) Outcome {
			if g.wait(ctx) {
				return Outcome{}
			}
			return Outcome{Status: StatusFailed, Message: "执行器被取消"}
		},
	}
}

type recorder struct {
	mu     sync.Mutex
	events []Progress
}

func (r *recorder) PublishProgress(p Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, p)
}

func (r *recorder) all() []Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Progress(nil), r.events...)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func newEnv(exec Executor) (*Queue, *recorder) {
	rec := &recorder{}
	return NewQueue(exec, rec), rec
}

func mustEnqueue(t *testing.T, q *Queue, req NewRequest) Task {
	t.Helper()
	tk, err := q.Enqueue(req)
	if err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	return tk
}

// statusOf 读一条任务当前的状态；队列里没有这条记录时返回 false。
func statusOf(q *Queue, id string) (Task, bool) {
	for _, tk := range q.Snapshot().Tasks {
		if tk.ID == id {
			return tk, true
		}
	}
	return Task{}, false
}

// waitForTask 轮询快照直到某条任务变成期望状态；超时即失败，不留悬空等待。
func waitForTask(t *testing.T, q *Queue, id string, want Status) Task {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if tk, ok := statusOf(q, id); ok && tk.Status == want {
			return tk
		}
		time.Sleep(2 * time.Millisecond)
	}
	tk, _ := statusOf(q, id)
	t.Fatalf("等任务 %s 变成 %s 超时，当前 %+v", id, want, tk)
	return Task{}
}

// drain 等到队列里没有等待、执行中、恢复中的任务。
func drain(t *testing.T, q *Queue) Snapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snap := q.Snapshot()
		active := false
		for _, tk := range snap.Tasks {
			if IsActive(tk.Status) {
				active = true
				break
			}
		}
		if !active {
			return snap
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("队列没能排空: %+v", q.Snapshot().Tasks)
	return Snapshot{}
}

// startedTask 从假执行器取到「这条确实开始执行了」的确认。
func startedTask(t *testing.T, f *fakeExecutor) Task {
	t.Helper()
	select {
	case tk := <-f.started:
		return tk
	case <-time.After(2 * time.Second):
		t.Fatal("任务没有开始执行")
		return Task{}
	}
}

// settleAfter 给固定延时，用来观察「驱动已经退出」这种没有事件可等的状态。
func settleAfter(d time.Duration) { time.Sleep(d) }

func TestEnqueueValidatesAndDefaultsName(t *testing.T) {
	q, _ := newEnv(successExecutor())

	if _, err := q.Enqueue(NewRequest{Kind: "probe", ItemID: "gh"}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("不认识的任务类型应被拒, 实际 %v", err)
	}
	if _, err := q.Enqueue(NewRequest{Kind: KindDetect, ItemID: "  "}); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("缺项目 ID 应被拒, 实际 %v", err)
	}
	tk := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh"})
	if tk.Name != "gh" {
		t.Errorf("Name 为空时应由项目 ID 顶上, 实际 %q", tk.Name)
	}
	drain(t, q)
}

func TestEnqueueDedupeWhileActive(t *testing.T) {
	g := newGate()
	defer g.open()
	f := gateExecutor(g)
	q, rec := newEnv(f)

	first := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh"})
	// 等这条真的被执行器领走并卡住：之后它不再自己变状态，事件数才是一个能比的数。
	waitForTask(t, q, first.ID, StatusRunning)
	before := rec.count()

	second := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh"})
	if first.ID != second.ID {
		t.Errorf("同项目同类在途任务不该重复排队: %s vs %s", first.ID, second.ID)
	}
	if rec.count() != before {
		t.Errorf("重复入队不该再发事件：界面不该看到两条, 实际多出 %d", rec.count()-before)
	}
	if len(q.Snapshot().Tasks) != 1 {
		t.Fatalf("队列里应该只有一条, 实际 %+v", q.Snapshot().Tasks)
	}
	other := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "gh"})
	if other.ID == first.ID {
		t.Error("应用任务和检测任务是两条不同记录，去重只挡同类型")
	}

	g.open()
	drain(t, q)

	third := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh"})
	if third.ID == first.ID {
		t.Error("已经结束的旧任务不该挡住新的排队")
	}
	drain(t, q)
}

func TestEnqueueSkippedLandsTerminalAndNeverRuns(t *testing.T) {
	g := newGate()
	defer g.open()
	f := gateExecutor(g)
	q, _ := newEnv(f)

	roundID := q.BeginRound(KindApply, "", "应用队列 · 2 项")
	normal := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "api", RoundID: roundID})
	skipped, err := q.EnqueueSkipped(NewRequest{Kind: KindApply, ItemID: "bare", RoundID: roundID}, "这个项目还没有检测结果，先检测再应用")
	if err != nil {
		t.Fatalf("该能排一条跳过: %v", err)
	}
	if skipped.Status != StatusSkipped {
		t.Errorf("跳过任务排进去就该是终态, 实际 %s", skipped.Status)
	}
	if skipped.ErrorMsg != "这个项目还没有检测结果，先检测再应用" {
		t.Errorf("原因要写在这条任务上，界面才有得显示, 实际 %q", skipped.ErrorMsg)
	}
	if skipped.FinishedAt == "" {
		t.Error("终态任务要有结束时间，不然轮次进度算不出「这一轮做完了」")
	}
	if skipped.RoundID != roundID {
		t.Errorf("跳过那条也归在这一轮里, 实际 %q vs %q", skipped.RoundID, roundID)
	}

	// 同项目同类已经在队列里（哪怕还没跑完）时，跳过不能把它改写成终态。
	again, err := q.EnqueueSkipped(NewRequest{Kind: KindApply, ItemID: "api", RoundID: roundID}, "不该盖掉在途任务")
	if err != nil {
		t.Fatalf("重复排同项目该返回已在的那条: %v", err)
	}
	if again.ID != normal.ID || again.Status == StatusSkipped {
		t.Errorf("在途任务不该被跳过那条顶掉: %+v vs %+v", again, normal)
	}

	g.open()
	drain(t, q)

	if got := f.seen(); got != "api" {
		t.Errorf("只有等待那条该被执行器领走, 实际领走了 %q", got)
	}
	tk, ok := statusOf(q, skipped.ID)
	if !ok || tk.Status != StatusSkipped {
		t.Errorf("跑完整批后跳过那条仍是跳过, 实际 %+v ok=%v", tk, ok)
	}
}

func TestSerialOrderAndSingleConcurrency(t *testing.T) {
	g := newGate()
	defer g.open()
	f := gateExecutor(g)
	q, _ := newEnv(f)

	for _, id := range []string{"a", "b", "c"} {
		mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: id})
	}
	first := startedTask(t, f)
	if first.ItemID != "a" {
		t.Errorf("应按入队顺序执行, 先跑的是 %s", first.ItemID)
	}
	// 第一条还没结束，后两条必须仍等待，且并发数不超过 1。
	for _, tk := range q.Snapshot().Tasks {
		want := StatusRunning
		if tk.ItemID != "a" {
			want = StatusWaiting
		}
		if tk.Status != want {
			t.Errorf("%s 状态应为 %s, 实际 %s", tk.ItemID, want, tk.Status)
		}
	}
	if got := f.peakConcurrency(); got > 1 {
		t.Errorf("同一时刻只该有一条任务在执行, 峰值 %d", got)
	}

	g.open()
	drain(t, q)
	if got := f.seen(); got != "a,b,c" {
		t.Errorf("执行顺序应为 a,b,c, 实际 %s", got)
	}
}

func TestSequenceMonotonicAndWatermark(t *testing.T) {
	f := &fakeExecutor{
		started: make(chan Task, 8),
		decide: func(ctx context.Context, tk Task, report Report) Outcome {
			report("backup", "备份 hosts…", 40)
			report("write", "写入域名映射…", 250) // 超界的比例要裁到 100
			return Outcome{}
		},
	}
	q, rec := newEnv(f)

	tk := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "gh"})
	done := waitForTask(t, q, tk.ID, StatusDone)

	events := rec.all()
	if len(events) == 0 {
		t.Fatal("应该有进度事件")
	}
	for i, ev := range events {
		if ev.Sequence != int64(i+1) {
			t.Fatalf("第 %d 条事件序号应为 %d, 实际 %d", i+1, i+1, ev.Sequence)
		}
	}
	snap := q.Snapshot()
	if snap.Sequence != events[len(events)-1].Sequence {
		t.Errorf("快照水位应等于最后一条事件序号: 快照 %d, 事件 %d", snap.Sequence, events[len(events)-1].Sequence)
	}
	last := events[len(events)-1]
	if last.Task.Phase != "write" || last.Task.Progress != 100 || last.Task.PhaseText != "写入域名映射…" {
		t.Errorf("进度内容没按回调更新: %+v", last.Task)
	}
	if done.Duration == "" || done.FinishedAt == "" || done.StartedAt == "" {
		t.Errorf("终态应带开始、结束和耗时: %+v", done)
	}
	// 状态推进必须逐档来：等待→执行中→完成，不能跳。
	var got []string
	for _, ev := range events {
		if len(got) == 0 || got[len(got)-1] != string(ev.Task.Status) {
			got = append(got, string(ev.Task.Status))
		}
	}
	if strings.Join(got, ",") != "waiting,running,done" {
		t.Errorf("状态推进应为 waiting→running→done, 实际 %s", strings.Join(got, ","))
	}
}

func TestOutcomeMappingPerKind(t *testing.T) {
	cases := []struct {
		name     string
		kind     Kind
		outcome  Outcome
		wantRun  Status
		wantDone Status
		wantMsg  string
	}{
		{name: "检测默认成功", kind: KindDetect, wantRun: StatusRunning, wantDone: StatusDone},
		{name: "应用默认成功", kind: KindApply, wantRun: StatusRunning, wantDone: StatusDone},
		{name: "恢复默认成功", kind: KindRestore, wantRun: StatusRestoring, wantDone: StatusRestored},
		{name: "恢复报完成被纠正", kind: KindRestore, outcome: Outcome{Status: StatusDone}, wantRun: StatusRestoring, wantDone: StatusRestored},
		{name: "跳过", kind: KindDetect, outcome: Outcome{Status: StatusSkipped, Message: "已有生效记录"}, wantRun: StatusRunning, wantDone: StatusSkipped, wantMsg: "已有生效记录"},
		{name: "跳过没给原因", kind: KindDetect, outcome: Outcome{Status: StatusSkipped}, wantRun: StatusRunning, wantDone: StatusSkipped, wantMsg: "这次任务被跳过，执行器没有给出原因"},
		{name: "失败", kind: KindApply, outcome: Outcome{Status: StatusFailed, Message: "候选地址失效"}, wantRun: StatusRunning, wantDone: StatusFailed, wantMsg: "候选地址失效"},
		{name: "回滚", kind: KindApply, outcome: Outcome{Status: StatusRolledback, Message: "写入失败已退回"}, wantRun: StatusRunning, wantDone: StatusRolledback, wantMsg: "写入失败已退回"},
		{name: "回滚没给原因", kind: KindApply, outcome: Outcome{Status: StatusRolledback}, wantRun: StatusRunning, wantDone: StatusRolledback, wantMsg: "任务失败且已退回改动前的状态，执行器没有给出原因"},
		{name: "结果认不出来按失败收口", kind: KindApply, outcome: Outcome{Status: Status("weird")}, wantRun: StatusRunning, wantDone: StatusFailed, wantMsg: "执行器返回了无法识别的结果，按失败处理"},
		{name: "成功不留文案", kind: KindDetect, outcome: Outcome{Status: StatusDone, Message: "顺手一句"}, wantRun: StatusRunning, wantDone: StatusDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.outcome
			// 卡住执行器，好让「执行中」这一档真的被看到；放行后才交回预设结果。
			g := newGate()
			defer g.open()
			f := &fakeExecutor{started: make(chan Task, 4), decide: func(ctx context.Context, tk Task, report Report) Outcome {
				if !g.wait(ctx) {
					return Outcome{Status: StatusFailed, Message: "执行器被取消"}
				}
				return out
			}}
			q, _ := newEnv(f)
			tk := mustEnqueue(t, q, NewRequest{Kind: tc.kind, ItemID: "x"})
			waitForTask(t, q, tk.ID, tc.wantRun)
			g.open()
			got := waitForTask(t, q, tk.ID, tc.wantDone)
			if got.ErrorMsg != tc.wantMsg {
				t.Errorf("错误文案应为 %q, 实际 %q", tc.wantMsg, got.ErrorMsg)
			}
		})
	}
}

func TestEnqueueAfterShutdownRefused(t *testing.T) {
	q, _ := newEnv(successExecutor())
	mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a"})
	drain(t, q)
	q.Shutdown()
	if _, err := q.Enqueue(NewRequest{Kind: KindDetect, ItemID: "b"}); !errors.Is(err, ErrQueueStopped) {
		t.Errorf("停止后入队应报队列已停止, 实际 %v", err)
	}
}

func TestClearFinishedKeepsActiveAndIsIdempotent(t *testing.T) {
	g := newGate()
	defer g.open()
	q, _ := newEnv(gateExecutor(g))

	running := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "a"})
	skipped, err := q.EnqueueSkipped(NewRequest{Kind: KindDetect, ItemID: "b"}, "这个项目还没有检测结果")
	if err != nil {
		t.Fatalf("该能排一条跳过: %v", err)
	}
	waitForTask(t, q, running.ID, StatusRunning)
	// 一条执行中、一条已跳过：清理已结束只该带走终态那条。
	if n := q.ClearFinished(); n != 1 {
		t.Errorf("应移除 1 条终态, 实际 %d", n)
	}
	snap := q.Snapshot()
	if len(snap.Tasks) != 1 || snap.Tasks[0].ID != running.ID {
		t.Errorf("执行中的必须留下: %+v", snap.Tasks)
	}
	if _, ok := statusOf(q, skipped.ID); ok {
		t.Errorf("终态那条该被清掉: %+v", snap.Tasks)
	}
	if n := q.ClearFinished(); n != 0 {
		t.Errorf("没有终态时清理应幂等返回 0, 实际 %d", n)
	}
	before := q.Sequence()
	g.open()
	drain(t, q)
	if n := q.ClearFinished(); n != 1 {
		t.Errorf("跑完后应清掉 1 条, 实际 %d", n)
	}
	if len(q.Snapshot().Tasks) != 0 {
		t.Errorf("队列应已空: %+v", q.Snapshot().Tasks)
	}
	if q.Sequence() <= before {
		t.Error("清理不该重置事件序号，也不该让水位倒退")
	}
}

func TestShutdownStopsDriverAndFinalizesRunning(t *testing.T) {
	g := newGate()
	defer g.open()
	f := gateExecutor(g)
	q, rec := newEnv(f)

	running := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a"})
	waiting := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "b"})
	waitForTask(t, q, running.ID, StatusRunning)
	before := rec.count()

	q.Shutdown()
	// Shutdown 会给执行器取消信号，跑完那条定稿，等待那条不再被领取。
	if tk, _ := statusOf(q, running.ID); !IsTerminal(tk.Status) {
		t.Errorf("退出时正在跑的任务应定稿, 实际 %s", tk.Status)
	}
	if tk, _ := statusOf(q, waiting.ID); tk.Status != StatusWaiting {
		t.Errorf("退出后不该领取等待任务, 实际 %s", tk.Status)
	}
	if rec.count() <= before {
		t.Error("定稿要发事件，否则前端水位对不上")
	}
	seqAfter := q.Sequence()
	settleAfter(80 * time.Millisecond)
	if q.Sequence() != seqAfter {
		t.Error("退出后事件序号不该继续增长")
	}
}

func TestNilPublisherStillRuns(t *testing.T) {
	q := NewQueue(successExecutor(), nil)
	tk := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "a"})
	if got := waitForTask(t, q, tk.ID, StatusDone); got.Status != StatusDone {
		t.Errorf("没接事件总线也要跑完, 实际 %s", got.Status)
	}
}

func TestTerminalAndActiveSets(t *testing.T) {
	// 前端按钮的启用条件依赖这两个集合，必须和规划里的五档终态一致。
	for _, s := range []Status{StatusDone, StatusSkipped, StatusFailed, StatusRolledback, StatusRestored} {
		if !IsTerminal(s) {
			t.Errorf("%s 应是终态", s)
		}
		if IsActive(s) {
			t.Errorf("%s 不该算在途", s)
		}
	}
	for _, s := range []Status{StatusWaiting, StatusRunning, StatusRestoring} {
		if !IsActive(s) {
			t.Errorf("%s 应算在途", s)
		}
		if IsTerminal(s) {
			t.Errorf("%s 不该是终态", s)
		}
	}
	if IsTerminal(Status("weird")) || IsActive(Status("weird")) {
		t.Error("不认识的状态既不是终态也不是在途")
	}
}

func TestSnapshotIsCopyAndAlwaysArray(t *testing.T) {
	q, _ := newEnv(successExecutor())
	empty := q.Snapshot()
	if empty.Tasks == nil {
		t.Error("空队列也要给数组形状，前端不该撞上 null")
	}
	if len(empty.Tasks) != 0 || empty.Sequence != 0 {
		t.Errorf("新队列应是空的、水位 0: %+v", empty)
	}

	g := newGate()
	defer g.open()
	gated, _ := newEnv(gateExecutor(g))
	tk := mustEnqueue(t, gated, NewRequest{Kind: KindDetect, ItemID: "a", Name: "GitHub 主站"})
	// 卡住执行器：这条的状态不会自己变，比对副本才有意义。
	waitForTask(t, gated, tk.ID, StatusRunning)
	snap := gated.Snapshot()
	if len(snap.Tasks) != 1 {
		t.Fatalf("快照应有 1 条: %+v", snap.Tasks)
	}
	snap.Tasks[0].Status = StatusFailed
	snap.Tasks[0].Name = "被外面改了"
	fresh := gated.Snapshot()
	if fresh.Tasks[0].Name != "GitHub 主站" || fresh.Tasks[0].Status != StatusRunning {
		t.Errorf("快照是副本，外面改不动队列里的记录: %+v", fresh.Tasks[0])
	}
	if tk.Status != StatusWaiting {
		t.Errorf("入队返回的也应是等待状态的副本: %+v", tk)
	}
	g.open()
	drain(t, gated)
}

/* 轮次（界面里的根节点）：ID 只由队列发号、名字有兜底、空轮不现身、清任务时一并带走。 */

// roundByID 从快照里按 ID 取一轮；没有则返回 false。
func roundByID(snap Snapshot, id string) (Round, bool) {
	for _, r := range snap.Rounds {
		if r.ID == id {
			return r, true
		}
	}
	return Round{}, false
}

// roundRegistry 直接看队列内部登记了几轮、顺序表多长。
// 清理到底有没有干活只能这么看：快照本来就只列有任务的轮，光看快照看不出差别。
func roundRegistry(q *Queue) (int, int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.rounds), len(q.roundIDs)
}

func TestBeginRoundIDsUniqueAndSnapshotKeepsOpenOrder(t *testing.T) {
	q, _ := newEnv(successExecutor())
	first := q.BeginRound(KindDetect, "github", "代码与下载")
	second := q.BeginRound(KindApply, "", "应用队列 · 2 项")
	if first == "" || first == second {
		t.Fatalf("每轮要有自己的 ID: %q / %q", first, second)
	}

	// 先往第二轮排任务：快照里轮的顺序仍是开轮先后，不是排队先后。
	mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a", RoundID: second})
	snap := q.Snapshot()
	if len(snap.Rounds) != 1 || snap.Rounds[0].ID != second {
		t.Fatalf("只有排了任务的轮进快照: %+v", snap.Rounds)
	}
	if snap.Rounds[0].Kind != KindApply || snap.Rounds[0].ScopeID != "" {
		t.Errorf("轮要带上类型和范围: %+v", snap.Rounds[0])
	}
	if _, err := time.Parse(time.RFC3339, snap.Rounds[0].CreatedAt); err != nil {
		t.Errorf("开轮时间要能被前端解析: %q %v", snap.Rounds[0].CreatedAt, err)
	}

	mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "b", RoundID: first})
	snap = q.Snapshot()
	if len(snap.Rounds) != 2 || snap.Rounds[0].ID != first || snap.Rounds[1].ID != second {
		t.Fatalf("轮该按开轮先后列出: %+v", snap.Rounds)
	}
	if snap.Rounds[0].Name != "代码与下载" {
		t.Errorf("轮名要用后端给的那个: %+v", snap.Rounds[0])
	}
	drain(t, q)
}

func TestRoundNameFallsBackToKindLabel(t *testing.T) {
	q, _ := newEnv(successExecutor())
	cases := []struct {
		kind Kind
		want string
	}{
		{KindDetect, "检测"},
		{KindApply, "应用"},
		{KindRestore, "恢复"},
	}
	for i, c := range cases {
		id := q.BeginRound(c.kind, " github ", "   ")
		if id != fmt.Sprintf("r%d", i+1) {
			t.Errorf("第 %d 次开轮的 ID 应是 r%d, 实际 %q", i+1, i+1, id)
		}
		mustEnqueue(t, q, NewRequest{Kind: c.kind, ItemID: fmt.Sprintf("item%d", i), RoundID: id})
		r, ok := roundByID(q.Snapshot(), id)
		if !ok {
			t.Fatalf("%s 这一轮没进快照", c.kind)
		}
		if r.Name != c.want {
			t.Errorf("%s 类型的轮名兜底应是 %q, 实际 %q", c.kind, c.want, r.Name)
		}
		if r.ScopeID != "github" {
			t.Errorf("轮的范围两侧空格要清掉: %q", r.ScopeID)
		}
	}
	drain(t, q)
}

// 开轮和排第一条任务之间被别的读插进来，也不能把这一轮剪空——
// 否则整批任务挂在一个已经不存在的轮上，界面上看不见根节点。
func TestSnapshotReadDoesNotPruneFreshEmptyRound(t *testing.T) {
	q, _ := newEnv(successExecutor())
	id := q.BeginRound(KindDetect, "github", "代码与下载")
	if got := q.Snapshot().Rounds; len(got) != 0 {
		t.Fatalf("空轮不该进快照: %+v", got)
	}
	// 读一次（模拟界面的轮询），再排任务。
	q.Snapshot()
	q.Snapshot()
	mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "api", RoundID: id})
	r, ok := roundByID(q.Snapshot(), id)
	if !ok || r.Name != "代码与下载" {
		t.Fatalf("被读快照插过的轮该照常现身: %+v", q.Snapshot().Rounds)
	}
	drain(t, q)
}

func TestTaskWithUnknownRoundStillLists(t *testing.T) {
	q, _ := newEnv(successExecutor())
	tk := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a", RoundID: "r404"})
	snap := q.Snapshot()
	if len(snap.Rounds) != 0 {
		t.Errorf("队列没登记过的轮不该凭空出现: %+v", snap.Rounds)
	}
	if tk.RoundID != "r404" {
		t.Errorf("任务上的轮 ID 要原样带出，前端按它分组: %q", tk.RoundID)
	}
	drain(t, q)
}

func TestClearFinishedPrunesEmptiedRounds(t *testing.T) {
	q, _ := newEnv(successExecutor())

	first := q.BeginRound(KindDetect, "github", "代码与下载")
	second := q.BeginRound(KindDetect, "github", "商店")
	q.BeginRound(KindDetect, "github", "开了没排任务的一轮")
	mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "a", RoundID: first})
	mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "b", RoundID: second})
	drain(t, q)
	// 两条都进终态了：清理任务的同时把空掉的轮一起带走，界面不会剩空根、内存也不会攒轮。
	if n := q.ClearFinished(); n != 2 {
		t.Fatalf("应清掉 2 条, 实际 %d", n)
	}
	if got := q.Snapshot().Rounds; len(got) != 0 {
		t.Errorf("任务清空后轮也该消失: %+v", got)
	}
	for _, id := range []string{first, second} {
		if _, ok := roundByID(q.Snapshot(), id); ok {
			t.Errorf("%s 这一轮该被清掉", id)
		}
	}
	if rounds, order := roundRegistry(q); rounds != 0 || order != 0 {
		t.Errorf("清理该把轮登记和顺序表一起腾空，实际 %d / %d", rounds, order)
	}
	// 没任务的轮不会再被登记回来：重复清理幂等。
	if n := q.ClearFinished(); n != 0 {
		t.Errorf("没有终态时清理应返回 0, 实际 %d", n)
	}
	// 轮号不回收：复用旧 ID 会把清理前就在跑的任务错并进新轮。
	if next := q.BeginRound(KindDetect, "github", "代码与下载"); next == first {
		t.Errorf("清掉一轮后开新轮不该顶用旧 ID: %q", next)
	}
}

// --- WaitRound：等一轮跑完 ---

// roundErr 等 WaitRound 给结果；超时说明等待方叫不醒。
func roundErr(t *testing.T, ch chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("WaitRound 两秒内没有返回")
		return nil
	}
}

// stillWaiting 确认这段时间里 WaitRound 没有返回。
func stillWaiting(t *testing.T, ch chan error, d time.Duration) {
	t.Helper()
	select {
	case err := <-ch:
		t.Fatalf("这一轮还没收口就返回了: %v", err)
	case <-time.After(d):
	}
}

// settledCtx 给同步调用的 WaitRound 一个时间上限：判据写反时它带着 ctx 的错误回来，
// 而不是把整包测试挂到超时。收口正常时这点时间用不到。
func settledCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestWaitRoundSettlesWhenNothingIsRunning(t *testing.T) {
	q, _ := newEnv(successExecutor())
	round := q.BeginRound(KindDetect, "github", "检测 GitHub · 1 项")
	tk := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh", ScopeID: "github", RoundID: round})
	drain(t, q)
	if got, ok := statusOf(q, tk.ID); !ok || got.Status != StatusDone {
		t.Fatalf("终态记录应还留在队列里等用户清理: %+v ok=%v", got, ok)
	}
	// 谓词是「这一轮没有在途任务」，不是「这一轮没有任务」：
	// 已完成的记录没清理也照样算收口，否则照后者等会永久卡死。
	if err := q.WaitRound(settledCtx(t), round); err != nil {
		t.Errorf("这一轮已经没有在途任务，应返回 nil, 实际 %v", err)
	}
	// 空轮 ID、不存在的轮 ID 都算已收口，不报错也不阻塞。
	// 再排一条不带轮 ID 的任务（它不进任何轮），空轮 ID 也不能把它认成「我要等的这一轮」。
	if _, err := q.Enqueue(NewRequest{Kind: KindDetect, ItemID: "noretry", ScopeID: "github"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", "r99"} {
		if err := q.WaitRound(settledCtx(t), id); err != nil {
			t.Errorf("轮 ID %q 不该有可等的东西, 实际 %v", id, err)
		}
	}
}

func TestWaitRoundBlocksUntilTheLastTaskOfTheRoundFinishes(t *testing.T) {
	g := newGate()
	defer g.open()
	f := &fakeExecutor{
		started: make(chan Task, 8),
		decide: func(ctx context.Context, tk Task, report Report) Outcome {
			if tk.ItemID == "b" {
				g.wait(ctx)
			}
			return Outcome{}
		},
	}
	q, _ := newEnv(f)
	round := q.BeginRound(KindApply, "github", "应用队列 · 2 项")
	mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a", ScopeID: "github", RoundID: round})
	bID := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "b", ScopeID: "github", RoundID: round}).ID
	startedTask(t, f)
	if got := waitForTask(t, q, bID, StatusRunning); got.Status != StatusRunning {
		t.Fatalf("第二条应已在执行中: %+v", got)
	}

	ch := make(chan error, 1)
	go func() { ch <- q.WaitRound(context.Background(), round) }()
	// 第一条已完成、第二条还在跑：这一轮没资格算收口。
	stillWaiting(t, ch, 80*time.Millisecond)
	g.open()
	if err := roundErr(t, ch); err != nil {
		t.Errorf("一轮跑完应返回 nil, 实际 %v", err)
	}
	drain(t, q)
}

func TestWaitRoundIsScopedToItsOwnRound(t *testing.T) {
	g := newGate()
	defer g.open()
	q, _ := newEnv(gateExecutor(g))
	roundA := q.BeginRound(KindDetect, "github", "检测 GitHub")
	roundB := q.BeginRound(KindDetect, "steam", "检测 Steam")
	if _, err := q.EnqueueSkipped(NewRequest{Kind: KindDetect, ItemID: "gh", ScopeID: "github", RoundID: roundA}, "这一轮不做"); err != nil {
		t.Fatal(err)
	}
	mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "st", ScopeID: "steam", RoundID: roundB})
	// A 轮只剩一条已跳过的记录：不该陪着 B 轮一起等。
	if err := q.WaitRound(settledCtx(t), roundA); err != nil {
		t.Errorf("A 轮已收口却返回错误: %v", err)
	}
	ch := make(chan error, 1)
	go func() { ch <- q.WaitRound(context.Background(), roundB) }()
	// B 轮那条还在途（执行中被卡住）：没跑完就不能返回。
	stillWaiting(t, ch, 80*time.Millisecond)
	g.open()
	if err := roundErr(t, ch); err != nil {
		t.Errorf("B 轮跑完应返回 nil, 实际 %v", err)
	}
}

func TestWaitRoundHonoursContextCancel(t *testing.T) {
	g := newGate()
	defer g.open()
	q, _ := newEnv(gateExecutor(g))
	round := q.BeginRound(KindDetect, "github", "检测 GitHub")
	mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh", ScopeID: "github", RoundID: round})
	startedTask(t, q.exec.(*fakeExecutor))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := q.WaitRound(ctx, round); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("ctx 到期应返回 ctx 的错误, 实际 %v", err)
	}
}

func TestWaitRoundReleasesWhenQueueStops(t *testing.T) {
	g := newGate()
	defer g.open()
	q, _ := newEnv(gateExecutor(g))
	round := q.BeginRound(KindApply, "github", "应用队列 · 2 项")
	running := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a", ScopeID: "github", RoundID: round})
	waitForTask(t, q, running.ID, StatusRunning)
	// 应用车道只有一条位置：第二条排在等待里，队列停了就永远不会再被领取。
	mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "b", ScopeID: "github", RoundID: round})
	ch := make(chan error, 1)
	go func() { ch <- q.WaitRound(context.Background(), round) }()
	stillWaiting(t, ch, 80*time.Millisecond)
	// 队列停了，这一轮还有没跑完的任务：给明确错误，不假装收口成功。
	q.Shutdown()
	if err := roundErr(t, ch); !errors.Is(err, ErrQueueStopped) {
		t.Errorf("队列停了还有没跑完的任务应返回 ErrQueueStopped, 实际 %v", err)
	}
}

func TestWaitRoundReleasesEveryWaiter(t *testing.T) {
	g := newGate()
	defer g.open()
	q, _ := newEnv(gateExecutor(g))
	round := q.BeginRound(KindDetect, "github", "检测 GitHub")
	mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh", ScopeID: "github", RoundID: round})
	startedTask(t, q.exec.(*fakeExecutor))
	// 广播用的是「关掉旧的、换一个新的」，不是发一个信号：几个等待方都得被叫醒。
	const waiters = 5
	ch := make(chan error, waiters)
	for i := 0; i < waiters; i++ {
		go func() { ch <- q.WaitRound(context.Background(), round) }()
	}
	settleAfter(80 * time.Millisecond)
	g.open()
	drain(t, q)
	for i := 0; i < waiters; i++ {
		if err := roundErr(t, ch); err != nil {
			t.Errorf("第 %d 个等待方没被叫醒: %v", i+1, err)
		}
	}
}
