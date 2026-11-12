package app

import (
	"strings"
	"sync"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
)

// detectResultStore 存每个项目最近一轮的检测结果，只在本进程内存里。
// 检测结果不是持久数据：应用重启后为空，前端取不到就显示「还没有检测结果」。
//
// 只存最近一轮：同一项目再检测，上一轮结果直接被顶掉。规划里候选结果按
// scopeID + strategyID + itemID + 时间绑定，应用（阶段四）真正写 Hosts 前会重新验证，
// 不拿这里的旧结果直接用；这里只负责「界面看得到上一轮查到了什么」。
type detectResultStore struct {
	mu     sync.Mutex
	byItem map[string]detectResultRecord
}

type detectResultRecord struct {
	scopeID    string
	strategyID string
	result     *accelerator.Result
}

func newDetectResultStore() *detectResultStore {
	return &detectResultStore{byItem: make(map[string]detectResultRecord)}
}

// put 记下一轮结果。result 给 nil（一轮没跑起来）就不覆盖旧值：
// 失败的这轮没有产出，界面上还该看到上一轮的样子。
func (s *detectResultStore) put(scopeID, strategyID string, result *accelerator.Result) {
	if result == nil || strings.TrimSpace(result.ItemID) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byItem[result.ItemID] = detectResultRecord{scopeID: scopeID, strategyID: strategyID, result: result}
}

// markApplied 按系统 Hosts 的现状重标这轮候选的「当前生效」标记。
// 应用成功后调：写入前检测的那轮里标记是旧事实，Hosts 才是权威——
// 候选的地址现在写在这个主机名上就是 true，不在就是 false（包括把旧标记清掉）。
func (s *detectResultStore) markApplied(itemID string, applied map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byItem[itemID]
	if !ok {
		return
	}
	for i, c := range rec.result.Candidates {
		on := false
		for _, ip := range applied[c.Hostname] {
			if ip == c.IP {
				on = true
				break
			}
		}
		rec.result.Candidates[i].Applied = on
	}
}

// clearApplied 把全部项目候选上的「当前生效」标记清掉。
// 全部还原是一笔不进队列的命令，没有执行器来重标（markApplied 只在逐项应用/还原里调）。
// 它成功之后系统 Hosts 上已经没有本工具写的映射，标记留着就是界面在说假话。
func (s *detectResultStore) clearApplied() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range s.byItem {
		for i := range rec.result.Candidates {
			rec.result.Candidates[i].Applied = false
		}
	}
}

// clear 把全部项目最近一轮的结论整份丢掉。
// 「清理全部 IP 缓存」用：底账、地址池和内存这一份都是同一批检测产出，
// 只清前两处、界面还摆着旧候选，列表看起来就像没跟着变。
func (s *detectResultStore) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byItem = make(map[string]detectResultRecord)
}

// count 报内存里记着几个项目的最近一轮结论。「清理全部 IP 缓存」会把这份一起丢掉，
// 预览按这个数如实说，别让用户以为只清了库里那几张表。
func (s *detectResultStore) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byItem)
}

// roundFinishedAt 给一个项目最近一轮的探测定格时间；没有记录给零值。
// 调用方（应用前的时效判定）把零值按「不新鲜」处理：说不清什么时候测出来的结论，
// 一律要现场复验一遍才准写进系统 Hosts。
func (s *detectResultStore) roundFinishedAt(itemID string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byItem[itemID]
	if !ok {
		return time.Time{}
	}
	return rec.result.FinishedAt
}

// markProbe 把一次现场复验的结论刷回内存这一轮里那条候选：状态、延迟、定格时间一起换。
// 刷新定格时间等于这条结论从这一刻起重新计算新鲜度，不用再为同一件事复验第二次。
// 找不到对应那行就什么都不动；动过就重算本轮与各主机的「可用」计数，
// 界面上那句「可用 N」跟着新事实走，不会留着复验前的数字。
func (s *detectResultStore) markProbe(itemID, hostname, ip string, status accelerator.CandidateStatus,
	latencyMS int64, reason string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byItem[itemID]
	if !ok {
		return
	}
	r := rec.result
	touched := false
	for i := range r.Candidates {
		c := &r.Candidates[i]
		if c.Hostname != hostname || c.IP != ip {
			continue
		}
		c.Status = status
		c.LatencyMS = latencyMS
		c.Reason = reason
		c.FinishedAt = at.UTC().Format(time.RFC3339)
		touched = true
	}
	if !touched {
		return
	}
	r.Available = 0
	for i := range r.Hostnames {
		r.Hostnames[i].Available = 0
	}
	available := make(map[string]int, len(r.Hostnames))
	for _, c := range r.Candidates {
		if c.Status == accelerator.StatusAvailable {
			r.Available++
			available[c.Hostname]++
		}
	}
	for i := range r.Hostnames {
		r.Hostnames[i].Available = available[r.Hostnames[i].Hostname]
	}
}

// view 把某项目最近一轮结果整理成给前端的 DTO。没有记录时 ok 为 false，
// 调用方返回「还没有检测结果」的空形状，这不是错误。
func (s *detectResultStore) view(itemID string) (DetectResultView, bool) {
	// DTO 组装全程持锁：markProbe/markApplied 会原地改 Candidates/Hostnames，
	// 解锁后遍历是真实数据竞争（对照复核 G1）。
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byItem[itemID]
	if !ok {
		return DetectResultView{}, false
	}
	return detectResultView(itemID, rec), true
}

// DetectResultView 是一个项目最近一轮检测的完整快照，给详情页的候选表直接渲染。
// 数组字段一律给空切片不给 null；没有记录时调用方自己拼空形状。
type DetectResultView struct {
	ItemID     string `json:"itemId"`
	ScopeID    string `json:"scopeId"`
	StrategyID string `json:"strategyId"`
	// StrategyName 是方法面向用户的名字（注册表里那份）；方法已卸载时给空串，前端回落到 ID。
	StrategyName string           `json:"strategyName"`
	StartedAt    string           `json:"startedAt,omitempty"`
	FinishedAt   string           `json:"finishedAt,omitempty"`
	Available    int              `json:"available"`
	ProbesUsed   int              `json:"probesUsed"`
	DNSQueries   int              `json:"dnsQueries"`
	Truncated    bool             `json:"truncated"`
	DeadlineHit  bool             `json:"deadlineHit"`
	Cancelled    bool             `json:"cancelled"`
	Hosts        []DetectHostView `json:"hosts"`
}

// DetectHostView 是一个主机名在这轮里的结果；候选行直接复用 accelerator.Candidate，
// 它的字段和 JSON 名就是前端候选表的列，再抄一层只会两边各改各的。
type DetectHostView struct {
	Hostname   string                  `json:"hostname"`
	Resolved   bool                    `json:"resolved"`
	Detail     string                  `json:"detail,omitempty"`
	Candidates []accelerator.Candidate `json:"candidates"`
}

func detectResultView(itemID string, rec detectResultRecord) DetectResultView {
	r := rec.result
	// Result 里的候选行是扁平列表；详情页按主机名分组展示，这里按行顺序归组，
	// 同一主机名的行保持本轮内的相对顺序。
	rowsByHost := make(map[string][]accelerator.Candidate, len(r.Hostnames))
	for _, c := range r.Candidates {
		rowsByHost[c.Hostname] = append(rowsByHost[c.Hostname], c)
	}
	hosts := make([]DetectHostView, 0, len(r.Hostnames))
	for _, h := range r.Hostnames {
		rows := rowsByHost[h.Hostname]
		if rows == nil {
			rows = []accelerator.Candidate{}
		}
		hosts = append(hosts, DetectHostView{
			Hostname:   h.Hostname,
			Resolved:   h.Resolved,
			Detail:     h.Detail,
			Candidates: rows,
		})
	}
	return DetectResultView{
		ItemID:       itemID,
		ScopeID:      rec.scopeID,
		StrategyID:   rec.strategyID,
		StrategyName: strategyDisplayName(rec.strategyID),
		StartedAt:    r.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt:   r.FinishedAt.UTC().Format(time.RFC3339),
		Available:    r.Available,
		ProbesUsed:   r.ProbesUsed,
		DNSQueries:   r.DNSQueries,
		Truncated:    r.Truncated,
		DeadlineHit:  r.DeadlineHit,
		Cancelled:    r.Cancelled,
		Hosts:        hosts,
	}
}

// detectResultEmptyView 是「还没有检测结果」的空形状：字段都是零值，数组是空切片。
func detectResultEmptyView(itemID string) DetectResultView {
	return DetectResultView{ItemID: itemID, Hosts: []DetectHostView{}}
}

// strategyDisplayName 把方法 ID 翻成用户可见的名字；注册表里查不到（方法被删）就给空串。
func strategyDisplayName(id string) string {
	if s, ok := strategies.Get(id); ok {
		return s.Name
	}
	return ""
}
