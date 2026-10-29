package accelerator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/detector"
	"github.com/xiaokentrl/github-net/internal/hosts"
)

// CandidateStatus 是一条候选在这一轮里的状态。八档是规划给的最小集合加两档：
// 「因总时限未开始」和「预算用完没排上」必须分开，因为一个是时间不够、一个是用户主动把上限调小了，
// 界面上给用户的解释和下一步动作完全不同。
type CandidateStatus string

const (
	StatusQueued      CandidateStatus = "queued"      // 排队中：候选已列出，还没轮到
	StatusProbing     CandidateStatus = "probing"     // 检测中
	StatusAvailable   CandidateStatus = "available"   // 可用：连上了且证书对这个主机名有效
	StatusUnavailable CandidateStatus = "unavailable" // 不可用：连不上、握手不成或证书不对
	StatusTimeout     CandidateStatus = "timeout"     // 超时：探测自己或本轮期限到点
	StatusCancelled   CandidateStatus = "cancelled"   // 已取消：用户取消了这一轮
	StatusNotStarted  CandidateStatus = "notStarted"  // 因总时限未开始：本轮期限到点时还没轮到它
	StatusUntested    CandidateStatus = "untested"    // 未检测：本轮候选探测预算用完，没排上
)

// final 报告这个状态是不是定格了。定格之后不会再被改写，界面上那一行不用再等更新。
func (s CandidateStatus) final() bool {
	switch s {
	case StatusAvailable, StatusUnavailable, StatusTimeout, StatusCancelled, StatusNotStarted, StatusUntested:
		return true
	}
	return false
}

// 阶段标识：解析和探测两段各一个，界面用它决定那一行现在说什么话。
const (
	PhaseResolving = "resolving" // 正在解析
	PhaseProbing   = "probing"   // 正在探测这个候选地址
)

// Candidate 是一条候选地址的行。ID 在本轮内稳定，界面按它逐行更新，不会出现「同一条候选两个身份」。
// Hostname 和 IP 是给界面显示用的；往日志和错误文案里去的那句话一律不含它们（见 Reason 的注释）。
type Candidate struct {
	ID         string          `json:"id"`
	Hostname   string          `json:"hostname"`
	IP         string          `json:"ip"`
	IPv6       bool            `json:"ipv6"`
	Status     CandidateStatus `json:"status"`
	LatencyMS  int64           `json:"latencyMs"`
	TLSMS      int64           `json:"tlsMs"`
	Reason     string          `json:"reason,omitempty"` // 一句人话；探测层保证不含主机名和地址
	StartedAt  string          `json:"startedAt,omitempty"`
	FinishedAt string          `json:"finishedAt,omitempty"`
}

// HostOutcome 是一个主机名在本轮的结果。解析失败只影响它自己那几个候选，
// 同项目其它主机名照旧检测——一个域名查不到不能把整轮判死。
type HostOutcome struct {
	Hostname   string `json:"hostname"`
	Resolved   bool   `json:"resolved"`
	Detail     string `json:"detail,omitempty"` // 解析失败那句说明，不含主机名
	Truncated  bool   `json:"truncated,omitempty"`
	Candidates int    `json:"candidates"`
	Available  int    `json:"available"`
	Untested   int    `json:"untested"`
}

// Progress 是一次状态转换的内容：这一行现在是什么状态，加上整轮的计数。
// 序号在本轮内从 1 开始单调递增，界面按它丢重复和乱序。
//
// 本轮序号是本包自己的计数器，和任务队列那条 `task:progress` 的序号不是同一个：
// 前者描述的是一条候选行的变化，后者描述的是任务记录的变化。接线到界面时要把这件事说清楚，
// 不能拿候选事件的序号去判任务快照新旧。
type Progress struct {
	Sequence    int64           `json:"sequence"`
	ItemID      string          `json:"itemId"`
	Hostname    string          `json:"hostname,omitempty"`
	CandidateID string          `json:"candidateId,omitempty"`
	Phase       string          `json:"phase,omitempty"`
	Status      CandidateStatus `json:"status,omitempty"`
	Done        int             `json:"done"`
	Total       int             `json:"total"`
	Active      int             `json:"active"`
	ProbesUsed  int             `json:"probesUsed"`
	DNSQueries  int             `json:"dnsQueries"`
	Candidate   *Candidate      `json:"candidate,omitempty"`
}

// Observer 收状态转换。实现必须能并发调用，且不许回头调本包的方法（事件在锁内投递，
// 和任务队列同样的取舍：换来序号严格同序，慢订阅者会拖慢本轮）。给 nil 就是不发事件。
type Observer interface {
	Observe(p Progress)
}

// LookupFunc 是取候选的接缝：线上传加速方法的 Lookup，测试换成假的。
type LookupFunc func(ctx context.Context, in strategies.Input) (strategies.Output, error)

// ProbeFunc 是探测接缝：线上用 detector.ProbeHTTPS，测试换成假的，
// 才能在不碰外网的前提下跑遍可用、超时、拒绝和各类证书失败。
type ProbeFunc func(ctx context.Context, t detector.Target) (detector.Result, error)

// Input 是一轮检测的输入。Hostnames 可以重复、可以带大写和尾随点，
// 本包先规范化再去重，同一轮里一个主机名只解析一次。
type Input struct {
	ItemID    string
	Hostnames []string
}

// Result 是一轮检测的结论。数组字段宁可给空切片也不给 nil，前端直接读不用判空。
type Result struct {
	ItemID     string
	Hostnames  []HostOutcome
	Candidates []Candidate
	// ProbesUsed 是实际启动的候选探测次数，也是预算扣掉的量。
	ProbesUsed int
	// DNSQueries 是本包发起的解析次数，和探测次数分开算：一次解析可能带出多个候选。
	DNSQueries  int
	Available   int
	Truncated   bool
	DeadlineHit bool // 本轮 5 秒期限到点：已完成的保留，没开始的标成因总时限未开始
	Cancelled   bool // 用户取消这一轮：已完成的保留，其余标成已取消
	StartedAt   time.Time
	FinishedAt  time.Time
}

// ErrBadRun 表示这一轮根本没跑起来的调用错误（缺项目 ID、没有可检测的主机名、
// 没注入取候选的方法、预算参数不合法）。检测没查出可用地址不是错误，是结论。
var ErrBadRun = errors.New("检测请求不合法")

// Runner 把「取候选」和「验候选」串成一轮检测。它不持有状态，可以同时跑多个项目，
// 但同一时刻的网络请求数由每轮的预算并发数管着，跨轮共用一个总闸属接线那一步。
type Runner struct {
	lookup LookupFunc
	probe  ProbeFunc
	obs    Observer
	now    func() time.Time
	// gate 是跨任务共享的并发闸（nil = 不设闸）：每次真正的 DNS 查询和候选探测
	// 都要先拿到闸里的一个位。多条检测任务并行时，全进程同时活动的网络请求
	// 也不过闸的容量——「全任务最多 8 个活动请求」的硬上限在网络上收口，而不是在任务数上。
	gate chan struct{}
}

// NewGate 建一个容量为 n 的共享并发闸。n 是进程级网络并发的硬上限。
func NewGate(n int) chan struct{} {
	if n < 1 {
		n = 1
	}
	return make(chan struct{}, n)
}

// acquire 拿闸里的一个位，返回放回函数。gate 为 nil 时空操作。
func (r *Runner) acquire() func() {
	if r.gate == nil {
		return func() {}
	}
	r.gate <- struct{}{}
	return func() { <-r.gate }
}

// NewRunner 建一个执行器。lookup 必须给（取候选的方式由用户选的方法决定）；
// probe 给 nil 就用探测层的 HTTPS 那条，这是规划里唯一的可用判据。
func NewRunner(lookup LookupFunc, probe ProbeFunc, obs Observer) *Runner {
	return NewRunnerGated(lookup, probe, obs, nil)
}

// NewRunnerGated 同 NewRunner，另接受一个跨任务共享的并发闸；测试和单任务场景传 nil。
func NewRunnerGated(lookup LookupFunc, probe ProbeFunc, obs Observer, gate chan struct{}) *Runner {
	if probe == nil {
		probe = detector.ProbeHTTPS
	}
	return &Runner{lookup: lookup, probe: probe, obs: obs, now: time.Now, gate: gate}
}

// Run 跑一轮检测：一个项目、若干个主机名、按传入的预算执行。
//
// 两段执行：先并发解析（解析完成立刻把候选列成排队中），全部解析结束后再并发探测。
// 分两段有两个理由：一是规划把预算定义成「按实际启动的候选探测扣减」，
// 只有候选全部列出后再按稳定顺序扣，「谁没排上」才有唯一答案；
// 二是解析和探测不重叠，同一时刻的活动请求就不会超过并发数。
//
// ctx 是这一轮的控制信号：取消它表示用户取消。本轮的 5 秒期限在这里自己加上，
// 服务级扫描的 30 秒总期限由调用方放在这个 ctx 上。
// 返回的 Result 在取消和超时时照样带已完成的结果，不会只给一个错误把已测出来的东西丢掉。
func (r *Runner) Run(ctx context.Context, in Input, budget Budget) (*Result, error) {
	if err := budget.validate(); err != nil {
		return nil, err
	}
	if r.lookup == nil {
		return nil, fmt.Errorf("%w：没有注入取候选的方法", ErrBadRun)
	}
	itemID := strings.TrimSpace(in.ItemID)
	if itemID == "" {
		return nil, fmt.Errorf("%w：缺少项目 ID", ErrBadRun)
	}
	hostnames := uniqueHostnames(in.Hostnames)
	if len(hostnames) == 0 {
		return nil, fmt.Errorf("%w：项目 %s 本轮没有可检测的主机名", ErrBadRun, itemID)
	}

	runCtx, cancel := context.WithTimeout(ctx, budget.effectiveDeadline())
	defer cancel()

	d := &detection{
		runner:    r,
		parent:    ctx,
		runCtx:    runCtx,
		itemID:    itemID,
		budget:    budget,
		hostnames: hostnames,
		started:   r.now().UTC(),
	}
	d.hosts = make([]*HostOutcome, len(hostnames))
	d.hostRows = make([][]*Candidate, len(hostnames))
	d.resolveAll()
	d.assembleRows()
	d.probeAll()
	return d.result(), nil
}

// detection 是一轮检测的执行现场。计数器都在一把锁里改，事件也在同一把锁内投递，
// 这样「序号」和「那一行当时的样子」不会各走各的。
type detection struct {
	runner    *Runner
	parent    context.Context
	runCtx    context.Context
	itemID    string
	budget    Budget
	hostnames []string
	started   time.Time

	mu         sync.Mutex
	seq        int
	done       int
	total      int
	active     int
	probes     int
	dnsQueries int

	hosts    []*HostOutcome
	hostRows [][]*Candidate
	rows     []*Candidate
	rowHost  []int
	next     int
}

// resolveAll 并发解析所有主机名，工作协程按顺序领任务，
// 所以并发数为 1 时解析顺序就是输入顺序（测试靠这一点拿到确定的事件序列）。
func (d *detection) resolveAll() {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < d.budget.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				d.resolveOne(i)
			}
		}()
	}
	for i := range d.hostnames {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func (d *detection) resolveOne(i int) {
	hostname := d.hostnames[i]

	d.mu.Lock()
	ho := &HostOutcome{Hostname: hostname}
	d.hosts[i] = ho
	d.dnsQueries++
	d.emitLocked(ho, nil, PhaseResolving, "")
	d.mu.Unlock()

	release := d.runner.acquire()
	out, err := d.runner.lookup(d.runCtx, strategies.Input{Hostname: hostname, Max: d.budget.PerHostname})
	release()

	d.mu.Lock()
	defer d.mu.Unlock()
	if err != nil {
		ho.Detail = lookupFailureText(err)
		return
	}
	ho.Resolved = true
	ho.Truncated = out.Truncated
	rows := make([]*Candidate, 0, len(out.Candidates))
	for _, c := range out.Candidates {
		row := &Candidate{
			ID:       candidateID(d.itemID, hostname, c.IP),
			Hostname: hostname,
			IP:       c.IP,
			IPv6:     c.IPv6,
			Status:   StatusQueued,
		}
		rows = append(rows, row)
		// 解析一出结果就立刻把候选列成「排队中」，不等整批解析完再一次性画出来。
		d.emitLocked(ho, row, "", StatusQueued)
	}
	d.hostRows[i] = rows
}

// assembleRows 把各主机名的候选按主机名顺序拼成一条稳定的待探测队列。
// 拼完之后 total 才是最终值——解析期间界面上那个「完成数/总数」的总数会随候选列出而增长，
// 所以进度只能在这一阶段之后当真。
func (d *detection) assembleRows() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.total = 0
	for i, rows := range d.hostRows {
		for _, row := range rows {
			d.rows = append(d.rows, row)
			d.rowHost = append(d.rowHost, i)
		}
	}
	d.total = len(d.rows)
}

// probeAll 按稳定顺序并发探测。每个主机名单独失败不影响其它候选：
// 一个地址连不上不该让整轮判死，规划要的是逐行定格。
func (d *detection) probeAll() {
	var wg sync.WaitGroup
	for w := 0; w < d.budget.Concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				row, hostIdx, ok := d.claim()
				if !ok {
					return
				}
				release := d.runner.acquire()
				res, err := d.runner.probe(d.runCtx, detector.Target{IP: row.IP, Hostname: row.Hostname})
				release()
				d.settle(row, hostIdx, res, err)
			}
		}()
	}
	wg.Wait()
}

// claim 领下一条候选并把它的状态推成「检测中」。返回 false 表示这一轮到此为止：
// 三种收口各有各自的原因，界面上必须分开显示，不能都写成「检测失败」。
func (d *detection) claim() (*Candidate, int, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.next >= len(d.rows) {
		return nil, 0, false
	}
	switch {
	case d.parent.Err() != nil:
		d.closeRemainingLocked(StatusCancelled, "这一轮已取消")
		return nil, 0, false
	case d.runCtx.Err() != nil:
		d.closeRemainingLocked(StatusNotStarted, fmt.Sprintf("本轮期限（%s）已到，这一条还没排上", deadlineText(d.budget.effectiveDeadline())))
		return nil, 0, false
	case d.probes >= d.budget.MaxProbes:
		d.closeRemainingLocked(StatusUntested, "本轮候选探测预算用完，这一条没有检测")
		return nil, 0, false
	}
	idx := d.next
	row := d.rows[idx]
	d.next++
	d.probes++
	row.Status = StatusProbing
	row.StartedAt = d.stampLocked()
	row.Reason = ""
	d.active++
	d.emitLocked(d.hosts[d.rowHost[idx]], row, PhaseProbing, StatusProbing)
	return row, d.rowHost[idx], true
}

// settle 定格一条候选的结论。每主机名的计数在汇总那一步统一算，这里不重复记，
// 否则一个「可用」会被加成两条。
func (d *detection) settle(row *Candidate, hostIdx int, res detector.Result, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.active--
	d.done++
	row.FinishedAt = d.stampLocked()
	row.LatencyMS = res.LatencyMS
	row.TLSMS = res.TLSMS
	if err != nil {
		// 探测层把「连不上」当结论返回，error 只有一种：目标本身不合法。
		// 这不是候选不可用，是这一条压根没测成，标未检测而不是给它一个假的探测结论。
		row.Status = StatusUntested
		row.Reason = "候选地址不符合探测层的规则，这一条没有检测"
	} else {
		row.Status, row.Reason = verdict(res)
	}
	d.emitLocked(d.hosts[hostIdx], row, "", row.Status)
}

// verdict 把探测结果换成候选状态。唯一的「可用」判据是连上并且证书对这个主机名有效：
// 只连上没验证书的候选不能算可用，否则就是把「IP 通着」当成「这个地址能替这个域名服务」。
func verdict(res detector.Result) (CandidateStatus, string) {
	switch {
	case res.OK && res.Verified:
		return StatusAvailable, ""
	case res.OK:
		return StatusUnavailable, "连上了，但没有通过该主机名的证书校验，不算可用"
	case res.Kind == detector.FailTimeout:
		return StatusTimeout, res.Detail
	case res.Kind == detector.FailCancelled:
		return StatusCancelled, res.Detail
	case res.Kind == "":
		// 探测层契约：有结论必有分类。走到这里说明注入的探测器给了个说不清的结果，
		// 宁可标未检测，也不替它编一个「不可用」。
		return StatusUntested, "探测没有给出结论，这一条按未检测处理"
	default:
		return StatusUnavailable, res.Detail
	}
}

// closeRemainingLocked 把还没开始的候选一次性定格，并交出这一轮的剩余队列。
// 假定调用方已持锁。
func (d *detection) closeRemainingLocked(status CandidateStatus, reason string) {
	for ; d.next < len(d.rows); d.next++ {
		row := d.rows[d.next]
		row.Status = status
		row.Reason = reason
		row.FinishedAt = d.stampLocked()
		d.done++
		d.emitLocked(d.hosts[d.rowHost[d.next]], row, "", status)
	}
}

// result 汇总本轮结论。取消和期限到点是两回事：期限到点是「时间不够」，
// 用户取消是「不做了」，界面上的话和后续动作都不一样。
func (d *detection) result() *Result {
	d.mu.Lock()
	defer d.mu.Unlock()

	out := &Result{
		ItemID:     d.itemID,
		Hostnames:  make([]HostOutcome, 0, len(d.hosts)),
		Candidates: make([]Candidate, 0, len(d.rows)),
		ProbesUsed: d.probes,
		DNSQueries: d.dnsQueries,
		StartedAt:  d.started,
		FinishedAt: d.runner.now().UTC(),
	}
	// 先按候选行汇总每主机名的计数，再拷出去——反过来会先拷走一份还没计数的副本。
	for idx, row := range d.rows {
		ho := d.hosts[d.rowHost[idx]]
		switch row.Status {
		case StatusAvailable:
			ho.Available++
			out.Available++
		case StatusUntested:
			ho.Untested++
		}
		out.Candidates = append(out.Candidates, *row)
	}
	for i, ho := range d.hosts {
		ho.Candidates = len(d.hostRows[i])
		out.Truncated = out.Truncated || ho.Truncated
		out.Hostnames = append(out.Hostnames, *ho)
	}
	out.Cancelled = d.parent.Err() != nil
	out.DeadlineHit = !out.Cancelled && d.runCtx.Err() != nil
	return out
}

// stampLocked 给出当前时刻的字符串。假定调用方已持锁。
func (d *detection) stampLocked() string {
	return d.runner.now().UTC().Format(time.RFC3339)
}

// emitLocked 占用本轮序号并投递一次状态转换。假定调用方已持锁。
// 观察者为 nil 时什么都不做——还没接界面的阶段和测试都走这条路。
func (d *detection) emitLocked(ho *HostOutcome, row *Candidate, phase string, status CandidateStatus) {
	if d.runner.obs == nil {
		return
	}
	d.seq++
	p := Progress{
		Sequence:   int64(d.seq),
		ItemID:     d.itemID,
		Phase:      phase,
		Status:     status,
		Done:       d.done,
		Total:      d.total,
		Active:     d.active,
		ProbesUsed: d.probes,
		DNSQueries: d.dnsQueries,
	}
	if row != nil {
		p.CandidateID = row.ID
		p.Hostname = row.Hostname
		snapshot := *row
		p.Candidate = &snapshot
	} else if ho != nil {
		p.Hostname = ho.Hostname
	}
	d.runner.obs.Observe(p)
}

// candidateID 三段都必要：不同项目可以共用同一个主机名和地址，界面上它们是各自行；
// 同一项目里不同主机名也可能共用一个地址（实测目录里有三组共用主机名）。
func candidateID(itemID, hostname, ip string) string {
	return itemID + "|" + hostname + "|" + ip
}

// uniqueHostnames 规范化再去重，保持输入顺序。
// 合法性判定留给加速方法（它会返回「域名不合法」且根本不发查询），这里不重复一套判定。
func uniqueHostnames(raw []string) []string {
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, h := range raw {
		n := hosts.NormalizeHostname(h)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

// lookupFailureText 把解析失败换成一句说明。
// 加速方法的 LookupError 本身就不含查询域名，这里也不碰 err.Error()——
// Go 的 *net.DNSError 打出来带域名，一旦拼进来这句就可能泄漏。
func lookupFailureText(err error) string {
	var le *strategies.LookupError
	if errors.As(err, &le) {
		return le.Detail
	}
	switch {
	case errors.Is(err, strategies.ErrBadInput):
		return "主机名不合规则，没有发起解析"
	case errors.Is(err, context.Canceled):
		return "解析已取消"
	case errors.Is(err, context.DeadlineExceeded):
		return "解析没赶上本轮期限"
	default:
		return "解析没能完成"
	}
}

// deadlineText 把期限说成人话：5 秒、1 秒 500 毫秒这类，界面直接印。
func deadlineText(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%d 毫秒", d/time.Millisecond)
	}
	if d.Round(time.Second) == d {
		return fmt.Sprintf("%d 秒", int(d/time.Second))
	}
	return fmt.Sprintf("%.1f 秒", d.Seconds())
}
