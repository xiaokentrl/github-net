package task

import "errors"

// 队列层的哨兵错误：上层按这些码判断是哪种拒绝，不解析中文文案。
var (
	// ErrInvalidRequest 表示排队请求本身不合格（类型不认识、项目 ID 为空）。
	ErrInvalidRequest = errors.New("任务请求不合法")
	// ErrQueueStopped 表示队列已经随应用退出停止，不再接受新任务。
	ErrQueueStopped = errors.New("任务队列已停止")
)
