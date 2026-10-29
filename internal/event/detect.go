package event

import (
	"github.com/xiaokentrl/github-net/internal/accelerator"
)

// DetectProgressName 是候选检测进度事件的名字。全仓库只有这一处写它，
// 前端订阅用的必须是同一个字符串。和任务事件分开：任务事件说「这一条任务整体到哪了」，
// 这个事件说「这一轮里每条候选此刻的样子」，粒度和消费方都不同。
const DetectProgressName = "detect:progress"

// DetectEmitter 是候选级进度事件的发布器。事件内容直接复用 accelerator.Progress——
// 它本来就有完整 JSON 标签（候选 ID、域名、状态、序号、计数），再抄一份结构只会两边各改各的。
type DetectEmitter struct {
	emit func(name string, payload any)
}

// NewDetectEmitter 包一个投递函数（装配层里通常是 Wails 的事件总线）。
// emit 为 nil 时投递静默丢弃：队列和检测没接到总线也要能跑。
func NewDetectEmitter(emit func(name string, payload any)) *DetectEmitter {
	return &DetectEmitter{emit: emit}
}

// Publish 转发一条候选级进度事件。
func (e *DetectEmitter) Publish(p accelerator.Progress) {
	if e == nil || e.emit == nil {
		return
	}
	e.emit(DetectProgressName, p)
}
