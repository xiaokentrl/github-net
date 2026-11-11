package app

import (
	"sync"
)

// roundBudgets 管一次批量检测动作共享的候选探测预算。
//
// 规划的预算语义是「每轮」：一次批量/服务检测动作是同一轮，这一轮的所有项目
// 共享 min(本轮唯一域名数 × 档位每域名候选上限, 用户总上限) 的探测预算；
// 单项检测就是只有一项的一轮。任务执行时从轮上取预算、跑完把没用掉的还回去，
// 别的项目就能多测——预算不足的任务明确标成「预算不足，未检测」，不静默跳过。
//
// 轮的 ID 不在这里生成：队列是轮 ID 的唯一来源（task.Queue.BeginRound），
// 这里只按那个 ID 记一份预算账本，避免两处各发一套「r1、r2」互相顶掉。
//
// 轮只在内存里，应用重启即清空；没有轮记录文件。
type roundBudgets struct {
	mu   sync.Mutex
	byID map[string]*int64
	// profileByID 记这一轮用户选定的检测档位。只影响本轮：不写回配置，也不改下一轮。
	// 任务真正开跑时执行器按 RoundID 取它，中途改设置也不会串档。
	profileByID map[string]string
}

// roundBudgetsMax 是轮账本的保留上限：长会话高频批量检测下，轮记录只增会无界增长。
// 每轮百字节级、速率低，但仍按基线纪律给上限——超限剪掉最老的轮（预算未用完的被剪
// 只是这轮不再受预算约束，等价于「早起的轮次早退役」，无正确性影响）。
const roundBudgetsMax = 500

func newRoundBudgets() *roundBudgets {
	return &roundBudgets{byID: make(map[string]*int64), profileByID: make(map[string]string)}
}

// pruneOldestLocked 剪掉多余的轮记录到上限内；skip 是本轮正在登记的 ID，不许剪。
// 调用方持有 r.mu。候选探测按预算走，被剪的轮等效「退役」，不再受预算约束。
func (r *roundBudgets) pruneOldestLocked(skip string) {
	excess := len(r.byID) - roundBudgetsMax
	if excess <= 0 {
		return
	}
	for roundID := range r.byID {
		if excess <= 0 || roundID == skip {
			continue
		}
		delete(r.byID, roundID)
		delete(r.profileByID, roundID)
		excess--
	}
}

// setProfile 给这一轮记下本次检测用的档位；空串表示按设置里的档位走。
func (r *roundBudgets) setProfile(roundID, profile string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profileByID[roundID] = profile
}

// profileOf 取这一轮选定的档位；没记过给空串，调用方照旧读配置。
func (r *roundBudgets) profileOf(roundID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.profileByID[roundID]
}

// put 给已经开好的轮登记预算。budget 就是这一轮允许启动的候选探测总数。
func (r *roundBudgets) put(roundID string, budget int) string {
	b := int64(budget)
	r.mu.Lock()
	r.pruneOldestLocked(roundID) // 先剪后加：本轮刚登记的档位与预算不会被自己剪掉
	r.byID[roundID] = &b
	r.mu.Unlock()
	return roundID
}

// take 尽量从轮上取 n 个预算，返回实际拿到的数量；轮不存在（旧任务）按「不受限」处理，返回 n。
func (r *roundBudgets) take(roundID string, n int) int {
	if n <= 0 {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.byID[roundID]
	if !ok {
		return n
	}
	cur := *p
	if cur <= 0 {
		return 0
	}
	grant := n
	if int64(grant) > cur {
		grant = int(cur)
	}
	*p = cur - int64(grant)
	return grant
}

// release 把没用掉的预算还给轮；轮已不存在（不会发生，防御一下）就丢掉。
func (r *roundBudgets) release(roundID string, n int) {
	if n <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.byID[roundID]; ok {
		*p += int64(n)
	}
}
