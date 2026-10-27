package event

import (
	"sync/atomic"
	"time"
)

// RuntimeStatusName 是纵向样例唯一的自定义事件名。
const RuntimeStatusName = "app:runtime-status"

// Status 是运行时状态事件的内容；Sequence 单调递增，前端据此丢弃重复和过期事件。
type Status struct {
	Sequence      int64  `json:"sequence"`
	State         string `json:"state"`
	WindowVisible bool   `json:"windowVisible"`
	EmittedAt     string `json:"emittedAt"`
}

// StatusEmitter 独占事件序号。它不注册为 Wails 服务，所以前端无法直接推动序号。
type StatusEmitter struct {
	sequence atomic.Int64
}

func (e *StatusEmitter) Next(windowVisible bool) Status {
	return Status{
		Sequence:      e.sequence.Add(1),
		State:         "running",
		WindowVisible: windowVisible,
		EmittedAt:     time.Now().UTC().Format(time.RFC3339),
	}
}

// Sequence 返回已发出的最后一个序号；未发出时为 0。
func (e *StatusEmitter) Sequence() int64 {
	return e.sequence.Load()
}
