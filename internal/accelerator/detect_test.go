package accelerator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/detector"
)

// stub 是假解析器加假探测器：全程不碰外网，才能把超时、取消、预算用完和各类证书失败
// 都跑一遍。返回值按主机名（解析）和 IP（探测）查表，没配的走默认「可用」。
type stub struct {
	mu           sync.Mutex
	candidates   map[string][]strategies.Candidate
	lookupErr    map[string]error
	results      map[string]detector.Result
	probeErr     map[string]error
	probeDelay   map[string]time.Duration
	lookupCalls  []string
	probeCalls   []string
	running      int
	maxRunning   int
	probeTargets []detector.Target
}

func newStub() *stub {
	return &stub{
		candidates: map[string][]strategies.Candidate{},
		lookupErr:  map[string]error{},
		results:    map[string]detector.Result{},
		probeErr:   map[string]error{},
		probeDelay: map[string]time.Duration{},
	}
}

func (s *stub) lookup(ctx context.Context, in strategies.Input) (strategies.Output, error) {
	s.mu.Lock()
	s.lookupCalls = append(s.lookupCalls, fmt.Sprintf("%s#%d", in.Hostname, in.Max))
	s.mu.Unlock()

	if err := s.lookupErr[in.Hostname]; err != nil {
		return strategies.Output{}, err
	}
	list := s.candidates[in.Hostname]
	out := strategies.Output{Candidates: list}
	if len(list) > in.Max {
		out.Candidates = list[:in.Max]
		out.Truncated = true
	}
	return out, nil
}

func (s *stub) probe(ctx context.Context, t detector.Target) (detector.Result, error) {
	s.mu.Lock()
	s.probeCalls = append(s.probeCalls, t.IP)
	s.probeTargets = append(s.probeTargets, t)
	s.running++
	if s.running > s.maxRunning {
		s.maxRunning = s.running
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running--
		s.mu.Unlock()
	}()

	if d := s.probeDelay[t.IP]; d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			// 和探测层同一套归因：上层期限到点算超时，用户取消算取消。
			if errors.Is(ctx.Err(), context.Canceled) {
				return detector.Result{Kind: detector.FailCancelled, Detail: "探测已取消"}, nil
			}
			return detector.Result{Kind: detector.FailTimeout, Detail: "对端没有按时响应"}, nil
		}
	}
	if err := s.probeErr[t.IP]; err != nil {
		return detector.Result{}, err
	}
	if res, ok := s.results[t.IP]; ok {
		return res, nil
	}
	return detector.Result{OK: true, Verified: true, LatencyMS: 7, TLSMS: 4}, nil
}

func (s *stub) counts() (lookups, probes, maxRunning int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.lookupCalls), len(s.probeCalls), s.maxRunning
}

func (s *stub) hostnames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lookupCalls...)
}

// recorder 记下每一次状态转换，顺序就是投递顺序。
type recorder struct {
	mu     sync.Mutex
	events []Progress
}

func (r *recorder) Observe(p Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, p)
}

func (r *recorder) all() []Progress {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Progress(nil), r.events...)
}

func budget(per, concurrency, maxProbes int) Budget {
	return Budget{
		Profile:     "low",
		PerHostname: per,
		Concurrency: concurrency,
		MaxProbes:   maxProbes,
		Needed:      maxProbes,
		deadline:    30 * time.Second,
	}
}

func v4host(n int) []strategies.Candidate {
	out := make([]strategies.Candidate, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, strategies.Candidate{IP: fmt.Sprintf("192.0.2.%d", i)})
	}
	return out
}

func available() detector.Result {
	return detector.Result{OK: true, Verified: true, LatencyMS: 9, TLSMS: 5}
}
func refused() detector.Result {
	return detector.Result{Kind: detector.FailRefused, Detail: "端口拒绝连接", LatencyMS: 2}
}
func certMismatch() detector.Result {
	return detector.Result{Kind: detector.FailCertMismatch, Detail: "证书不是签给这个主机名的", LatencyMS: 11, TLSMS: 6}
}

// 一轮正常的检测：候选一边解析一边列成排队中，探测阶段逐条走「检测中 → 定格」，
// 事件序号连续，最后每一行都定格、done 等于 total。
func TestRunEmitsFullCandidateLifecycle(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(2)
	s.candidates["b.test"] = []strategies.Candidate{{IP: "198.51.100.1"}, {IP: "2001:db8::1", IPv6: true}}
	s.results["192.0.2.2"] = refused()
	rec := &recorder{}

	res, err := NewRunner(s.lookup, s.probe, rec).Run(context.Background(),
		Input{ItemID: "item-a", Hostnames: []string{"a.test", "b.test"}}, budget(2, 1, 10))
	if err != nil {
		t.Fatalf("这一轮应当跑起来：%v", err)
	}
	if res.Available != 3 || len(res.Candidates) != 4 {
		t.Fatalf("4 条候选里应有 3 条可用，实得 %+v", res.Candidates)
	}

	events := rec.all()
	for i, p := range events {
		if p.Sequence != int64(i+1) {
			t.Fatalf("事件序号必须从 1 连续递增，第 %d 条序号是 %d", i+1, p.Sequence)
		}
	}
	last := events[len(events)-1]
	if last.Done != last.Total || last.Total != 4 {
		t.Errorf("收尾时完成数应等于总数 4，实得 完成 %d、总数 %d", last.Done, last.Total)
	}
	if last.Active != 0 {
		t.Errorf("本轮结束时不该还有活动请求，实得 %d", last.Active)
	}

	// 解析阶段：每个主机名一条「正在解析」，随后它的候选逐条列出为排队中。
	var resolving []string
	for _, p := range events {
		if p.Phase == PhaseResolving {
			resolving = append(resolving, p.Hostname)
		}
	}
	if want := []string{"a.test", "b.test"}; strings.Join(resolving, ",") != strings.Join(want, ",") {
		t.Errorf("解析阶段事件的主机名顺序应为 %v，实得 %v", want, resolving)
	}

	// 同一条候选的转换顺序：排队中 → 检测中 → 定格。
	for _, id := range []string{"item-a|a.test|192.0.2.1", "item-a|b.test|198.51.100.1"} {
		var got []CandidateStatus
		for _, p := range events {
			if p.CandidateID == id && p.Status != "" {
				got = append(got, p.Status)
			}
		}
		want := []CandidateStatus{StatusQueued, StatusProbing, StatusAvailable}
		if len(got) != len(want) {
			t.Fatalf("候选 %s 的状态转换 %v，期望 %v", id, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("候选 %s 第 %d 次转换是 %s，期望 %s", id, i+1, got[i], want[i])
			}
		}
	}

	// 探测带上主机名，否则证书没处验。
	for _, tgt := range s.probeTargets {
		if tgt.Hostname == "" {
			t.Errorf("探测 %s 没带主机名，证书校验无法进行", tgt.IP)
		}
	}
	// 事件里的行内容就是定格后的那一行。
	for _, p := range events {
		if p.Candidate != nil && p.Candidate.Status != p.Status {
			t.Errorf("事件 %d 的状态 %s 和行里的 %s 不一致", p.Sequence, p.Status, p.Candidate.Status)
		}
	}
}

func TestRunRowOrderIsStable(t *testing.T) {
	s := newStub()
	s.candidates["z.test"] = v4host(3)
	s.candidates["a.test"] = []strategies.Candidate{{IP: "203.0.113.9"}}

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-o", Hostnames: []string{"z.test", "a.test"}}, budget(3, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	want := []string{"192.0.2.1", "192.0.2.2", "192.0.2.3", "203.0.113.9"}
	for i, w := range want {
		if res.Candidates[i].IP != w {
			t.Fatalf("第 %d 行应是 %s，实得 %s（顺序漂了预算扣减就没有唯一答案）", i+1, w, res.Candidates[i].IP)
		}
	}
	if got := res.Candidates[0].ID; got != "item-o|z.test|192.0.2.1" {
		t.Errorf("候选标识应由项目、主机名、地址三段组成，实得 %q", got)
	}
}

// 只有「连上 + 证书对这个主机名有效」才算可用：只连上不算，因为规划要的是这个地址能替这个域名服务。
func TestRunRequiresCertificateMatchForAvailable(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = []strategies.Candidate{{IP: "192.0.2.1"}, {IP: "192.0.2.2"}}
	s.results["192.0.2.1"] = detector.Result{OK: true, Verified: false, LatencyMS: 3}
	s.results["192.0.2.2"] = available()

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-c", Hostnames: []string{"a.test"}}, budget(2, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if res.Candidates[0].Status != StatusUnavailable {
		t.Errorf("连上但没验证书不能算可用，实得 %s", res.Candidates[0].Status)
	}
	if !strings.Contains(res.Candidates[0].Reason, "证书") {
		t.Errorf("那句原因要说清是没验证书，实得 %q", res.Candidates[0].Reason)
	}
	if res.Candidates[1].Status != StatusAvailable || res.Available != 1 {
		t.Errorf("验过证书的那条应算可用，实得 %+v（可用数 %d）", res.Candidates[1], res.Available)
	}
}

// 探测层的每档结论都要有归属：超时和取消不能混成「不可用」，说不清的也不能替它编一个结论。
func TestRunMapsEveryProbeOutcome(t *testing.T) {
	cases := []struct {
		name string
		res  detector.Result
		err  error
		want CandidateStatus
	}{
		{"可用", available(), nil, StatusAvailable},
		{"超时", detector.Result{Kind: detector.FailTimeout, Detail: "连接与握手超过 2 秒预算"}, nil, StatusTimeout},
		{"取消", detector.Result{Kind: detector.FailCancelled, Detail: "探测已取消"}, nil, StatusCancelled},
		{"拒绝", refused(), nil, StatusUnavailable},
		{"不可达", detector.Result{Kind: detector.FailUnreachable, Detail: "到该地址的路走不通"}, nil, StatusUnavailable},
		{"握手不成", detector.Result{Kind: detector.FailHandshake, Detail: "TLS 握手没成功"}, nil, StatusUnavailable},
		{"证书不受信", detector.Result{Kind: detector.FailCertUntrusted, Detail: "证书链不受信任"}, nil, StatusUnavailable},
		{"证书不匹配", certMismatch(), nil, StatusUnavailable},
		{"证书过期", detector.Result{Kind: detector.FailCertExpired, Detail: "证书已过期或还没生效"}, nil, StatusUnavailable},
		{"其它失败", detector.Result{Kind: detector.FailOther, Detail: "探测失败"}, nil, StatusUnavailable},
		{"没给分类", detector.Result{}, nil, StatusUntested},
		{"目标不合法", detector.Result{}, detector.ErrBadTarget, StatusUntested},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStub()
			s.candidates["a.test"] = []strategies.Candidate{{IP: "192.0.2.1"}}
			s.results["192.0.2.1"] = tc.res
			s.probeErr["192.0.2.1"] = tc.err

			res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
				Input{ItemID: "item-k", Hostnames: []string{"a.test"}}, budget(1, 1, 10))
			if err != nil {
				t.Fatalf("跑不起来：%v", err)
			}
			row := res.Candidates[0]
			if row.Status != tc.want {
				t.Fatalf("状态 %s，期望 %s（%s）", row.Status, tc.want, row.Reason)
			}
			if !row.Status.final() {
				t.Errorf("收尾状态 %s 没定格，界面上那一行会一直转圈", row.Status)
			}
			if tc.want == StatusAvailable && row.Reason != "" {
				t.Errorf("可用不该带原因，实得 %q", row.Reason)
			}
			if tc.want == StatusUnavailable && row.Reason == "" {
				t.Error("不可用必须给一句原因，界面不能显示空白")
			}
		})
	}
}

func TestRunKeepsLatencyFromProbe(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = []strategies.Candidate{{IP: "192.0.2.1"}}
	s.results["192.0.2.1"] = certMismatch()

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-l", Hostnames: []string{"a.test"}}, budget(1, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if res.Candidates[0].LatencyMS != 11 || res.Candidates[0].TLSMS != 6 {
		t.Errorf("两段耗时都要留下，实得 %+v", res.Candidates[0])
	}
	if res.Candidates[0].StartedAt == "" || res.Candidates[0].FinishedAt == "" {
		t.Error("开始和结束时间都要记，界面才分得出这条真跑过")
	}
}

// 预算用完不是「全都不可用」：已经测出来的保留，没排上的明写未检测。
// 解析次数单独计数，因为一次解析可能带出好几个候选。
func TestRunStopsAtProbeBudgetAndMarksUntested(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(3)
	s.candidates["b.test"] = v4host(2)

	rec := &recorder{}
	res, err := NewRunner(s.lookup, s.probe, rec).Run(context.Background(),
		Input{ItemID: "item-b", Hostnames: []string{"a.test", "b.test"}}, budget(3, 1, 2))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if res.ProbesUsed != 2 {
		t.Errorf("实际启动的探测必须正好等于预算 2，实得 %d", res.ProbesUsed)
	}
	if res.DNSQueries != 2 {
		t.Errorf("两个主机名各解析一次，实得 %d", res.DNSQueries)
	}
	if got := res.Candidates[0].Status; got != StatusAvailable {
		t.Errorf("排上的第一条该保留结果，实得 %s", got)
	}
	for _, row := range res.Candidates[2:] {
		if row.Status != StatusUntested || !strings.Contains(row.Reason, "预算") {
			t.Errorf("没排上的候选要标未检测并说明预算用完，实得 %+v", row)
		}
		if row.StartedAt != "" {
			t.Errorf("没开始的候选不该有开始时间：%+v", row)
		}
	}
	// 收口那一步也必须逐条发事件：界面上没排上的候选要立刻变成「未检测」，不能停在排队中转圈。
	if got := finalStatusEvents(rec, StatusUntested); got != 3 {
		t.Errorf("3 条未检测候选该有 3 次定格事件，实得 %d", got)
	}
	assertLastEventComplete(t, rec, 5)

	var untested int
	for _, h := range res.Hostnames {
		untested += h.Untested
	}
	if untested != 3 {
		t.Errorf("按主机名归集的未检测数应为 3，实得 %d（%+v）", untested, res.Hostnames)
	}
	if res.Hostnames[0].Untested != 1 || res.Hostnames[1].Untested != 2 {
		t.Errorf("未检测要按主机名各自归集，实得 %+v", res.Hostnames)
	}
}

// 预算 0：一条都不探，但解析照旧发起——界面上要能列出「这些候选本轮没测」而不是什么都没有。
func TestRunZeroBudgetStillResolves(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(2)

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-z", Hostnames: []string{"a.test"}}, budget(2, 1, 0))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if _, probes, _ := s.counts(); probes != 0 {
		t.Errorf("预算 0 还发起了 %d 次探测", probes)
	}
	if res.DNSQueries != 1 || len(res.Candidates) != 2 {
		t.Errorf("解析要发起、候选要列出，实得解析 %d、候选 %d", res.DNSQueries, len(res.Candidates))
	}
	for _, row := range res.Candidates {
		if row.Status != StatusUntested {
			t.Errorf("预算 0 时所有候选都该是未检测，实得 %s", row.Status)
		}
	}
}

// 本轮期限到点：算「时间不够」，不算用户取消。已完成的保留，没开始的单独成档。
func TestRunDeadlineMarksNotStarted(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(3)
	s.probeDelay["192.0.2.1"] = 300 * time.Millisecond

	b := budget(3, 1, 10)
	b.deadline = 50 * time.Millisecond
	rec := &recorder{}
	res, err := NewRunner(s.lookup, s.probe, rec).Run(context.Background(),
		Input{ItemID: "item-d", Hostnames: []string{"a.test"}}, b)
	if err != nil {
		t.Fatalf("期限到点不是调用错误：%v", err)
	}
	if !res.DeadlineHit {
		t.Error("本轮期限到点要如实标出来")
	}
	if res.Cancelled {
		t.Error("期限到点不是用户取消，两档的界面话术不同")
	}
	if res.Candidates[0].Status != StatusTimeout {
		t.Errorf("正在探测的那条该按探测层的归因标成超时，实得 %s", res.Candidates[0].Status)
	}
	for _, row := range res.Candidates[1:] {
		if row.Status != StatusNotStarted || !strings.Contains(row.Reason, "期限") {
			t.Errorf("没排上的候选要标「因总时限未开始」并说清期限，实得 %+v", row)
		}
	}
	if got := finalStatusEvents(rec, StatusNotStarted); got != 2 {
		t.Errorf("2 条「因总时限未开始」该有 2 次定格事件，实得 %d", got)
	}
	assertLastEventComplete(t, rec, 3)
}

// 用户取消：整轮收口成已取消，已测出来的结果保留，不伪装成「因总时限未开始」。
func TestRunUserCancelKeepsDoneResults(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(4)
	s.probeDelay["192.0.2.2"] = 300 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// 等第一条测完、第二条已经在跑，再取消整轮。
		tick := time.NewTicker(time.Millisecond)
		defer tick.Stop()
		for range tick.C {
			if _, probes, _ := s.counts(); probes >= 2 {
				cancel()
				return
			}
		}
	}()

	rec := &recorder{}
	res, err := NewRunner(s.lookup, s.probe, rec).Run(ctx,
		Input{ItemID: "item-x", Hostnames: []string{"a.test"}}, budget(4, 1, 10))
	if err != nil {
		t.Fatalf("取消不是调用错误：%v", err)
	}
	cancel()
	if !res.Cancelled {
		t.Fatal("这一轮是被用户取消的，要如实标出来")
	}
	if res.DeadlineHit {
		t.Error("用户取消不该同时报「期限到点」")
	}
	if res.Candidates[0].Status != StatusAvailable {
		t.Errorf("取消前测完的那条要保留，实得 %s", res.Candidates[0].Status)
	}
	var cancelled int
	for _, row := range res.Candidates {
		if row.Status == StatusCancelled {
			cancelled++
		}
		if row.Status == StatusNotStarted || row.Status == StatusQueued {
			t.Errorf("用户取消的轮次里不该出现「因总时限未开始」或未定格行：%+v", row)
		}
	}
	if cancelled == 0 {
		t.Error("没开始的候选要标成已取消，一条都没有")
	}
	if got := finalStatusEvents(rec, StatusCancelled); got != cancelled {
		t.Errorf("取消的 %d 条候选都该有定格事件，实得 %d", cancelled, got)
	}
	assertLastEventComplete(t, rec, 4)
}

// 并发数是后端硬约束：活动请求峰值不许超过它，也不能一直是 1（那样档位等于没生效）。
func TestRunRespectsConcurrencyCeiling(t *testing.T) {
	for _, concurrency := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("并发%d", concurrency), func(t *testing.T) {
			s := newStub()
			s.candidates["a.test"] = v4host(6)
			for _, c := range s.candidates["a.test"] {
				s.probeDelay[c.IP] = 20 * time.Millisecond
			}
			b := budget(6, concurrency, 10)

			if _, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
				Input{ItemID: "item-p", Hostnames: []string{"a.test"}}, b); err != nil {
				t.Fatalf("跑不起来：%v", err)
			}
			_, probes, maxRunning := s.counts()
			if probes != 6 {
				t.Fatalf("6 条候选都该测到，实得 %d", probes)
			}
			if maxRunning > concurrency {
				t.Errorf("活动请求峰值 %d 超过并发数 %d", maxRunning, concurrency)
			}
			if concurrency > 1 && maxRunning < 2 {
				t.Errorf("并发数 %d 却一次只有一个活动请求，档位没生效（峰值 %d）", concurrency, maxRunning)
			}
		})
	}
}

// 一个域名查不到不该把整轮判死：其它主机名照旧检测，失败原因单独记在它那行。
func TestRunLookupFailureIsPerHostnameOnly(t *testing.T) {
	s := newStub()
	s.lookupErr["missing.test"] = &strategies.LookupError{Kind: strategies.KindNotFound, Detail: "域名不存在"}
	s.candidates["ok.test"] = v4host(2)

	rec := &recorder{}
	res, err := NewRunner(s.lookup, s.probe, rec).Run(context.Background(),
		Input{ItemID: "item-f", Hostnames: []string{"missing.test", "ok.test"}}, budget(2, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if res.Hostnames[0].Resolved || res.Hostnames[0].Detail != "域名不存在" {
		t.Errorf("解析失败要按主机名单独记账并给出原因，实得 %+v", res.Hostnames[0])
	}
	if res.Hostnames[1].Available != 2 || res.Available != 2 {
		t.Errorf("另一个主机名的两条候选不受影响，实得 %+v", res.Hostnames[1])
	}
	if len(res.Candidates) != 2 {
		t.Errorf("查不到的域名不该凭空造候选，实得 %d 条", len(res.Candidates))
	}
	// 解析阶段事件按主机名各发一次，界面才有一行「正在解析」。
	var resolving []string
	for _, p := range rec.all() {
		if p.Phase == PhaseResolving {
			resolving = append(resolving, p.Hostname)
		}
	}
	if len(resolving) != 2 {
		t.Errorf("两个主机名各一条解析事件，实得 %v", resolving)
	}
}

func TestRunUsesStrategyFailureTextWithoutLeakingName(t *testing.T) {
	s := newStub()
	s.lookupErr["secret.example"] = &strategies.LookupError{Kind: strategies.KindTimeout, Detail: "解析超过 2 秒预算"}
	s.candidates["ok.example"] = v4host(1)
	s.results["192.0.2.1"] = certMismatch()

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-s", Hostnames: []string{"secret.example", "ok.example"}}, budget(1, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	assertNoSensitive(t, res, "secret.example", "ok.example", "192.0.2.1")
	if res.Hostnames[0].Detail != "解析超过 2 秒预算" {
		t.Errorf("解析失败那句要用方法给的分类说明，实得 %q", res.Hostnames[0].Detail)
	}
}

// 意外错误（不是方法给的分类错误）也不能把原文带出去：Go 的 DNS 错误文本里有域名。
func TestRunUnexpectedLookupErrorBecomesFixedText(t *testing.T) {
	s := newStub()
	s.lookupErr["boom.test"] = errors.New("dial udp 8.8.8.8:53: lookup boom.test: no such host")

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-u", Hostnames: []string{"boom.test"}}, budget(1, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if res.Hostnames[0].Detail != "解析没能完成" {
		t.Errorf("说不清的解析失败要换成固定话，实得 %q", res.Hostnames[0].Detail)
	}
	assertNoSensitive(t, res, "boom.test", "8.8.8.8")
}

// 方法直接判「域名不合法」时不发查询，界面上这句要说清是没发起。
func TestRunBadInputFromStrategy(t *testing.T) {
	s := newStub()
	s.lookupErr["bad_input.test"] = fmt.Errorf("%w：域名里有非法字符", strategies.ErrBadInput)

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-n", Hostnames: []string{"bad_input.test"}}, budget(1, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if res.Hostnames[0].Detail != "主机名不合规则，没有发起解析" {
		t.Errorf("实得 %q", res.Hostnames[0].Detail)
	}
	if res.DNSQueries != 1 {
		t.Errorf("本包发起了一次解析请求（方法内部没发查询），实得 %d", res.DNSQueries)
	}
}

// 候选比档位上限多：截断标记必须带出来，否则界面会把「只有两个」和「被裁到两个」当成一回事。
func TestRunReportsTruncation(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(5)

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-t", Hostnames: []string{"a.test"}}, budget(2, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if !res.Truncated || !res.Hostnames[0].Truncated {
		t.Error("候选被裁到 2 个必须标出截断")
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("每主机名最多探 2 个，实得 %d", len(res.Candidates))
	}
	if s.hostnames()[0] != "a.test#2" {
		t.Errorf("传给方法的候选上限应是档位的 2，实得 %q", s.hostnames()[0])
	}
}

// 主机名先规范化再去重：同一个域名的三种写法只解析一次，空条目直接丢。
func TestRunNormalizesAndDedupesHostnames(t *testing.T) {
	s := newStub()
	s.candidates["github.com"] = v4host(1)

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-h", Hostnames: []string{"GitHub.COM.", " github.com ", "github.com", "", "   "}}, budget(1, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if got := s.hostnames(); len(got) != 1 || got[0] != "github.com#1" {
		t.Errorf("应当只对规范化后的 github.com 解析一次，实得 %v", got)
	}
	if len(res.Hostnames) != 1 || res.Hostnames[0].Hostname != "github.com" {
		t.Errorf("结论按规范化后的主机名给，实得 %+v", res.Hostnames)
	}
}

// 共用同一主机名的两个项目各占一行：候选标识必须带项目段，否则界面上两行会互相顶掉。
func TestCandidateIDSeparatesItemsAndSharedHostnames(t *testing.T) {
	if a, b := candidateID("github", "github.com", "1.2.3.4"), candidateID("api", "github.com", "1.2.3.4"); a == b {
		t.Errorf("不同项目共用同一主机名和地址必须是两行：%s 与 %s 相同", a, b)
	}
	if a, b := candidateID("github", "github.com", "1.2.3.4"), candidateID("github", "codeload.github.com", "1.2.3.4"); a == b {
		t.Errorf("同项目不同主机名的同一地址必须是两行：%s 与 %s 相同", a, b)
	}
}

func TestRunRejectsBadCallsWithoutTouchingNetwork(t *testing.T) {
	cases := []struct {
		name   string
		runner func(s *stub) *Runner
		in     Input
		budget Budget
	}{
		{"没有项目 ID", func(s *stub) *Runner { return NewRunner(s.lookup, s.probe, nil) }, Input{Hostnames: []string{"a.test"}}, budget(1, 1, 1)},
		{"一个主机名都没有", func(s *stub) *Runner { return NewRunner(s.lookup, s.probe, nil) }, Input{ItemID: "i", Hostnames: []string{" ", "."}}, budget(1, 1, 1)},
		{"没注入取候选的方法", func(s *stub) *Runner { return NewRunner(nil, s.probe, nil) }, Input{ItemID: "i", Hostnames: []string{"a.test"}}, budget(1, 1, 1)},
		{"并发越过硬上限", func(s *stub) *Runner { return NewRunner(s.lookup, s.probe, nil) }, Input{ItemID: "i", Hostnames: []string{"a.test"}}, budget(1, MaxConcurrency+1, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStub()
			s.candidates["a.test"] = v4host(1)
			if _, err := tc.runner(s).Run(context.Background(), tc.in, tc.budget); !errors.Is(err, ErrBadRun) && !errors.Is(err, ErrBadBudget) {
				t.Errorf("应当当场拒掉，实得 %v", err)
			}
			if lookups, probes, _ := s.counts(); lookups != 0 || probes != 0 {
				t.Errorf("被拒的调用一个请求都不该发出去，实得解析 %d、探测 %d", lookups, probes)
			}
		})
	}
}

// 没接界面时观察者给 nil 也要能跑完（队列接线前的过渡期就走这条路）。
func TestRunWithoutObserver(t *testing.T) {
	// 候选故意给一个不是 IP 字面量的地址：探测接缝给 nil 时用探测层的 HTTPS，
	// 它在解析之前就把这种地址拒了，所以这一步不会真去连任何东西。
	s := newStub()
	s.candidates["a.test"] = []strategies.Candidate{{IP: "not-an-ip"}}
	res, err := NewRunner(s.lookup, nil, nil).Run(context.Background(),
		Input{ItemID: "item-nil", Hostnames: []string{"a.test"}}, budget(2, 2, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("一条候选都要有结论，实得 %d", len(res.Candidates))
	}
	row := res.Candidates[0]
	if row.Status != StatusUntested || !strings.Contains(row.Reason, "探测层的规则") {
		t.Errorf("非 IP 字面量的候选该被探测层当场拒掉并标未检测，实得 %+v", row)
	}
}

// 已经写在系统里的地址照样列成候选、照样探测、照样占本轮名额：
// 界面上「现在用的那个地址灵不灵」永远有一行实测结论，不会因为 DNS 不再返回它就消失。
func TestRunIncludesAppliedIPsAsCandidates(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(2)
	rec := &recorder{}

	res, err := NewRunner(s.lookup, s.probe, rec).Run(context.Background(),
		Input{ItemID: "item-ap", Hostnames: []string{"a.test"}, Applied: map[string][]string{" A.TEST. ": {"192.0.2.9"}}},
		budget(3, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if len(res.Candidates) != 3 {
		t.Fatalf("已知地址 1 条 + 解析 2 条 = 3 行，实得 %d（%+v）", len(res.Candidates), res.Candidates)
	}
	first := res.Candidates[0]
	if first.IP != "192.0.2.9" || !first.Applied {
		t.Errorf("第一条该是那个已生效的地址并带上标记，实得 %+v", first)
	}
	if first.ID != "item-ap|a.test|192.0.2.9" {
		t.Errorf("已知地址的行标识同规则（项目|主机名|地址），实得 %q", first.ID)
	}
	for _, row := range res.Candidates[1:] {
		if row.Applied {
			t.Errorf("解析出来的候选不该带「当前生效」标记：%+v", row)
		}
	}
	if res.ProbesUsed != 3 {
		t.Errorf("3 条候选都该探到（含已生效那条），实得 %d", res.ProbesUsed)
	}
	var probed bool
	for _, ip := range s.probeCalls {
		if ip == "192.0.2.9" {
			probed = true
		}
	}
	if !probed {
		t.Error("已生效的地址没被探测，界面上那一行永远定格不了")
	}
	// 名额：已生效那条占掉一格，传给方法的候选上限相应少 1，别把整档名额吃光。
	if got := s.hostnames(); len(got) != 1 || got[0] != "a.test#2" {
		t.Errorf("传给方法的候选上限应是 3-1=2，实得 %v", got)
	}
	if ho := res.Hostnames[0]; ho.Candidates != 3 || ho.Available != 3 {
		t.Errorf("按主机名归集的计数要把这一行算进去，实得 %+v", ho)
	}
	// 已知地址在解析之前就列成排队中，界面立刻有这一行。
	if got := finalStatusEvents(rec, StatusQueued); got != 3 {
		t.Errorf("3 条候选该有 3 次排队中事件，实得 %d", got)
	}
}

// 解析又返回同一个地址时只留一行：两条同 IP 的候选在界面上会各自更新、互相顶不掉。
func TestRunDedupesAppliedAgainstLookup(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(3)

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-dd", Hostnames: []string{"a.test"},
			Applied: map[string][]string{"a.test": {" 192.0.2.2 ", "192.0.2.2"}}},
		budget(4, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	seen := map[string]int{}
	for _, row := range res.Candidates {
		seen[row.IP]++
	}
	for ip, n := range seen {
		if n != 1 {
			t.Errorf("地址 %s 出现 %d 行，同一个主机名下不该有重复行", ip, n)
		}
	}
	if len(res.Candidates) != 3 {
		t.Fatalf("重复地址收成一行后应是 3 行（192.0.2.2 + 192.0.2.1 + 192.0.2.3），实得 %+v", res.Candidates)
	}
	if !res.Candidates[0].Applied || res.Candidates[0].IP != "192.0.2.2" {
		t.Errorf("去重后保留的那一行要用已生效那条（带标记、写法已规范化），实得 %+v", res.Candidates[0])
	}
}

// 已生效的地址不许把新候选挤光：最多占档位名额里的 PerHostname-1 个，至少给解析结果留一格。
func TestRunAppliedSeedsLeaveRoomForLookup(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(2)

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-lim", Hostnames: []string{"a.test"},
			Applied: map[string][]string{"a.test": {"198.51.100.1", "198.51.100.2", "198.51.100.3"}},
		}, budget(2, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("每主机名最多 2 行，实得 %+v", res.Candidates)
	}
	var applied, fresh int
	for _, row := range res.Candidates {
		if row.Applied {
			applied++
		} else {
			fresh++
		}
	}
	if applied != 1 || fresh != 1 {
		t.Errorf("已生效的最多占 1 格、解析结果至少留 1 格，实得已生效 %d、解析 %d（%+v）", applied, fresh, res.Candidates)
	}
	if got := s.hostnames(); len(got) != 1 || got[0] != "a.test#1" {
		t.Errorf("名额被占掉 1 个后传给方法的上限应是 1，实得 %v", got)
	}
}

// 解析失败不该顺手把已知地址那行抹掉：这一行恰恰是「现在用的地址还灵不灵」唯一的答案。
func TestRunKeepsAppliedSeedWhenLookupFails(t *testing.T) {
	s := newStub()
	s.lookupErr["a.test"] = &strategies.LookupError{Kind: strategies.KindNotFound, Detail: "域名不存在"}

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-lf", Hostnames: []string{"a.test"},
			Applied: map[string][]string{"a.test": {"192.0.2.9"}}},
		budget(2, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if len(res.Candidates) != 1 || !res.Candidates[0].Applied {
		t.Fatalf("解析查不到也要保留已生效那一行，实得 %+v", res.Candidates)
	}
	if !res.Candidates[0].Status.final() {
		t.Errorf("这一行照样要定过格，实得 %s", res.Candidates[0].Status)
	}
	ho := res.Hostnames[0]
	if ho.Resolved || ho.Detail != "域名不存在" || ho.Candidates != 1 {
		t.Errorf("解析失败的说明照旧按主机名记，且候选数要把这一行算上，实得 %+v", ho)
	}
}

// 已知地址里的垃圾写法（不是 IP）丢掉：探测层连门都摸不到，列进去只会多一行永远失败的噪音。
// 本轮没查的主机名给的地址也不参与；v6 地址要带上 v6 标记。
func TestRunAppliedFiltersGarbageAndUnknownHostnames(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(1)

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-g", Hostnames: []string{"a.test"},
			Applied: map[string][]string{
				"a.test": {"not-an-ip", "  ", "2001:db8::1"},
				"z.test": {"198.51.100.7"},
			}}, budget(3, 1, 10))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	if len(res.Candidates) != 2 {
		t.Fatalf("非法地址丢掉、别的主机名的地址不掺进来，应剩 2 行，实得 %+v", res.Candidates)
	}
	if res.Candidates[0].IP != "2001:db8::1" || !res.Candidates[0].IPv6 || !res.Candidates[0].Applied {
		t.Errorf("v6 的已生效地址要带 v6 标记，实得 %+v", res.Candidates[0])
	}
	for _, row := range res.Candidates {
		if row.IP == "198.51.100.7" || row.IP == "not-an-ip" {
			t.Errorf("不该出现的候选行：%+v", row)
		}
	}
}

// 每主机名的计数：可用、未检测、候选数三样都要对得上，界面按主机名汇总显示。
func TestRunHostOutcomeCounters(t *testing.T) {
	s := newStub()
	s.candidates["a.test"] = v4host(3)
	s.results["192.0.2.2"] = refused()

	res, err := NewRunner(s.lookup, s.probe, nil).Run(context.Background(),
		Input{ItemID: "item-c2", Hostnames: []string{"a.test"}}, budget(3, 1, 3))
	if err != nil {
		t.Fatalf("跑不起来：%v", err)
	}
	ho := res.Hostnames[0]
	if ho.Candidates != 3 || ho.Available != 2 || ho.Untested != 0 {
		t.Errorf("计数应为候选 3、可用 2、未检测 0，实得 %+v", ho)
	}
	if res.Available != 2 {
		t.Errorf("整轮可用数应为 2，实得 %d", res.Available)
	}
	assertNoSensitive(t, res, "a.test", "192.0.2.2")
}

// assertNoSensitive 检查往界面上去的所有句子都不含被查主机名和候选地址。
// 隐私口径要求记录的是耗时与结论，不是查了谁。
func assertNoSensitive(t *testing.T, res *Result, secrets ...string) {
	t.Helper()
	check := func(where, text string) {
		for _, s := range secrets {
			if strings.Contains(strings.ToLower(text), strings.ToLower(s)) {
				t.Errorf("%s 泄漏了被查对象 %q：%q", where, s, text)
			}
		}
	}
	for _, h := range res.Hostnames {
		check("主机名 "+h.Hostname+" 的解析说明", h.Detail)
	}
	for _, c := range res.Candidates {
		check("候选 "+c.ID+" 的原因", c.Reason)
	}
}

// finalStatusEvents 数一数某个最终状态被逐条发过多少次事件。
// 规划要求每次候选状态转换立刻同步，所以「收口」那一步（未检测、未开始、已取消）
// 也必须逐条发，不能只写在结果里等整轮结束一次性画出来。
func finalStatusEvents(rec *recorder, status CandidateStatus) int {
	var n int
	for _, p := range rec.all() {
		if p.Status == status && p.CandidateID != "" {
			n++
		}
	}
	return n
}

// assertLastEventComplete 检查最后一条事件的完成数已经追上候选总数：
// 界面靠这一对数字判断「这一轮不再有行了」。
func assertLastEventComplete(t *testing.T, rec *recorder, wantTotal int) {
	t.Helper()
	events := rec.all()
	last := events[len(events)-1]
	if last.Done != wantTotal || last.Total != wantTotal {
		t.Errorf("最后一条事件应为完成 %d、总数 %d，实得 完成 %d、总数 %d", wantTotal, wantTotal, last.Done, last.Total)
	}
}
