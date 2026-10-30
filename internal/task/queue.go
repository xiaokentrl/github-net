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
	// 轮次登记表：ID 只由 BeginRound 生成，是全工程唯一的轮 ID 来源。
	// 预算账本（internal/app 的 roundBudgets）用的也是这个 ID，两边不再各发一套号。
	rounds   map[string]*Round
	roundIDs []string
	roundSeq int
	// driving 表示已经有一个驱动协程在领取任务，避免入队时开出第二个执行者。
	driving bool
	// 双车道：检测类任务可以并行（上限 detectLane），应用/恢复类严格串行——
	// 并行写 Hosts 会互相踩。两个车道之间允许并行：检测走网络、应用走特权服务，互不碰同一份状态。
	applyRunning  string
	applyCancel   context.CancelFunc
	detectRunning map[string]context.CancelFunc
	detectLane    int
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
	return NewQueueLaned(exec, pub, 1)
}

// NewQueueLaned 建一条带检测并行的队列：detectLane 是同时执行的检测任务上限
// （应用的串行车道不受它影响）。lane 给 1 就是「一次只跑一条检测」的旧行为。
func NewQueueLaned(exec Executor, pub Publisher, detectLane int) *Queue {
	if pub == nil {
		pub = nopPublisher{}
	}
	if detectLane < 1 {
		detectLane = 1
	}
	return &Queue{
		exec:          exec,
		pub:           pub,
		now:           time.Now,
		entries:       make(map[string]*entry),
		rounds:        make(map[string]*Round),
		detectRunning: make(map[string]context.CancelFunc),
		detectLane:    detectLane,
	}
}

// Snapshot 是队列的当前副本：任务按入队顺序排列，Sequence 是水位。
// 前端先订阅事件再取快照，丢掉水位以内已经包含的事件；水位之后的事件才往界面上应用。
// Paused 一并带出，因为没有别的地方能读到暂停状态。
//
// Rounds 只列「此刻还有任务挂在上面」的轮，按开轮先后排；一个任务都没剩下的轮不出现在快照里，
// 界面上也就不会出现一个点不开的空根节点。
type Snapshot struct {
	Tasks    []Task  `json:"tasks"`
	Rounds   []Round `json:"rounds"`
	Sequence int64   `json:"sequence"`
	Paused   bool    `json:"paused"`
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
		RoundID:    req.RoundID,
		Status:     StatusWaiting,
	})
	return t.task, nil
}

// BeginRound 开一轮，返回轮 ID；接着排进这个 ID 的任务都属于这一轮。
// 名字给空时按任务类型顶上（检测/应用/恢复），界面不会拿到一个空标题。
// 开轮本身不算错误：一轮还没有任务时它不进快照，等第一条任务排进来才现身。
func (q *Queue) BeginRound(kind Kind, scopeID, name string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.beginRoundLocked(kind, scopeID, name)
}

// beginRoundLocked 假定调用方已持锁。
func (q *Queue) beginRoundLocked(kind Kind, scopeID, name string) string {
	q.roundSeq++
	id := fmt.Sprintf("r%d", q.roundSeq)
	scopeID = strings.TrimSpace(scopeID)
	name = strings.TrimSpace(name)
	if name == "" {
		name = kindLabel(kind)
	}
	q.rounds[id] = &Round{
		ID:        id,
		Name:      name,
		Kind:      kind,
		ScopeID:   scopeID,
		CreatedAt: q.now().UTC().Format(time.RFC3339),
	}
	q.roundIDs = append(q.roundIDs, id)
	return id
}

// kindLabel 是轮名字兜底用的三种类型名，和原型队列里的三类标签一致。
func kindLabel(k Kind) string {
	switch k {
	case KindDetect:
		return "检测"
	case KindRestore:
		return "恢复"
	default:
		return "应用"
	}
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
	if cancel, ok := q.detectRunning[id]; ok {
		cancel()
	} else if q.applyRunning == id && q.applyCancel != nil {
		q.applyCancel()
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
	// 轮里最后一条任务被清掉，这一轮也就从队列里消失了。
	q.pruneRoundsWithoutTasksLocked()
	return removed
}

// ClearQueue 移除全部任务记录，包括还没执行的等待任务，返回移除条数。
// 有任务正在执行时拒绝并返回 ErrQueueRunning：正在跑的那条不能从队列里消失。
// 队列只在内存中，没有持久化删除，也就没有「删除失败」。
func (q *Queue) ClearQueue() (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.applyRunning != "" || len(q.detectRunning) > 0 {
		busy := q.applyRunning
		if busy == "" {
			for id := range q.detectRunning {
				busy = id
				break
			}
		}
		return 0, fmt.Errorf("%w: %s", ErrQueueRunning, busy)
	}
	removed := len(q.order)
	q.order = nil
	q.entries = make(map[string]*entry)
	// 任务全没了，轮也不留。轮号继续往后发，不清零、不复用。
	q.rounds = make(map[string]*Round)
	q.roundIDs = nil
	return removed, nil
}

// RetryFailed 为每条被接受的原任务在队尾新建一条重试任务。
// 只接受失败或已回滚的应用任务；原记录保持终态不动，新任务用 RetryOf 指回原任务，
// 这样界面上「第 1 次失败、第 2 次成功」两条都在，不覆盖历史。
// 逐项给结果，不接受的项返回原因。
//
// 这一批被接受的重试一起进一个新轮次，轮名按原型规则是「重试失败 · N 项」，
// N 是真正排进去的条数——被拒的项不占数，否则界面会看到一个对不上的数量。
func (q *Queue) RetryFailed(taskIDs []string) []RetryResult {
	q.mu.Lock()
	defer q.mu.Unlock()
	results := make([]RetryResult, 0, len(taskIDs))
	accepted := make([]int, 0, len(taskIDs))
	sources := make([]Task, 0, len(taskIDs))
	// 一次请求里同项目只收第一条：新任务要等整批校验完才排进去，
	// 所以「已有在途」这一条得先拿本次已接受的项比一遍，否则同一条被点两次会排进去两次。
	pending := make(map[string]bool, len(taskIDs))
	for _, raw := range taskIDs {
		id := strings.TrimSpace(raw)
		result := RetryResult{SourceTaskID: id}
		e, ok := q.entries[id]
		key := ""
		if ok {
			key = string(e.task.Kind) + "|" + e.task.ItemID
		}
		switch {
		case q.stopped:
			result.Reason = "任务队列已停止，这次重试没有排队"
		case !ok:
			result.Reason = "队列里已经没有这条任务记录"
		case e.task.Kind != KindApply:
			result.Reason = "只有应用任务支持重试，检测任务请直接重新检测"
		case e.task.Status != StatusFailed && e.task.Status != StatusRolledback:
			result.Reason = "这条任务不是失败或已回滚状态"
		case pending[key] || q.findActiveLocked(e.task.Kind, e.task.ItemID) != nil:
			result.Reason = "这个项目已经有在途任务，先等它结束"
		default:
			pending[key] = true
			accepted = append(accepted, len(results))
			sources = append(sources, e.task)
		}
		results = append(results, result)
	}
	if len(sources) == 0 {
		return results
	}
	roundID := q.beginRoundLocked(KindApply, "", fmt.Sprintf("重试失败 · %d 项", len(sources)))
	for i, source := range sources {
		nt := q.appendLocked(Task{
			Kind:       source.Kind,
			ItemID:     source.ItemID,
			Name:       source.Name,
			ScopeID:    source.ScopeID,
			StrategyID: source.StrategyID,
			RoundID:    roundID,
			Status:     StatusWaiting,
			RetryOf:    source.ID,
		})
		results[accepted[i]].TaskID = nt.task.ID
	}
	return results
}

// Snapshot 返回队列副本和当前水位；任务顺序就是执行顺序，轮次顺序是开轮先后。
func (q *Queue) Snapshot() Snapshot {
	q.mu.Lock()
	defer q.mu.Unlock()
	snap := Snapshot{
		Tasks:    make([]Task, 0, len(q.order)),
		Rounds:   make([]Round, 0, len(q.rounds)),
		Sequence: q.sequence,
		Paused:   q.paused,
	}
	live := q.liveRoundIDsLocked()
	for _, id := range q.order {
		if e, ok := q.entries[id]; ok {
			snap.Tasks = append(snap.Tasks, e.task)
		}
	}
	for _, r := range q.roundOrderLocked() {
		if live[r.ID] {
			snap.Rounds = append(snap.Rounds, *r)
		}
	}
	return snap
}

// roundOrderLocked 假定调用方已持锁：按开轮先后返回还登记着的轮。
func (q *Queue) roundOrderLocked() []*Round {
	out := make([]*Round, 0, len(q.roundIDs))
	for _, id := range q.roundIDs {
		if r, ok := q.rounds[id]; ok {
			out = append(out, r)
		}
	}
	return out
}

// liveRoundIDsLocked 假定调用方已持锁：哪些轮上还挂着任务。只读，不删登记——
// 读路径顺手删会让「刚 BeginRound、任务还没排进来」的 microseconds 窗口被一次快照剪空，
// 那批任务就永久挂在一个已经不存在的轮上，界面上整批看不见根节点。
// 空轮的清理交给 ClearFinished：用户点「清理已结束」时一并带走。
func (q *Queue) liveRoundIDsLocked() map[string]bool {
	live := make(map[string]bool, len(q.rounds))
	for _, id := range q.order {
		e, ok := q.entries[id]
		if !ok || e.task.RoundID == "" {
			continue
		}
		if _, ok := q.rounds[e.task.RoundID]; ok {
			live[e.task.RoundID] = true
		}
	}
	return live
}

// pruneRoundsWithoutTasksLocked 假定调用方已持锁：删掉没任务的轮，并压实开轮顺序。
func (q *Queue) pruneRoundsWithoutTasksLocked() {
	live := q.liveRoundIDsLocked()
	if len(q.rounds) == len(live) {
		return
	}
	kept := q.roundIDs[:0]
	for _, id := range q.roundIDs {
		if live[id] {
			kept = append(kept, id)
			continue
		}
		delete(q.rounds, id)
	}
	q.roundIDs = kept
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
	if q.applyCancel != nil {
		q.applyCancel()
	}
	for _, cancel := range q.detectRunning {
		cancel()
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

// drive 是唯一的领取者：循环把能领的任务领出来、交给自己的协程去跑，
// 然后继续领（所以多条检测任务真正并行）。没有可领的了（跑完、暂停、被停、车道满）
// 就交出驱动身份退出；某条任务跑完时 settle 会重新 pump。
func (q *Queue) drive() {
	defer q.wg.Done()
	for {
		t, ctx, claimed := q.claim()
		if !claimed {
			return
		}
		q.wg.Add(1)
		go func(t Task, ctx context.Context) {
			defer q.wg.Done()
			outcome := q.exec.Execute(ctx, t, q.reporter(t.ID))
			q.settle(t.ID, outcome)
		}(t, ctx)
	}
}

// claim 领取「自己的车道还有空位」的第一条等待任务并标成执行中。
// 应用/恢复车道满（有一条在跑）时不挡后面的检测任务——扫到能领的为止。
// 暂停、队列已停或全部车道都满时返回 false，同时交出 driving 身份。
func (q *Queue) claim() (Task, context.Context, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.paused || q.stopped {
		q.driving = false
		return Task{}, nil, false
	}
	for _, id := range q.order {
		e, ok := q.entries[id]
		if !ok || e.task.Status != StatusWaiting {
			continue
		}
		if e.task.Kind == KindDetect {
			if len(q.detectRunning) >= q.detectLane {
				continue
			}
		} else if q.applyRunning != "" {
			continue
		}
		e.task.Status = runStatus(e.task.Kind)
		e.started = q.now()
		e.task.StartedAt = e.started.UTC().Format(time.RFC3339)
		e.task.Progress = 0
		e.task.Phase = ""
		e.task.PhaseText = ""
		ctx, cancel := context.WithCancel(context.Background())
		if e.task.Kind == KindDetect {
			q.detectRunning[id] = cancel
		} else {
			q.applyRunning, q.applyCancel = id, cancel
		}
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
	if cancel, ok := q.detectRunning[id]; ok {
		cancel()
		delete(q.detectRunning, id)
	} else if q.applyRunning == id {
		if q.applyCancel != nil {
			q.applyCancel()
			q.applyCancel = nil
		}
		q.applyRunning = ""
	}
	if e, ok := q.entries[id]; ok && IsActive(e.task.Status) {
		status, message := outcomeStatus(e.task.Kind, out)
		e.task.Status = status
		e.task.ErrorMsg = message
		e.finish(q.now())
		q.publishLocked(e)
	}
	// 车道空出来了：把驱动重新拉起来领下一条（驱动可能早就因为「没得领」退出了）。
	q.pumpLocked()
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
