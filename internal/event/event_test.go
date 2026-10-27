package event

import (
	"testing"

	"github.com/xiaokentrl/github-net/internal/task"
)

func TestStatusEmitterSequenceMonotonic(t *testing.T) {
	emitter := &StatusEmitter{}

	if emitter.Sequence() != 0 {
		t.Fatalf("初始序号应为 0，实际 %d", emitter.Sequence())
	}

	for i := int64(1); i <= 5; i++ {
		status := emitter.Next(i%2 == 0)
		if status.Sequence != i {
			t.Fatalf("第 %d 次序号应为 %d，实际 %d", i, i, status.Sequence)
		}
		if status.State != "running" || status.EmittedAt == "" {
			t.Errorf("状态内容不完整: %+v", status)
		}
		if status.WindowVisible != (i%2 == 0) {
			t.Errorf("窗口可见性应与入参一致: %+v", status)
		}
	}

	if emitter.Sequence() != 5 {
		t.Errorf("已发出序号应为 5，实际 %d", emitter.Sequence())
	}
}

// 任务进度事件的契约：名字只有 TaskProgressName 这一处，内容是队列那份任务 DTO 的原件，
// 事件层不再抄一个副本结构。
func TestTaskEmitterUsesSingleNameAndSharedDTO(t *testing.T) {
	type got struct {
		name    string
		payload any
	}
	var seen []got
	emitter := NewTaskEmitter(func(name string, payload any) {
		seen = append(seen, got{name: name, payload: payload})
	})

	p := task.Progress{
		Sequence: 7,
		Task:     task.Task{ID: "t1", Kind: task.KindApply, ItemID: "gh", Status: task.StatusRunning, Progress: 40},
	}
	emitter.PublishProgress(p)

	if len(seen) != 1 {
		t.Fatalf("应投递 1 次, 实际 %d", len(seen))
	}
	if seen[0].name != TaskProgressName {
		t.Errorf("事件名应为 %q, 实际 %q", TaskProgressName, seen[0].name)
	}
	out, ok := seen[0].payload.(task.Progress)
	if !ok {
		t.Fatalf("投递的内容应就是 task.Progress，不是一层新结构: %T", seen[0].payload)
	}
	if out.Sequence != 7 || out.Task.ID != "t1" || out.Task.Status != task.StatusRunning || out.Task.Progress != 40 {
		t.Errorf("内容原样透传，序号和任务字段都不该丢: %+v", out)
	}
}

func TestTaskEmitterToleratesMissingBusAndNil(t *testing.T) {
	// 装配层还没接上总线时，队列发事件不该 panic。
	NewTaskEmitter(nil).PublishProgress(task.Progress{Sequence: 1, Task: task.Task{ID: "t1"}})
	var emitter *TaskEmitter
	emitter.PublishProgress(task.Progress{Sequence: 1})
}
