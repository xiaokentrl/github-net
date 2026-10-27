package event

import (
	"github.com/xiaokentrl/github-net/internal/task"
)

// TaskProgressName 是任务进度事件的名字。全仓库只有这一处写它：
// 队列只认 Publisher 接口，不知道事件叫什么；前端订阅用的必须是同一个字符串。
const TaskProgressName = "task:progress"

// TaskEmitter 是 internal/task 需要的发布器实现，只负责两件事：
// 决定事件名，把进度交给外面传进来的投递函数。
// 事件内容直接复用 task.Progress——它和 GetTasks 快照里的任务字段是同一份 DTO，
// 这里再抄一份结构就会变成两边各改各的。
type TaskEmitter struct {
	emit func(name string, payload any)
}

// NewTaskEmitter 包一个投递函数（装配层里通常是 Wails 的事件总线）。
// emit 为 nil 时后续投递静默丢弃，不会 panic：队列没接到总线也要能跑。
func NewTaskEmitter(emit func(name string, payload any)) *TaskEmitter {
	return &TaskEmitter{emit: emit}
}

// PublishProgress 实现 task.Publisher。
func (e *TaskEmitter) PublishProgress(p task.Progress) {
	if e == nil || e.emit == nil {
		return
	}
	e.emit(TaskProgressName, p)
}

var _ task.Publisher = (*TaskEmitter)(nil)
