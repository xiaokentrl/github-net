package app

import (
	"strings"
	"sync"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
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

// view 把某项目最近一轮结果整理成给前端的 DTO。没有记录时 ok 为 false，
// 调用方返回「还没有检测结果」的空形状，这不是错误。
func (s *detectResultStore) view(itemID string) (DetectResultView, bool) {
	s.mu.Lock()
	rec, ok := s.byItem[itemID]
	s.mu.Unlock()
	if !ok {
		return DetectResultView{}, false
	}
	return detectResultView(itemID, rec), true
}

// DetectResultView 是一个项目最近一轮检测的完整快照，给详情页的候选表直接渲染。
// 数组字段一律给空切片不给 null；没有记录时调用方自己拼空形状。
type DetectResultView struct {
	ItemID      string           `json:"itemId"`
	ScopeID     string           `json:"scopeId"`
	StrategyID  string           `json:"strategyId"`
	StartedAt   string           `json:"startedAt,omitempty"`
	FinishedAt  string           `json:"finishedAt,omitempty"`
	Available   int              `json:"available"`
	ProbesUsed  int              `json:"probesUsed"`
	DNSQueries  int              `json:"dnsQueries"`
	Truncated   bool             `json:"truncated"`
	DeadlineHit bool             `json:"deadlineHit"`
	Cancelled   bool             `json:"cancelled"`
	Hosts       []DetectHostView `json:"hosts"`
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
		ItemID:      itemID,
		ScopeID:     rec.scopeID,
		StrategyID:  rec.strategyID,
		StartedAt:   r.StartedAt.UTC().Format(time.RFC3339),
		FinishedAt:  r.FinishedAt.UTC().Format(time.RFC3339),
		Available:   r.Available,
		ProbesUsed:  r.ProbesUsed,
		DNSQueries:  r.DNSQueries,
		Truncated:   r.Truncated,
		DeadlineHit: r.DeadlineHit,
		Cancelled:   r.Cancelled,
		Hosts:       hosts,
	}
}

// detectResultEmptyView 是「还没有检测结果」的空形状：字段都是零值，数组是空切片。
func detectResultEmptyView(itemID string) DetectResultView {
	return DetectResultView{ItemID: itemID, Hosts: []DetectHostView{}}
}
