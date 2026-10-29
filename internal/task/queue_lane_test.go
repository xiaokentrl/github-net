package task

import (
	"context"
	"sync"
	"testing"
	"time"
)

// blockingExecutor 让每条任务卡在放行信号上，用来观察「几条同时在跑」。
type blockingExecutor struct {
	mu       sync.Mutex
	running  int
	maxSeen  int
	started  chan struct{}
	release  chan struct{}
	failNext bool
}

func newBlocking() *blockingExecutor {
	return &blockingExecutor{started: make(chan struct{}, 16), release: make(chan struct{})}
}

func (b *blockingExecutor) Execute(_ context.Context, t Task, _ Report) Outcome {
	b.mu.Lock()
	b.running++
	if b.running > b.maxSeen {
		b.maxSeen = b.running
	}
	b.mu.Unlock()
	b.started <- struct{}{}
	<-b.release
	b.mu.Lock()
	b.running--
	b.mu.Unlock()
	if b.failNext {
		return Outcome{Status: StatusFailed, Message: "blocked run failed"}
	}
	return Outcome{Status: StatusDone}
}

func waitSignal(t *testing.T, ch chan struct{}, n int, desc string) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("等了 2 秒，%s 只到 %d/%d", desc, i, n)
		}
	}
}

func TestDetectLaneRunsTasksConcurrently(t *testing.T) {
	exec := newBlocking()
	q := NewQueueLaned(exec, nil, 2)

	q.Enqueue(NewRequest{Kind: KindDetect, ItemID: "a", Name: "a"})
	q.Enqueue(NewRequest{Kind: KindDetect, ItemID: "b", Name: "b"})
	// 两条检测任务都该同时进入执行中（车道上限 2）。
	waitSignal(t, exec.started, 2, "并行检测启动")

	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := q.Snapshot()
		running := 0
		for _, task := range snap.Tasks {
			if task.Status == StatusRunning {
				running++
			}
		}
		if running == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("两条检测该同时在跑，实际 %+v", snap.Tasks)
		}
		time.Sleep(5 * time.Millisecond)
	}

	exec.mu.Lock()
	maxSeen := exec.maxSeen
	exec.mu.Unlock()
	if maxSeen < 2 {
		t.Fatalf("执行器该见过 2 条并发，实际 %d", maxSeen)
	}

	close(exec.release)
	waitSettled(t, q, 2)
}

func TestApplyLaneStaysSerial(t *testing.T) {
	exec := newBlocking()
	q := NewQueueLaned(exec, nil, 8) // 检测车道放开，应用车道仍应串行

	q.Enqueue(NewRequest{Kind: KindApply, ItemID: "a", Name: "a"})
	q.Enqueue(NewRequest{Kind: KindApply, ItemID: "b", Name: "b"})
	waitSignal(t, exec.started, 1, "第一条应用启动")

	// 给第二条一点时间：它不该在第一条放行前启动。
	time.Sleep(80 * time.Millisecond)
	snap := q.Snapshot()
	if snap.Tasks[1].Status != StatusWaiting {
		t.Fatalf("第二条应用该还在等（应用车道串行），实际 %s", snap.Tasks[1].Status)
	}

	close(exec.release)
	waitSettled(t, q, 2)
	if exec.maxSeen > 1 {
		t.Fatalf("应用任务绝不能并行，见过 %d 条并发", exec.maxSeen)
	}
}

func TestDetectDoesNotBlockApplyLane(t *testing.T) {
	exec := newBlocking()
	q := NewQueueLaned(exec, nil, 1)

	q.Enqueue(NewRequest{Kind: KindDetect, ItemID: "d1", Name: "d1"})
	q.Enqueue(NewRequest{Kind: KindApply, ItemID: "a1", Name: "a1"})
	waitSignal(t, exec.started, 1, "检测先启动")

	// 检测车道只有 1 且被占着，但应用车道是空的：应用该照常启动，不被前面的检测挡住。
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := q.Snapshot()
		if snap.Tasks[1].Status == StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("应用该和检测并行，实际 %+v", snap.Tasks)
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(exec.release)
	waitSettled(t, q, 2)
}

func TestClearQueueRefusesWhileDetectRunning(t *testing.T) {
	exec := newBlocking()
	q := NewQueueLaned(exec, nil, 2)
	q.Enqueue(NewRequest{Kind: KindDetect, ItemID: "a", Name: "a"})
	waitSignal(t, exec.started, 1, "检测启动")

	if _, err := q.ClearQueue(); err == nil {
		t.Fatal("有检测在跑时清空队列该被拒")
	}
	close(exec.release)
	waitSettled(t, q, 1)
	if _, err := q.ClearQueue(); err != nil {
		t.Fatalf("跑完后清空该成功: %v", err)
	}
}

func waitSettled(t *testing.T, q *Queue, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := q.Snapshot()
		done := 0
		for _, task := range snap.Tasks {
			if IsTerminal(task.Status) {
				done++
			}
		}
		if done == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 2 秒，终态任务只有 %d/%d", done, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
