package task

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Queue 是内存任务队列。同一时刻只执行一条任务，按入队顺序，和原型里
// 「队列正在执行中」那一屏的行为一致；并行执行会让 Hosts 写入互相踩，不是这里要的形状。
//
// 所有可导方法都能并发调用。事件投递在队列锁内完成：发布器只做一次总线投递、
// 不会回头调本包的方法，所以不会自锁；换来的是「状态变更和它的事件序号严格同序」，
// 慢订阅者会拖慢队列，这是已知取舍。
type Queue struct {
	exec Executor
	pub  Publisher
	// now 只给测试换时钟；生产用 time.Now。
	now func() time.Time

	mu       sync.Mutex
	entries  map[string]*entry
	order    []string
	sequence int64
	idSeq    int
	paused   bool
	// driving 表示已经有一个驱动协程在领取任务，避免入队时开出第二个执行者。
	driving bool
	// running 是当前正在执行的任务 ID，空串代表没有；cancel 用来取消它。
	running string
	cancel  context.CancelFunc
	// stopped 之后不再领取新任务，也不再接受入队。
	stopped bool
	wg      sync.WaitGroup
}

type entry struct {
	task    Task
	started time.Time
}

// NewQueue 建一个空队列。pub 给 nil 就是不发事件，方便测试和未接线阶段使用。
func NewQueue(exec Executor, pub Publisher) *Queue {
	if pub == nil {
		pub = nopPublisher{}
	}
	return &Queue{
		exec:    exec,
		pub:     pub,
		now:     time.Now,
		entries: make(map[string]*entry),
	}
}

// Snapshot 是队列的当前副本：任务按入队顺序排列，Sequence 是水位。
// 前端先订阅事件再取快照，丢掉水位以内已经包含的事件；水位之后的事件才往界面上应用。
// Paused 一并带出，因为没有别的地方能读到暂停状态。
type Snapshot struct {
	Tasks    []Task `json:"tasks"`
	Sequence int64  `json:"sequence"`
	Paused   bool   `json:"paused"`
}

// RetryResult 是批量重试的逐项结果：成功给新任务 ID，被拒给原因。
// 一次请求里两项互不影响，不会一项不合格就把整批挡回去。
type RetryResult struct {
	SourceTaskID string `json:"sourceTaskId"`
	TaskID       string `json:"taskId,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// Enqueue 把一条任务排到队尾并返回它的快照。
// 同一项目的同类任务已经在队列里（等待或执行中）时不重复排，直接返回已在的那条——
// 连点两次「检测」不应该出现两条一样的任务，也不该因此报错。
func (q *Queue) Enqueue(req NewRequest) (Task, error) {
	if err := normalize(&req); err != nil {
		return Task{}, err
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return Task{}, ErrQueueStopped
	}
	if existing := q.findActiveLocked(req.Kind, req.ItemID); existing != nil {
		return existing.task, nil
	}
	t := q.appendLocked(Task{
		Kind:       req.Kind,
		ItemID:     req.ItemID,
		Name:       req.Name,
		ScopeID:    req.ScopeID,
		StrategyID: req.StrategyID,
		Status:     StatusWaiting,
	})
	return t.task, nil
}

// Pause 停止领取新的等待任务；已经在跑的那条不受影响，会跑完。
// 重复暂停幂等，返回当前是否处于暂停。
func (q *Queue) Pause() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.paused = true
	return q.paused
}

// Resume 恢复领取，按原队列顺序继续；重复继续幂等。
func (q *Queue) Resume() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.paused = false
	q.pumpLocked()
	return q.paused
}

// Paused 报告当前是否暂停。
func (q *Queue) Paused() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.paused
}

// Cancel 取消一条排队中或执行中的任务，返回取消后的快照。
// 等待中的任务就地变成「已取消」；执行中的任务会收到取消信号，
// 状态以取消这一刻为准——执行器之后返回什么都不会再把它改回完成或失败。
// 终态任务不能被取消，返回 ErrTaskFinished；队列里没有这条记录返回 ErrTaskNotFound。
func (q *Queue) Cancel(taskID string) (Task, error) {
	id := strings.TrimSpace(taskID)
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.entries[id]
	if !ok {
		return Task{}, fmt.Errorf("%w: %s", ErrTaskNotFound, id)
	}
	if IsTerminal(e.task.Status) {
		return e.task, fmt.Errorf("%w: %s", ErrTaskFinished, id)
	}
	e.task.Status = StatusCancelled
	e.finish(q.now())
	q.publishLocked(e)
	if q.running == id && q.cancel != nil {
		q.cancel()
	}
	return e.task, nil
}

// ClearFinished 移除全部终态记录（完成、跳过、失败、已回滚、已取消、已恢复），
// 保留等待中与执行中的任务，返回移除条数。没有终态记录时幂等成功。
// 它不碰 Hosts、不碰候选结果、不碰设置。
func (q *Queue) ClearFinished() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := make([]string, 0, len(q.order))
	removed := 0
	for _, id := range q.order {
		e, ok := q.entries[id]
		if ok && IsTerminal(e.task.Status) {
			delete(q.entries, id)
			removed++
			continue
		}
		kept = append(kept, id)
	}
	q.order = kept
	return removed
}

// ClearQueue 移除全部任务记录，包括还没执行的等待任务，返回移除条数。
// 有任务正在执行时拒绝并返回 ErrQueueRunning：正在跑的那条不能从队列里消失。
// 队列只在内存中，没有持久化删除，也就没有「删除失败」。
func (q *Queue) ClearQueue() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.running != "" {
		return 0, fmt.Errorf("%w: %s", ErrQueueRunning, q.running)
	}
	removed := len(q.order)
	q.order = nil
	q.entries = make(map[string]*entry)
	return removed, nil
}

// RetryFailed 为每条被接受的原任务在队尾新建一条重试任务。
// 只接受失败或已回滚的应用任务；原记录保持终态不动，新任务用 RetryOf 指回原任务，
// 这样界面上「第 1 次失败、第 2 次成功」两条都在，不覆盖历史。
// 逐项给结果，不接受的项返回原因。
func (q *Queue) RetryFailed(taskIDs []string) []RetryResult {
	q.mu.Lock()
	defer q.mu.Unlock()
	results := make([]RetryResult, 0, len(taskIDs))
	for _, raw := range taskIDs {
		id := strings.TrimSpace(raw)
		result := RetryResult{SourceTaskID: id}
		e, ok := q.entries[id]
		switch {
		case q.stopped:
			result.Reason = "任务队列已停止，这次重试没有排队"
		case !ok:
			result.Reason = "队列里已经没有这条任务记录"
		case e.task.Kind != KindApply:
			result.Reason = "只有应用任务支持重试，检测任务请直接重新检测"
		case e.task.Status != StatusFailed && e.task.Status != StatusRolledback:
			result.Reason = "这条任务不是失败或已回滚状态"
		case q.findActiveLocked(e.task.Kind, e.task.ItemID) != nil:
			result.Reason = "这个项目已经有在途任务，先等它结束"
		default:
			source := e.task
			nt := q.appendLocked(Task{
				Kind:       source.Kind,
				ItemID:     source.ItemID,
				Name:       source.Name,
				ScopeID:    source.ScopeID,
				StrategyID: source.StrategyID,
				Status:     StatusWaiting,
				RetryOf:    source.ID,
			})
			result.TaskID = nt.task.ID
		}
		results = append(results, result)
	}
	return results
}

// Snapshot 返回队列副本和当前水位；顺序就是执行顺序。
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	snap := Snapshot{
		Tasks:    make([]Task, 0, len(q.order)),
		Sequence: q.sequence,
		Paused:   q.paused,
	}
	for _, id := range q.order {
		if e, ok := q.entries[id]; ok {
			snap.Tasks = append(snap.Tasks, e.task)
		}
	}
	return snap
}

// Sequence 返回已发出的最后一个事件序号；还没发过事件时为 0。
func (q *Queue) Sequence() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.sequence
}

// Shutdown 停止领取新任务、取消正在执行的那条，并等到驱动协程退出。
// 队列退出后不再接受入队，也不再把等待任务标成已取消：队列本来就要随进程一起消失。
func (q *Queue) Shutdown() {
	q.mu.Lock()
	q.stopped = true
	if q.cancel != nil {
		q.cancel()
	}
	q.mu.Unlock()
	q.wg.Wait()
}

// normalize 校验排队请求：类型必须认识，项目 ID 必须有；Name 空时用项目 ID 顶上。
func normalize(req *NewRequest) error {
	req.Kind = Kind(strings.TrimSpace(string(req.Kind)))
	req.ItemID = strings.TrimSpace(req.ItemID)
	req.Name = strings.TrimSpace(req.Name)
	req.ScopeID = strings.TrimSpace(req.ScopeID)
	req.StrategyID = strings.TrimSpace(req.StrategyID)
	if !req.Kind.valid() {
		return fmt.Errorf("%w: 不认识的任务类型 %q", ErrInvalidRequest, req.Kind)
	}
	if req.ItemID == "" {
		return fmt.Errorf("%w: 缺少项目 ID", ErrInvalidRequest)
	}
	if req.Name == "" {
		req.Name = req.ItemID
	}
	return nil
}

// appendLocked 假定调用方已持锁：编号、入队尾、发事件、必要时启动驱动。
func (q *Queue) appendLocked(t Task) *entry {
	q.idSeq++
	t.ID = fmt.Sprintf("t%d", q.idSeq)
	e := &entry{task: t}
	q.entries[t.ID] = e
	q.order = append(q.order, t.ID)
	q.publishLocked(e)
	q.pumpLocked()
	return e
}

// findActiveLocked 找出同类型同项目的在途任务；没有则返回 nil。
func (q *Queue) findActiveLocked(k Kind, itemID string) *entry {
	for _, id := range q.order {
		e, ok := q.entries[id]
		if ok && e.task.Kind == k && e.task.ItemID == itemID && IsActive(e.task.Status) {
			return e
		}
	}
	return nil
}

// pumpLocked 假定调用方已持锁：有待跑任务且没有驱动协程时启动一个。
// 入队和领取都在同一把锁里判断，不会出现「入队以为有人在跑、驱动刚好退出」的漏唤醒。
func (q *Queue) pumpLocked() {
	if q.driving || q.stopped || q.paused {
		return
	}
	for _, id := range q.order {
		if e, ok := q.entries[id]; ok && e.task.Status == StatusWaiting {
			q.driving = true
			q.wg.Add(1)
			go q.drive()
			return
		}
	}
}

// drive 是唯一的执行者：循环领取队首的等待任务，跑完再领下一条；
// 没有可领的了（跑完、暂停、被停）就交出驱动身份退出。
func (q *Queue) drive() {
	defer q.wg.Done()
	for {
		t, ctx, claimed := q.claim()
		if !claimed {
			return
		}
		outcome := q.exec.Execute(ctx, t, q.reporter(t.ID))
		if keepGoing := q.settle(t.ID, outcome); !keepGoing {
			return
		}
	}
}

// claim 领取队首的等待任务并标成执行中。
// 暂停、已有任务在跑、队列已停或没有等待任务时返回 false，同时交出 driving 身份。
func (q *Queue) claim() (Task, context.Context, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.paused || q.stopped || q.running != "" {
		q.driving = false
		return Task{}, nil, false
	}
	for _, id := range q.order {
		e, ok := q.entries[id]
		if !ok || e.task.Status != StatusWaiting {
			continue
		}
		e.task.Status = runStatus(e.task.Kind)
		e.started = q.now()
		e.task.StartedAt = e.started.UTC().Format(time.RFC3339)
		e.task.Progress = 0
		e.task.Phase = ""
		e.task.PhaseText = ""
		ctx, cancel := context.WithCancel(context.Background())
		q.running, q.cancel = id, cancel
		snapshot := e.task
		q.publishLocked(e)
		return snapshot, ctx, true
	}
	q.driving = false
	return Task{}, nil, false
}

// reporter 生成某条任务的进度回调：只往还在执行中的记录上写阶段和比例。
// 已经取消或结束的任务不再被改，执行器收尾时晚到的汇报会被丢掉。
func (q *Queue) reporter(id string) Report {
	return func(phase string, text string, progress int) {
		q.mu.Lock()
		defer q.mu.Unlock()
		e, ok := q.entries[id]
		if !ok || !IsActive(e.task.Status) {
			return
		}
		e.task.Phase = strings.TrimSpace(phase)
		e.task.PhaseText = strings.TrimSpace(text)
		e.task.Progress = clampPercent(progress)
		q.publishLocked(e)
	}
}

// settle 收尾一条任务：登记执行器给出的结论并落终态，返回是否继续领下一条。
// 已经被取消的任务保持取消，不再按执行器的结果改写。
func (q *Queue) settle(id string, out Outcome) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cancel != nil {
		q.cancel()
		q.cancel = nil
	}
	q.running = ""
	if e, ok := q.entries[id]; ok && IsActive(e.task.Status) {
		status, message := outcomeStatus(e.task.Kind, out)
		e.task.Status = status
		e.task.ErrorMsg = message
		e.finish(q.now())
		q.publishLocked(e)
	}
	if q.stopped {
		q.driving = false
		return false
	}
	return true
}

// publishLocked 假定调用方已持锁：占用下一个序号并投递一条进度事件。
// 序号在这里递增，快照的 Sequence 读的是同一个计数器，两者不会各走各的。
func (q *Queue) publishLocked(e *entry) {
	q.sequence++
	q.pub.PublishProgress(Progress{Sequence: q.sequence, Task: e.task})
}

// finish 记录结束时间和耗时。没开始过的任务（等待中被取消）耗时留空，
// 前端显示成「—」，不会看到一个假数字。
func (e *entry) finish(now time.Time) {
	e.task.FinishedAt = now.UTC().Format(time.RFC3339)
	if e.started.IsZero() {
		return
	}
	d := now.Sub(e.started)
	if d < 0 {
		d = 0
	}
	e.task.Duration = d.Round(10 * time.Millisecond).String()
}

// outcomeStatus 把执行器结果翻译成终态。
// 留空表示按类型成功；类型对不上的成功态（比如恢复任务报 done）会被纠正；
// 认不出来的状态一律按失败收口，消息里不带执行器的内部细节。
func outcomeStatus(k Kind, out Outcome) (Status, string) {
	success := StatusDone
	if k == KindRestore {
		success = StatusRestored
	}
	switch out.Status {
	case "":
		return success, ""
	case StatusDone, StatusRestored, StatusSkipped, StatusFailed, StatusRolledback:
		message := strings.TrimSpace(out.Message)
		if out.Status == success {
			return out.Status, ""
		}
		if out.Status == StatusDone || out.Status == StatusRestored {
			// 成功态和任务类型对不上（恢复任务报「完成」）：纠正成该类型该有的成功态，
			// 结论是对的，只是档位名字用错了，不值得为此把一次成功的恢复标成失败。
			return success, ""
		}
		if message == "" {
			message = missingReason(out.Status)
		}
		return out.Status, message
	default:
		return StatusFailed, "执行器返回了无法识别的结果，按失败处理"
	}
}

// missingReason 给「没有原因」的终态补一句人话；错误展示不能是空白。
func missingReason(s Status) string {
	switch s {
	case StatusSkipped:
		return "这次任务被跳过，执行器没有给出原因"
	case StatusRolledback:
		return "任务失败且已退回改动前的状态，执行器没有给出原因"
	default:
		return "任务失败，执行器没有给出原因"
	}
}

func clampPercent(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// 保证接口形状在编译期就对上。
var (
	_ Publisher = nopPublisher{}
)
