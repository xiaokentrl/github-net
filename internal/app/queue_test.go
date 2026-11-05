package app

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/task"
)

// gatedExecutor 只用来造出测试需要的状态：项目 ID 是 "hold" 的任务卡在放行信号上，
// 其余项目按预设结果立刻收口（预设留空就是零值，队列按「该类任务成功」处理）。
type gatedExecutor struct {
	hold     chan struct{}
	started  chan struct{}
	outcomes map[string]task.Outcome
}

func (e *gatedExecutor) Execute(ctx context.Context, t task.Task, _ task.Report) task.Outcome {
	if t.ItemID == "hold" {
		select {
		case e.started <- struct{}{}:
		default:
		}
		select {
		case <-e.hold:
		case <-ctx.Done():
		}
		return task.Outcome{}
	}
	return e.outcomes[t.ItemID]
}

func newGatedExecutor() *gatedExecutor {
	return &gatedExecutor{
		hold:     make(chan struct{}),
		started:  make(chan struct{}, 1),
		outcomes: map[string]task.Outcome{},
	}
}

// queueService 直接装配一条注入了指定执行器的队列，跳过 NewService 的目录与配置加载。
// 命令层只依赖 tasks 这一个字段，所以这里不填其余字段。
func queueService(exec task.Executor) *Service {
	return &Service{tasks: task.NewQueue(exec, nil)}
}

func waitFor(t *testing.T, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("等了 2 秒，%s 仍未发生", desc)
}

func findTask(snap task.Snapshot, id string) (task.Task, bool) {
	for _, item := range snap.Tasks {
		if item.ID == id {
			return item, true
		}
	}
	return task.Task{}, false
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	var appErr *Error
	if !errors.As(err, &appErr) {
		t.Fatalf("错误没有包装成 *Error: %v", err)
	}
	return appErr.Code
}

func TestGetTasksOnEmptyQueue(t *testing.T) {
	svc := NewService("github-net", "test", nil, nil)

	snap := svc.GetTasks()
	if len(snap.Tasks) != 0 || snap.Sequence != 0 {
		t.Fatalf("新服务应是空队列、水位 0，实际 %+v", snap)
	}
	// 空队列必须是空数组而不是 null：前端直接 map，不该先判空。
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("快照无法序列化: %v", err)
	}
	if want := `{"tasks":[],"rounds":[],"sequence":0}`; string(raw) != want {
		t.Errorf("空队列的 JSON 形状不对：want %s got %s", want, raw)
	}
}

func TestClearFinishedTasksKeepsActiveOnes(t *testing.T) {
	exec := newGatedExecutor()
	svc := queueService(exec)

	done, _ := svc.tasks.Enqueue(task.NewRequest{Kind: task.KindDetect, ItemID: "ok"})
	waitFor(t, "第一条任务收口", func() bool {
		item, ok := findTask(svc.GetTasks(), done.ID)
		return ok && item.Status == task.StatusDone
	})
	waiting, _ := svc.tasks.Enqueue(task.NewRequest{Kind: task.KindDetect, ItemID: "hold"})
	<-exec.started

	snap := svc.ClearFinishedTasks()
	if _, ok := findTask(snap, done.ID); ok {
		t.Errorf("已结束的任务该被清掉，快照 %+v", snap.Tasks)
	}
	if _, ok := findTask(snap, waiting.ID); !ok {
		t.Errorf("还在跑的任务不该被清掉，快照 %+v", snap.Tasks)
	}

	close(exec.hold)
	waitFor(t, "剩下的任务收口", func() bool {
		item, ok := findTask(svc.GetTasks(), waiting.ID)
		return ok && task.IsTerminal(item.Status)
	})
	if snap := svc.ClearFinishedTasks(); len(snap.Tasks) != 0 {
		t.Errorf("收口之后该被清掉，快照 %+v", snap.Tasks)
	}
	// 水位不回退：前端拿它判断事件是否已经包含在快照里，清理不该把它归零。
	if seq := svc.GetTasks().Sequence; seq == 0 {
		t.Errorf("清理已结束不该把事件水位归零，实际 %d", seq)
	}
}

func TestQueueCommandsPublishProgressEvents(t *testing.T) {
	type captured struct {
		name    string
		payload any
	}
	var mu sync.Mutex
	var capturedEvents []captured
	emit := func(name string, payload any) {
		mu.Lock()
		defer mu.Unlock()
		capturedEvents = append(capturedEvents, captured{name: name, payload: payload})
	}
	svc := NewService("github-net", "test", nil, emit)

	if _, err := svc.tasks.Enqueue(task.NewRequest{Kind: task.KindDetect, ItemID: "github.com"}); err != nil {
		t.Fatalf("排队失败: %v", err)
	}
	waitFor(t, "任务落到终态", func() bool {
		item, ok := findTask(svc.GetTasks(), "t1")
		return ok && task.IsTerminal(item.Status)
	})

	mu.Lock()
	// 先把捕获到的事件抄出来再松锁：投递发生在队列锁里，
	// 拿着这把锁再回头读快照会让两边互相等。
	emitted := append([]captured(nil), capturedEvents...)
	mu.Unlock()
	// 等待、执行中、终态各一条：状态一变就发，不攒批。
	if len(emitted) != 3 {
		t.Fatalf("进度事件应有 3 条，实际 %d 条：%+v", len(emitted), emitted)
	}
	wantStatuses := []task.Status{task.StatusWaiting, task.StatusRunning, task.StatusFailed}
	var lastSequence int64
	for i, ev := range emitted {
		if ev.name != event.TaskProgressName {
			t.Errorf("第 %d 条事件名不对，期望 %q 实际 %q", i, event.TaskProgressName, ev.name)
		}
		progress, ok := ev.payload.(task.Progress)
		if !ok {
			t.Fatalf("第 %d 条事件载荷不是 task.Progress，实际 %T", i, ev.payload)
		}
		if want := int64(i + 1); progress.Sequence != want {
			t.Errorf("第 %d 条事件序号应为 %d，实际 %d", i, want, progress.Sequence)
		}
		if progress.Task.Status != wantStatuses[i] {
			t.Errorf("第 %d 条事件的状态应为 %s，实际 %s", i, wantStatuses[i], progress.Task.Status)
		}
		lastSequence = progress.Sequence
	}
	// 事件水位和快照水位是同一个计数器：两边对不上，前端就会漏应用或重复应用。
	if snap := svc.GetTasks(); snap.Sequence != lastSequence {
		t.Errorf("快照水位 %d 与最后一条事件水位 %d 不一致", snap.Sequence, lastSequence)
	}
}

func TestPendingExecutorFailsQueuedTaskInsteadOfCrashing(t *testing.T) {
	// 队列入队会拦下未知类型，nil 执行器的风险只剩一种：某个 Kind 的执行器没接。
	// 这里直接打分发执行器的 nil 分支：万一以后有人在分发台挂了个没接活的类型，
	// 任务会以「失败」被界面看见，而不是让驱动协程撞上 nil 把整个应用带崩。
	dispatch := newDispatchExecutor(nil, nil, newDetectResultStore(), nil, nil)
	out := dispatch.Execute(context.Background(), task.Task{ID: "t1", Kind: task.KindDetect},
		func(string, string, int) {})
	if out.Status != task.StatusFailed || out.Message != pendingExecutorMessage {
		t.Fatalf("没接执行器的类型该当场判失败并给出原因，实际 %+v", out)
	}
	out = dispatch.Execute(context.Background(), task.Task{ID: "t2", Kind: task.KindApply},
		func(string, string, int) {})
	if out.Status != task.StatusFailed || out.Message != pendingExecutorMessage {
		t.Fatalf("没接特权客户端的应用任务该当场判失败，实际 %+v", out)
	}
}

func TestShutdownTaskQueueStopsDriver(t *testing.T) {
	exec := newGatedExecutor()
	svc := queueService(exec)

	svc.tasks.Enqueue(task.NewRequest{Kind: task.KindDetect, ItemID: "hold"})
	<-exec.started
	waitFor(t, "任务进入执行中", func() bool {
		item, ok := findTask(svc.GetTasks(), "t1")
		return ok && item.Status == task.StatusRunning
	})

	done := make(chan struct{})
	go func() {
		ShutdownTaskQueue(svc)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("退出时停队列被卡住：应用关不掉")
	}
	item, ok := findTask(svc.GetTasks(), "t1")
	if !ok || !task.IsTerminal(item.Status) {
		t.Errorf("退出后正在执行的任务应落到终态，实际 %+v", item)
	}
	if _, err := svc.tasks.Enqueue(task.NewRequest{Kind: task.KindDetect, ItemID: "after"}); !errors.Is(err, task.ErrQueueStopped) {
		t.Errorf("队列停止后不该再接受任务，实际 %v", err)
	}
}

// TestTwoQueueCommandsAreBound 是形状门：队列命令只剩这两条，签名必须对得上，
// 而「停掉队列」「暂停」「取消」「重试」「清空整队」必须不是 Service 的方法——
// Wails 会把每个导出方法都绑成前端能调的命令，界面不要的东西就不该有这条路。
func TestTwoQueueCommandsAreBound(t *testing.T) {
	svcType := reflect.TypeOf(&Service{})
	want := []struct {
		name   string
		in     []reflect.Type
		out    []reflect.Type
		reason string
	}{
		{"GetTasks", nil, []reflect.Type{reflect.TypeOf(task.Snapshot{})}, "前端取快照"},
		{"ClearFinishedTasks", nil, []reflect.Type{reflect.TypeOf(task.Snapshot{})}, "清理已结束"},
	}
	for _, item := range want {
		method, ok := svcType.MethodByName(item.name)
		if !ok {
			t.Errorf("%s 没有绑给前端（少了这条命令：%s）", item.name, item.reason)
			continue
		}
		sig := method.Type
		// 方法值的第一个入参是接收者，所以期望参数表要加上它。
		if sig.NumIn() != len(item.in)+1 || sig.NumOut() != len(item.out) {
			t.Errorf("%s 签名形状不对：入参 %d 出参 %d，期望 %d/%d", item.name, sig.NumIn(), sig.NumOut(), len(item.in)+1, len(item.out))
			continue
		}
		// 方法值第一项是接收者，参数从下标 1 开始。
		for i, wantIn := range item.in {
			if got := sig.In(i + 1); got != wantIn {
				t.Errorf("%s 第 %d 个参数类型应为 %s，实际 %s", item.name, i, wantIn, got)
			}
		}
		for i, wantOut := range item.out {
			if got := sig.Out(i); got != wantOut {
				t.Errorf("%s 第 %d 个返回值类型应为 %s，实际 %s", item.name, i, wantOut, got)
			}
		}
	}

	for _, forbidden := range []string{
		"Shutdown", "ShutdownTaskQueue", "Enqueue", "PruneCaches",
		"PauseQueue", "ResumeQueue", "CancelTask", "RetryFailedTasks", "ClearQueue",
	} {
		if _, ok := svcType.MethodByName(forbidden); ok {
			t.Errorf("Service 上不该有 %s 这个方法：前端能调到它就越过边界", forbidden)
		}
	}
}
