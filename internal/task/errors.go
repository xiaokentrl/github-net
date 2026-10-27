package task

import "errors"

// 队列层的哨兵错误：上层按这些码判断是哪种拒绝，不解析中文文案。
var (
	// ErrInvalidRequest 表示排队请求本身不合格（类型不认识、项目 ID 为空）。
	ErrInvalidRequest = errors.New("任务请求不合法")
	// ErrTaskNotFound 表示队列里没有这条任务记录（可能已被清理，或 ID 是编的）。
	ErrTaskNotFound = errors.New("队列里没有这个任务")
	// ErrTaskFinished 表示任务已到终态，不能再取消。终态不回退是硬规则。
	ErrTaskFinished = errors.New("任务已经结束，不能取消")
	// ErrQueueRunning 表示有任务正在执行，此时拒绝清空整个队列。
	// 否则正在跑的那条会从界面上消失，只剩一个没人认领的后台改动。
	ErrQueueRunning = errors.New("有任务正在执行，队列不能清空")
	// ErrQueueStopped 表示队列已经随应用退出停止，不再接受新任务。
	ErrQueueStopped = errors.New("任务队列已停止")
)
