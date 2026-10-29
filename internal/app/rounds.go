package app

import (
	"strconv"
	"sync"
	"sync/atomic"
)

// roundBudgets 管一次批量检测动作共享的候选探测预算。
//
// 规划的预算语义是「每轮」：一次批量/服务检测动作是同一轮，这一轮的所有项目
// 共享 min(本轮唯一域名数 × 档位每域名候选上限, 用户总上限) 的探测预算；
// 单项检测就是只有一项的一轮。任务执行时从轮上取预算、跑完把没用掉的还回去，
// 别的项目就能多测——预算不足的任务明确标成「预算不足，未检测」，不静默跳过。
//
// 轮只在内存里，应用重启即清空；没有轮记录文件。
type roundBudgets struct {
	mu   sync.Mutex
	byID map[string]*int64
	seq  atomic.Int64
}

func newRoundBudgets() *roundBudgets {
	return &roundBudgets{byID: make(map[string]*int64)}
}

// newRound 建一轮预算，返回轮 ID。预算就是这一轮允许启动的候选探测总数。
func (r *roundBudgets) newRound(budget int) string {
	id := "r" + strconv.FormatInt(r.seq.Add(1), 10)
	b := int64(budget)
	r.mu.Lock()
	r.byID[id] = &b
	r.mu.Unlock()
	return id
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
