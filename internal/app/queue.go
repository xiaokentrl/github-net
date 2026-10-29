package app

import (
	"context"
	"strings"

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
	return task.NewQueue(exec, event.NewTaskEmitter(emit))
}

// GetTasks 返回队列快照和 sequence 水位。它只读内存里的一把锁，没有失败面，所以不返回错误。
// 快照里的任务按入队顺序排列，tasks 为空时是空数组而不是 null。
func (s *Service) GetTasks() task.Snapshot {
	return s.tasks.Snapshot()
}

// PauseQueue 暂停领取新的等待任务，已经在跑的那条不受影响，返回暂停后的完整快照。
// 重复调用幂等：界面上按快照里的 paused 显示，而不是数自己点了几次。
func (s *Service) PauseQueue() task.Snapshot {
	s.tasks.Pause()
	return s.tasks.Snapshot()
}

// ResumeQueue 恢复领取，按原队列顺序继续，重复调用同样幂等。
func (s *Service) ResumeQueue() task.Snapshot {
	s.tasks.Resume()
	return s.tasks.Snapshot()
}

// CancelTask 取消一条排队中或执行中的任务，返回取消后的完整快照。
// 空 ID 是调用方给错了东西，跟「队列里没有这条记录」分成两个错误码。
func (s *Service) CancelTask(taskID string) (task.Snapshot, error) {
	id := strings.TrimSpace(taskID)
	if id == "" {
		return task.Snapshot{}, NewError(CodeInvalidTaskID, "任务 ID 是空的，没有要取消哪一条", nil)
	}
	if _, err := s.tasks.Cancel(id); err != nil {
		return task.Snapshot{}, ToPublicError(err)
	}
	return s.tasks.Snapshot(), nil
}

// RetryFailedTasks 逐项处理重试请求：接受的原任务在队尾新建一条重试任务，
// 不接受的项给一句原因。一次请求里两项互不影响，所以整条命令没有失败面，只回数组。
func (s *Service) RetryFailedTasks(taskIDs []string) []task.RetryResult {
	return s.tasks.RetryFailed(taskIDs)
}

// ClearFinishedTasks 移除全部终态记录，保留等待中与执行中的任务，返回清理后的快照。
// 没有终态记录时什么都不删，也算成功。
func (s *Service) ClearFinishedTasks() task.Snapshot {
	s.tasks.ClearFinished()
	return s.tasks.Snapshot()
}

// ClearQueue 移除全部记录；有任务正在执行时拒绝（queue_running），
// 否则正在跑的那条会从界面上消失，只剩一个没人认领的后台改动。
func (s *Service) ClearQueue() (task.Snapshot, error) {
	if _, err := s.tasks.ClearQueue(); err != nil {
		return task.Snapshot{}, ToPublicError(err)
	}
	return s.tasks.Snapshot(), nil
}

// ShutdownTaskQueue 在应用退出时停掉这条队列：不再领取新任务，取消正在执行的那条，
// 并等到驱动协程退出。它是包级函数而不是 Service 的方法——Service 的每个导出方法都会
// 被 Wails 绑成前端能调的命令，而停掉队列只能是进程退出的动作。
//
// 队列只在内存里，退出即消失，没有需要落盘或清理的历史文件。
func ShutdownTaskQueue(s *Service) {
	s.tasks.Shutdown()
}
