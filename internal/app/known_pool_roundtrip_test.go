package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/detector"
	"github.com/xiaokentrl/github-net/internal/store"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 底账和排队之间是一条真链路：第一轮的真结论落盘，下一轮开局从底账读回来决定谁先被探测。
// 这条用例盯的是两边写法对不对得上——底账存的是状态字符串，排队认的是检测包的状态常量；
// 这两套字面量哪天错开，前置会静默失效（一个可用地址都不排前），界面上看不出原因。
func TestKnownPoolRoundTripThroughLedger(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "detect-history.db"))
	if err != nil {
		t.Fatalf("候选底账打不开：%v", err)
	}
	defer db.Close()
	svc := &Service{candidateStore: db}

	// 第一轮：同一个域名的两条候选，一条可用（延迟 5）、一条超时。
	probeWith := func(byIP map[string]detector.Result) accelerator.ProbeFunc {
		return func(_ context.Context, got detector.Target) (detector.Result, error) {
			if res, ok := byIP[got.IP]; ok {
				return res, nil
			}
			return detector.Result{OK: true, Verified: true, LatencyMS: 90}, nil
		}
	}
	firstRound := probeWith(map[string]detector.Result{
		"203.0.113.10": {OK: true, Verified: true, LatencyMS: 5},
		"203.0.113.11": {Kind: detector.FailTimeout, Detail: "对端没有按时响应", LatencyMS: 900},
	})
	lookup := &detectLookup{cands: detectCandidates()}
	exec := newDetectExecutorForTest(t, detectStore(t, nil), firstRound)
	exec.strategyFor = lookup.table(lookup.strategy(detectMethodGitHub, []string{detectScopeGitHub}))
	var first *accelerator.Result
	exec.save = func(_ string, _ string, _ string, r *accelerator.Result) { first = r }
	if out := exec.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub,
			StrategyID: detectMethodGitHub, RoundID: "round-1"},
		(&detectReports{}).report); out.Status != task.StatusDone {
		t.Fatalf("第一轮该跑完，实际 %+v", out)
	}
	if first == nil {
		t.Fatal("第一轮该把结论交给底账接缝")
	}
	if err := db.SaveRound("round-1", detectScopeGitHub, detectMethodGitHub, first); err != nil {
		t.Fatalf("第一轮落盘失败：%v", err)
	}

	// 读回来：域名下面两条候选都在，状态各是当时那个结论，时间就是那一轮的结束时刻。
	known := svc.knownCandidatesFor([]string{detectHostAPI})
	var gotAPI, gotTimeout bool
	for _, k := range known[detectHostAPI] {
		switch k.IP {
		case "203.0.113.10":
			gotAPI = k.Status == accelerator.StatusAvailable && k.LatencyMS == 5
		case "203.0.113.11":
			gotTimeout = k.Status == accelerator.StatusTimeout
		}
		if _, err := time.Parse(time.RFC3339, k.UpdatedAt); err != nil {
			t.Errorf("底账那句时间要读得懂，否则下一轮只会把它当过期：%q", k.UpdatedAt)
		}
	}
	if !gotAPI || !gotTimeout {
		t.Fatalf("底账读回来的状态要和检测时的结论对上，实得 %+v", known[detectHostAPI])
	}

	// 第二轮：本轮解析先给出那条超时的，前置要把它换到可用那条之后。
	lookup2 := &detectLookup{cands: map[string][]strategies.Candidate{
		detectHostAPI:      {{IP: "203.0.113.11"}, {IP: "203.0.113.10"}},
		detectHostCodeload: {{IP: "203.0.113.12"}, {IP: "203.0.113.13"}},
	}}
	exec2 := newDetectExecutorForTest(t, detectStore(t, nil), firstRound)
	exec2.strategyFor = lookup2.table(lookup2.strategy(detectMethodGitHub, []string{detectScopeGitHub}))
	exec2.knownPool = svc.knownCandidatesFor
	if out := exec2.Execute(context.Background(),
		task.Task{Kind: task.KindDetect, ItemID: "api", ScopeID: detectScopeGitHub,
			StrategyID: detectMethodGitHub, RoundID: "round-2"},
		(&detectReports{}).report); out.Status != task.StatusDone {
		t.Fatalf("第二轮该跑完，实际 %+v", out)
	}
	view, ok := exec2.results.view("api")
	if !ok {
		t.Fatal("第二轮的结果该存进结果存储供详情页取")
	}
	for _, h := range view.Hosts {
		if h.Hostname != detectHostAPI {
			continue
		}
		if len(h.Candidates) == 0 || h.Candidates[0].IP != "203.0.113.10" {
			t.Fatalf("上一轮可用那条该排在最前面，实得 %+v", h.Candidates)
		}
	}
}
