package app

import (
	"context"

	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/task"
)

// pendingExecutorMessage 是「还没接执行器」那句拒绝。写在这里而不是散落各处：
// 分发执行器对没接上的类型也说同一句话，界面文案保持一致。
const pendingExecutorMessage = "这一类任务还没有接上执行动作，这条任务没有开始"

// pendingExecutor 是「执行动作还没接上」这件事的可见答案。
//
// 检测执行器（detectExecutor）在目录和设置就绪时由装配层换进队列；应用和恢复
// 要写系统 Hosts，属阶段四的受限辅助服务，它们排进来的任务会以「失败」落到队列里被看见，
// 而不是让驱动协程撞上 nil 把整个应用带崩。目录或设置读不出来时检测也走这条拒答。
type pendingExecutor struct{}

func (pendingExecutor) Execute(_ context.Context, _ task.Task, _ task.Report) task.Outcome {
	return task.Outcome{Status: task.StatusFailed, Message: "这一类任务还没有接上执行动作，这条任务没有开始"}
}

// newTaskQueue 建服务内部那一条内存队列。执行动作由装配层决定：
// 目录和设置都就绪时是真实检测执行器，否则是上面的拒答。
// 进度事件经 emit 投出去（emit 给 nil 就是不发事件，测试和还没接总线的阶段走这条路）。
//
// 队列藏在 Service 里、不放在装配层，代价是退出这件事不能写成 Service 的方法：
// Wails 会把 Service 的每个导出方法都当成命令绑给前端，而「停掉队列」不该是前端能点的东西。
// 所以停止走下面那个包级函数，由 main.go 在退出时调用。
func newTaskQueue(exec task.Executor, emit func(name string, payload any)) *task.Queue {
	return newTaskQueueLaned(exec, emit, 1)
}

// newTaskQueueLaned 同 newTaskQueue，另指定检测任务的并行上限（应用/恢复恒串行）。
func newTaskQueueLaned(exec task.Executor, emit func(name string, payload any), detectLane int) *task.Queue {
	return task.NewQueueLaned(exec, event.NewTaskEmitter(emit), detectLane)
}

// GetTasks 返回队列快照和 sequence 水位。它只读内存里的一把锁，没有失败面，所以不返回错误。
// 快照里的任务按入队顺序排列，tasks 为空时是空数组而不是 null。
func (s *Service) GetTasks() task.Snapshot {
	return s.tasks.Snapshot()
}

// ClearFinishedTasks 移除全部终态记录，保留等待中与执行中的任务，返回清理后的快照。
// 没有终态记录时什么都不删，也算成功。
func (s *Service) ClearFinishedTasks() task.Snapshot {
	s.tasks.ClearFinished()
	return s.tasks.Snapshot()
}

// ShutdownTaskQueue 在应用退出时停掉这条队列：不再领取新任务，取消正在执行的那条，
// 并等到驱动协程退出。它是包级函数而不是 Service 的方法——Service 的每个导出方法都会
// 被 Wails 绑成前端能调的命令，而停掉队列只能是进程退出的动作。
//
// 队列只在内存里，退出即消失，没有需要落盘或清理的历史文件。
func ShutdownTaskQueue(s *Service) {
	s.tasks.Shutdown()
}
