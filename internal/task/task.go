// Package task 是内存任务队列：任务生命周期、状态流转、取消、暂停、重试和清理。
// 队列不落地——应用每次启动都是空队列，不读也不写队列历史文件。
//
// 这里只有「排队和记账」，不碰真实世界：真正做事的执行动作由注入的 Executor 完成，
// 本包不认识 DNS、Hosts 和系统命令；进度通过注入的 Publisher 发出去，
// 事件叫什么名字、投递到哪个总线由 internal/event 决定，本包不重复定义事件名。
package task

import (
	"context"
	"fmt"
)

// Kind 是任务类型，和前端原型队列里的三类标签一一对应。
type Kind string

const (
	KindDetect  Kind = "detect"  // 检测：找候选地址
	KindApply   Kind = "apply"   // 应用：把候选写进 Hosts
	KindRestore Kind = "restore" // 恢复：写回应用前的映射
)

func (k Kind) valid() bool {
	return k == KindDetect || k == KindApply || k == KindRestore
}

// Status 是任务状态，九档和原型 TaskMeta 逐一对应，前端按这个字符串显示。
type Status string

const (
	StatusWaiting    Status = "waiting"    // 等待：排队中，还没开始
	StatusRunning    Status = "running"    // 执行中：检测或应用正在跑
	StatusDone       Status = "done"       // 完成
	StatusFailed     Status = "failed"     // 失败
	StatusSkipped    Status = "skipped"    // 跳过：这次没必要做或条件不满足
	StatusRolledback Status = "rolledback" // 已回滚：做失败了，且已退回改动前的状态
	StatusCancelled  Status = "cancelled"  // 已取消
	StatusRestoring  Status = "restoring"  // 恢复中：恢复类任务的执行中状态
	StatusRestored   Status = "restored"   // 已恢复
)

// 终态是这六档：进了这一组就不会再回到等待或执行中。
// 「清理已结束」的按钮启用条件和后端的判定必须用同一个集合，不能两边各写一份近似规则。
var terminalStatuses = map[Status]bool{
	StatusDone:       true,
	StatusSkipped:    true,
	StatusFailed:     true,
	StatusRolledback: true,
	StatusCancelled:  true,
	StatusRestored:   true,
}

// IsTerminal 报告这个状态是不是终态。
func IsTerminal(s Status) bool { return terminalStatuses[s] }

// IsActive 报告任务是否还在队列里占位：等待、执行中或恢复中。
// 同一个项目的同类任务只有在这一段里算「已有在途」，用来挡住重复排队。
func IsActive(s Status) bool {
	return s == StatusWaiting || s == StatusRunning || s == StatusRestoring
}

// runStatus 给出某类任务开始执行时应该显示的状态：恢复类是「恢复中」，其余是「执行中」。
func runStatus(k Kind) Status {
	if k == KindRestore {
		return StatusRestoring
	}
	return StatusRunning
}

// Task 是一个任务的对外形状，同时是 GetTasks 快照里的条目和进度事件带的内容——
// 同一份 DTO，不分两套，否则前端要对着两个结构猜哪个字段更新。
// 数组和数值字段宁可给零值也不要 null，前端直接读不用判空。
type Task struct {
	ID         string `json:"id"`
	Kind       Kind   `json:"kind"`
	ItemID     string `json:"itemId"`
	Name       string `json:"name"`
	ScopeID    string `json:"scopeId,omitempty"`
	StrategyID string `json:"strategyId,omitempty"`
	// RoundID 把同一轮批量检测的任务串在一起：它们共享一份候选探测预算，
	// 由发起这次批量动作的入口创建。单项检测也有自己的轮次；空串只在旧记录里出现。
	RoundID    string `json:"roundId,omitempty"`
	Status     Status `json:"status"`
	Phase      string `json:"phase,omitempty"`
	PhaseText  string `json:"phaseText,omitempty"`
	Progress   int    `json:"progress"`
	ErrorMsg   string `json:"errorMsg,omitempty"`
	RetryOf    string `json:"retryOf,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
	Duration   string `json:"duration,omitempty"`
}

// String 只给日志和测试断言用，不参与 JSON。
func (t Task) String() string {
	return fmt.Sprintf("%s %s/%s %s", t.ID, t.Kind, t.ItemID, t.Status)
}

// NewRequest 是排一条任务所需的最少信息。
// Name 留空时由 ItemID 顶上，前端显示不会拿到空标题。
type NewRequest struct {
	Kind       Kind
	ItemID     string
	Name       string
	ScopeID    string
	StrategyID string
	RoundID    string
}

// Outcome 是执行器交回的结果。
// Status 留空表示「按任务类型算成功」：检测和应用落到 done，恢复落到 restored。
// Message 只在失败、跳过、回滚时给前端一句人话；成功时的文案会被丢掉，不显示成噪音。
type Outcome struct {
	Status  Status
	Message string
}

// Report 是执行器往上汇报中间进度的回调：阶段标识、给看的文字、0 到 100 的比例。
// 回调可能在队列锁里被调用，执行器不要在里面再回头调队列的其他方法。
type Report func(phase string, text string, progress int)

// Executor 负责把一条任务真正做完。本包不实现它：
// 检测、写 Hosts、刷新 DNS 都在别处，队列只管排队、记账和取消。
// 实现必须尊重 ctx：被取消时尽快返回，返回的结果会被已取消的状态覆盖掉。
type Executor interface {
	Execute(ctx context.Context, task Task, report Report) Outcome
}

// Progress 是一条进度事件的内容：单调递增的序号加上任务当时的完整快照。
// 前端只接受比自己见过的序号大的事件；序号对不上就重新取快照，不自己拼半截状态。
type Progress struct {
	Sequence int64 `json:"sequence"`
	Task     Task  `json:"task"`
}

// Publisher 是队列对事件总线的唯一要求：把一条进度事件投出去。
// 谁实现它、事件叫什么名字、发到哪条总线，都不归本包决定。
type Publisher interface {
	PublishProgress(p Progress)
}

// nopPublisher 用于不关心事件的调用方（测试和后续装配前的过渡期）。
type nopPublisher struct{}

func (nopPublisher) PublishProgress(Progress) {}
