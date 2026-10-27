package event

import "testing"

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
