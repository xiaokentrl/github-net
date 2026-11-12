package event

import (
	"sort"
	"sync"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
)

// DetectProgressName 是候选检测进度事件的名字。全仓库只有这一处写它，
// 前端订阅用的必须是同一个字符串。和任务事件分开：任务事件说「这一条任务整体到哪了」，
// 这个事件说「这一轮里每条候选此刻的样子」，粒度和消费方都不同。
const DetectProgressName = "detect:progress"

const (
	// progressFlushInterval 是批量投递的节拍：攒这么久就把攒下的进度一次性发出去。
	// 50ms 对界面足够「实时」（人眼读不出差别），又把「探测一完就发一条」的碎事件
	// 收成最多 20 批/秒——突发洪峰受单批上限约束，持续速率则由探测并发闸（8）收口
	// （NFR 表的 ≤50 条/秒目标以此为据，实测数字仍待录，见 docs/NFR.md）。
	progressFlushInterval = 50 * time.Millisecond
	// progressFlushMax 是单批投递上限：一批攒得再多也只发这么多，剩下的留给下一拍。
	// 没有上限的话，完整档一轮可瞬时攒出几百条，一次投出去照样是洪峰。
	progressFlushMax = 50
)

// DetectEmitter 是候选级进度事件的发布器。事件内容直接复用 accelerator.Progress——
// 它本来就有完整 JSON 标签（候选 ID、域名、状态、序号、计数），再抄一份结构只会两边各改各的。
//
// 发布按「同键后到覆盖」合并再批量投递（NFR 表待办的落地）：
// 同一条候选在一拍内换了几次状态，界面只关心它现在什么样，所以只发最新一份；
// 探测完最后一批后没有新事件来触发下一拍，用 time.AfterFunc 定时兜底投出，
// 不留常驻 goroutine。序号保持首次转入的顺序，前端按序消费不受影响。
type DetectEmitter struct {
	emit func(name string, payload any)
	mu   sync.Mutex
	// pending 是攒着没发的进度，按首次转入的顺序排；index 记每个键在 pending 里的位置，
	// 同键再来的事件原地覆盖（后到覆盖），不追加新行。
	pending []accelerator.Progress
	index   map[string]int
	timer   *time.Timer
}

// NewDetectEmitter 包一个投递函数（装配层里通常是 Wails 的事件总线）。
// emit 为 nil 时投递静默丢弃：队列和检测没接到总线也要能跑。
func NewDetectEmitter(emit func(name string, payload any)) *DetectEmitter {
	return &DetectEmitter{emit: emit, index: map[string]int{}}
}

// progressKey 是合并的键：同一条候选（或同一域名级状态）一拍内只保留最新一份。
// 键里带上 ItemID：不同项目可以共用候选 ID 和主机名，界面上它们是各自行。
func progressKey(p accelerator.Progress) string {
	if p.CandidateID != "" {
		return p.ItemID + "|" + p.CandidateID
	}
	return p.ItemID + "|#" + p.Hostname + "|" + p.Phase
}

// Publish 收一条候选级进度事件：攒进待发清单，同键覆盖，攒够一拍就发。
func (e *DetectEmitter) Publish(p accelerator.Progress) {
	if e == nil || e.emit == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := progressKey(p)
	if at, ok := e.index[key]; ok {
		e.pending[at] = p
		return
	}
	e.index[key] = len(e.pending)
	e.pending = append(e.pending, p)
	if e.timer == nil {
		e.timer = time.AfterFunc(progressFlushInterval, e.flush)
	}
}

// flush 把攒下的进度投出去；单批不超过 progressFlushMax，没发完的下一拍继续。
// AfterFunc 到点调用，可能与 Publish 并发，所以先摘清单再放锁投递。
func (e *DetectEmitter) flush() {
	e.mu.Lock()
	if len(e.pending) == 0 {
		e.timer = nil
		e.mu.Unlock()
		return
	}
	// 摘清单必须拷贝：放锁后投递期间 Publish 还在往 e.pending 里 append，
	// 不拷贝的话 batch 和 e.pending 共用底层数组，同拍写入会覆写正在投递的元素（数据竞争）。
	batch := append([]accelerator.Progress(nil), e.pending...)
	if len(batch) > progressFlushMax {
		e.pending = append([]accelerator.Progress(nil), batch[progressFlushMax:]...)
		batch = batch[:progressFlushMax]
	} else {
		e.pending = e.pending[:0]
	}
	if len(e.pending) > 0 {
		// 没发完的部分重新排位：键还活着，下一拍的同键覆盖要能找到它们。
		e.index = make(map[string]int, len(e.pending))
		for i, p := range e.pending {
			e.index[progressKey(p)] = i
		}
		e.timer = time.AfterFunc(progressFlushInterval, e.flush)
	} else {
		e.index = map[string]int{}
		e.timer = nil
	}
	emit := e.emit
	e.mu.Unlock()
	// 批内按序号排序后投递：清单顺序是「键首次转入」的顺序，不是发生顺序——
	// 后到的候选若排在前面，计数（Done/ProbesUsed）会在前端回跳。
	sort.Slice(batch, func(i, j int) bool { return batch[i].Sequence < batch[j].Sequence })
	for _, p := range batch {
		emit(DetectProgressName, p)
	}
}

// Flush 把还攒在待发清单里的进度同步投完（分拍照旧）。轮收尾时调用：
// 不收尾的话，最后一批候选进度会晚于「轮完成」的任务事件到达，前端先看到轮结束再看到候选行。
func (e *DetectEmitter) Flush() {
	if e == nil {
		return
	}
	for {
		e.mu.Lock()
		if len(e.pending) == 0 {
			e.mu.Unlock()
			return
		}
		e.mu.Unlock()
		e.flush()
	}
}
