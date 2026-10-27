package task

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// 这一批测试全程不打网络、不碰 Hosts、不读系统状态：执行动作是假件。
// 要验的是队列自己的记账——顺序、状态流转、取消、暂停、重试、清理和事件序号。

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
// 被取消时故意返回失败：用来验证「取消那一刻就是终态，之后不许改写」。
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
	// 先暂停：这样事件数只由入队决定，不会被驱动协程的执行中事件插进来。
	q.Pause()

	first := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh"})
	if rec.count() != 1 {
		t.Errorf("排队该发 1 条事件, 实际 %d", rec.count())
	}
	second := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh"})
	if first.ID != second.ID {
		t.Errorf("同项目同类在途任务不该重复排队: %s vs %s", first.ID, second.ID)
	}
	if rec.count() != 1 {
		t.Errorf("重复入队不该再发事件：界面不该看到两条, 实际 %d", rec.count())
	}
	if len(q.Snapshot().Tasks) != 1 {
		t.Fatalf("队列里应该只有一条, 实际 %+v", q.Snapshot().Tasks)
	}
	other := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "gh"})
	if other.ID == first.ID {
		t.Error("应用任务和检测任务是两条不同记录，去重只挡同类型")
	}
	for _, tk := range q.Snapshot().Tasks {
		if tk.Status != StatusWaiting {
			t.Errorf("暂停期间新加入的任务应保持等待, 实际 %s", tk.Status)
		}
	}

	q.Resume()
	g.open()
	drain(t, q)

	third := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "gh"})
	if third.ID == first.ID {
		t.Error("已经结束的旧任务不该挡住新的排队")
	}
	drain(t, q)
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

func TestPauseDoesNotInterruptRunningAndResumeKeepsOrder(t *testing.T) {
	g := newGate()
	defer g.open()
	f := gateExecutor(g)
	q, _ := newEnv(f)

	first := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "a"})
	waitForTask(t, q, first.ID, StatusRunning)
	second := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "b"})

	if !q.Pause() || !q.Pause() {
		t.Error("暂停和重复暂停都要报告已暂停")
	}
	// 暂停不打断运行中的那条。
	if tk, _ := statusOf(q, first.ID); tk.Status != StatusRunning {
		t.Errorf("运行中任务不该被暂停改掉, 实际 %s", tk.Status)
	}
	g.open()
	waitForTask(t, q, first.ID, StatusDone)
	// 第一条结束后驱动退出，第二条在暂停中保持等待，不会被偷偷领走。
	settleAfter(100 * time.Millisecond)
	if tk, _ := statusOf(q, second.ID); tk.Status != StatusWaiting {
		t.Errorf("暂停期间等待任务应保持等待, 实际 %s", tk.Status)
	}
	if got := f.peakConcurrency(); got > 1 {
		t.Errorf("暂停期间不该有第二条在跑, 峰值 %d", got)
	}

	if q.Resume() || q.Resume() {
		t.Error("继续和重复继续都要报告未暂停")
	}
	snap := drain(t, q)
	if got := f.seen(); got != "a,b" {
		t.Errorf("继续后按原队列顺序执行, 实际 %s", got)
	}
	for _, tk := range snap.Tasks {
		if tk.Status != StatusDone {
			t.Errorf("排空后都该是完成, 实际 %s", tk.Status)
		}
	}
}

func TestCancelWaitingAndRunning(t *testing.T) {
	g := newGate()
	defer g.open()
	f := gateExecutor(g)
	q, _ := newEnv(f)

	running := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a"})
	waitForTask(t, q, running.ID, StatusRunning)
	waiting := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "b"})

	cancelled, err := q.Cancel(waiting.ID)
	if err != nil {
		t.Fatalf("取消等待中的任务失败: %v", err)
	}
	if cancelled.Status != StatusCancelled || cancelled.Duration != "" {
		t.Errorf("取消的等待任务应是已取消且无耗时: %+v", cancelled)
	}

	// 取消正在跑的：执行器随后返回失败也不得改写「已取消」。
	if _, err := q.Cancel(running.ID); err != nil {
		t.Fatalf("取消运行中任务失败: %v", err)
	}
	got := waitForTask(t, q, running.ID, StatusCancelled)
	settleAfter(80 * time.Millisecond)
	snap := q.Snapshot()
	if len(snap.Tasks) != 2 {
		t.Fatalf("取消不该删记录: %+v", snap.Tasks)
	}
	for _, tk := range snap.Tasks {
		if tk.Status != StatusCancelled {
			t.Errorf("%s 应停在已取消, 实际 %s", tk.ItemID, tk.Status)
		}
		if tk.ErrorMsg != "" {
			t.Errorf("取消不该带执行器的失败文案: %q", tk.ErrorMsg)
		}
	}
	if got.Duration == "" {
		t.Error("运行中被取消的任务应有耗时")
	}

	if _, err := q.Cancel(running.ID); !errors.Is(err, ErrTaskFinished) {
		t.Errorf("取消终态任务应报已结束, 实际 %v", err)
	}
	if _, err := q.Cancel("t999"); !errors.Is(err, ErrTaskNotFound) {
		t.Errorf("取消不存在的任务应报没找到, 实际 %v", err)
	}
	// 已取消不算在途，同项目可以重新排队。
	again := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "b"})
	if again.ID == waiting.ID {
		t.Error("取消后应能重新排队")
	}
	g.open()
	drain(t, q)
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

func TestRetryFailedRules(t *testing.T) {
	byItem := map[string]Outcome{
		"a": {Status: StatusFailed, Message: "候选失效"},
		"b": {Status: StatusRolledback, Message: "写入失败已退回"},
		"c": {},
	}
	f := &fakeExecutor{started: make(chan Task, 8), decide: func(ctx context.Context, tk Task, report Report) Outcome {
		return byItem[tk.ItemID]
	}}
	q, _ := newEnv(f)

	failID := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a", ScopeID: "github", StrategyID: "dns-hosts"}).ID
	rollID := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "b", ScopeID: "github", StrategyID: "dns-hosts"}).ID
	okID := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "c", ScopeID: "github", StrategyID: "dns-hosts"}).ID
	detectID := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "a"}).ID
	drain(t, q)

	got := q.RetryFailed([]string{failID, rollID, okID, detectID, "t999", "  "})
	if len(got) != 6 {
		t.Fatalf("逐项结果数应为 6, 实际 %d", len(got))
	}
	bySource := map[string]RetryResult{}
	for _, r := range got {
		bySource[r.SourceTaskID] = r
	}
	if bySource[failID].TaskID == "" || bySource[failID].Reason != "" {
		t.Errorf("失败的应用任务应能重试, 实际 %+v", bySource[failID])
	}
	if bySource[rollID].TaskID == "" {
		t.Errorf("已回滚的应用任务应能重试, 实际 %+v", bySource[rollID])
	}
	for _, id := range []string{okID, detectID, "t999", ""} {
		r := bySource[id]
		if r.TaskID != "" || r.Reason == "" {
			t.Errorf("%q 不该被接受重试, 应给出原因: %+v", id, r)
		}
	}

	// 新任务继承范围和方法，用 retryOf 指回原任务；原记录保持终态不动。
	snap := q.Snapshot()
	retries := map[string]Task{}
	originals := map[string]Task{}
	for _, tk := range snap.Tasks {
		if tk.RetryOf != "" {
			retries[tk.RetryOf] = tk
		}
		originals[tk.ID] = tk
	}
	for _, id := range []string{failID, rollID} {
		r, ok := retries[id]
		if !ok {
			t.Fatalf("%s 应该有对应的重试任务", id)
		}
		if r.Kind != KindApply || r.ScopeID != "github" || r.StrategyID != "dns-hosts" || r.ItemID != originals[id].ItemID {
			t.Errorf("重试任务要继承原任务的项目、范围和方法: %+v", r)
		}
	}
	if originals[failID].Status != StatusFailed || originals[rollID].Status != StatusRolledback {
		t.Error("原终态记录不该被回退或改写")
	}

	drain(t, q)
}

func TestRetryRefusedWhenSameItemActive(t *testing.T) {
	g := newGate()
	defer g.open()
	var runs int32
	f := &fakeExecutor{started: make(chan Task, 4), decide: func(ctx context.Context, tk Task, report Report) Outcome {
		if atomic.AddInt32(&runs, 1) == 1 {
			return Outcome{Status: StatusFailed, Message: "候选失效"}
		}
		if g.wait(ctx) {
			return Outcome{}
		}
		return Outcome{Status: StatusFailed, Message: "执行器被取消"}
	}}
	q, _ := newEnv(f)
	failID := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a", ScopeID: "github", StrategyID: "dns-hosts"}).ID
	drain(t, q)

	// 同项目同类型又排了一条在途任务，此时重试旧失败记录要被逐项拒绝。
	active := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a", ScopeID: "github", StrategyID: "dns-hosts"}).ID
	waitForTask(t, q, active, StatusRunning)
	results := q.RetryFailed([]string{failID})
	if len(results) != 1 || results[0].TaskID != "" || results[0].Reason == "" {
		t.Errorf("已有在途任务时重试应被拒并给出原因: %+v", results)
	}
	if got := q.Snapshot().Tasks; len(got) != 2 {
		t.Errorf("被拒的重试不该多出记录: %+v", got)
	}
	g.open()
	drain(t, q)
}

func TestRetryAfterShutdownRefused(t *testing.T) {
	q, _ := newEnv(successExecutor())
	tk := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a"})
	drain(t, q)
	q.Shutdown()
	for _, r := range q.RetryFailed([]string{tk.ID}) {
		if r.Reason == "" {
			t.Errorf("队列停止后的重试应被拒并说明原因: %+v", r)
		}
	}
	if _, err := q.Enqueue(NewRequest{Kind: KindDetect, ItemID: "b"}); !errors.Is(err, ErrQueueStopped) {
		t.Errorf("停止后入队应报队列已停止, 实际 %v", err)
	}
}

func TestClearFinishedKeepsActiveAndIsIdempotent(t *testing.T) {
	g := newGate()
	defer g.open()
	q, _ := newEnv(gateExecutor(g))

	running := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "a"})
	waiting := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "b"})
	waitForTask(t, q, running.ID, StatusRunning)
	if _, err := q.Cancel(waiting.ID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	// 一条执行中、一条已取消：清理已结束只该带走已取消那条。
	if n := q.ClearFinished(); n != 1 {
		t.Errorf("应移除 1 条终态, 实际 %d", n)
	}
	snap := q.Snapshot()
	if len(snap.Tasks) != 1 || snap.Tasks[0].ID != running.ID {
		t.Errorf("执行中的必须留下: %+v", snap.Tasks)
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

func TestClearQueueRefusesWhileRunning(t *testing.T) {
	g := newGate()
	defer g.open()
	q, _ := newEnv(gateExecutor(g))

	running := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "a"})
	mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "b"})
	waitForTask(t, q, running.ID, StatusRunning)

	if _, err := q.ClearQueue(); !errors.Is(err, ErrQueueRunning) {
		t.Errorf("有任务在跑时清空应被拒, 实际 %v", err)
	}
	if len(q.Snapshot().Tasks) != 2 {
		t.Error("被拒的清空不该动任何记录")
	}
	g.open()
	drain(t, q)
	n, err := q.ClearQueue()
	if err != nil {
		t.Fatalf("没有运行中任务时清空应成功: %v", err)
	}
	if n != 2 || len(q.Snapshot().Tasks) != 0 {
		t.Errorf("应清掉 2 条并留空队列, 实际 %d / %+v", n, q.Snapshot().Tasks)
	}
	if _, err := q.ClearQueue(); err != nil {
		t.Errorf("空队列再清空应幂等成功, 实际 %v", err)
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

func TestReportIgnoredAfterCancel(t *testing.T) {
	reported := make(chan struct{})
	f := &fakeExecutor{
		started: make(chan Task, 4),
		decide: func(ctx context.Context, tk Task, report Report) Outcome {
			report("backup", "备份 hosts…", 30)
			<-ctx.Done()
			// 取消之后迟到的汇报必须被丢掉。
			report("write", "写入域名映射…", 80)
			close(reported)
			return Outcome{Status: StatusFailed, Message: "被取消"}
		},
	}
	q, _ := newEnv(f)
	tk := mustEnqueue(t, q, NewRequest{Kind: KindApply, ItemID: "a"})
	waitForTask(t, q, tk.ID, StatusRunning)
	if _, err := q.Cancel(tk.ID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	select {
	case <-reported:
	case <-time.After(2 * time.Second):
		t.Fatal("执行器没跑到收尾")
	}
	got := q.Snapshot().Tasks[0]
	if got.Status != StatusCancelled || got.PhaseText != "备份 hosts…" || got.Progress != 30 {
		t.Errorf("取消后不该被迟到的汇报改写: %+v", got)
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
	// 前端按钮的启用条件依赖这两个集合，必须和规划里的六档终态一致。
	for _, s := range []Status{StatusDone, StatusSkipped, StatusFailed, StatusRolledback, StatusCancelled, StatusRestored} {
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
	if len(empty.Tasks) != 0 || empty.Sequence != 0 || empty.Paused {
		t.Errorf("新队列应是空且未暂停、水位 0: %+v", empty)
	}

	g := newGate()
	defer g.open()
	q.Pause()
	tk := mustEnqueue(t, q, NewRequest{Kind: KindDetect, ItemID: "a", Name: "GitHub 主站"})
	snap := q.Snapshot()
	if len(snap.Tasks) != 1 {
		t.Fatalf("快照应有 1 条: %+v", snap.Tasks)
	}
	snap.Tasks[0].Status = StatusFailed
	snap.Tasks[0].Name = "被外面改了"
	fresh := q.Snapshot()
	if fresh.Tasks[0].Name != "GitHub 主站" || fresh.Tasks[0].Status != StatusWaiting {
		t.Errorf("快照是副本，外面改不动队列里的记录: %+v", fresh.Tasks[0])
	}
	if tk.Status != StatusWaiting {
		t.Errorf("入队返回的也应是等待状态的副本: %+v", tk)
	}
}
