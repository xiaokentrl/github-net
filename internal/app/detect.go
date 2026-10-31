package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/hosts"
	"github.com/xiaokentrl/github-net/internal/task"
)

// detectExecutor 是「一条检测任务真做起来是什么样」的那层适配：
// 把队列给的记录（项目、范围、方法）翻译成一轮检测的输入，再把一轮的状态变化折算回队列的进度和终态。
// 分工不停在这层：怎么取候选属加速方法，连不连得上属探测层，一轮怎么排属检测编排，本文件只管对齐三者与队列。
//
// 装配层在目录和设置都就绪时把它换进队列（替换 pendingExecutor），同轮接通排任务的入口
// DetectItems——这是「真实网络放行」那一步。缺哪样就继续用 pendingExecutor，不降级硬跑。
//
// 这一层不写 Hosts、不改系统任何状态，产出只有一条任务的终态、它的进度汇报，
// 以及交给出结果存储的这一轮候选结论（详情页从那里取，执行器自己不持有）。
type detectExecutor struct {
	// catalog 是项目与域名的唯一来源；构造本执行器时目录必须已经读出来。
	catalog *catalog.Catalog
	// settings 是设置来源。档位、每轮候选上限和「某个范围默认用哪个方法」都从这里现读，
	// 一轮开始后就冻结在这一轮的 Budget 里，中途改设置影响不到正在跑的这一轮。
	settings *config.Store
	// strategyFor 解析「方法 ID → 方法实现」。生产装配用包级注册表（strategies.Get）；
	// 留成一个字段是为了让测试用自己的表，而不是往全局注册表里塞假方法——
	// 同一个 ID 重复注册会把错误写进全局 Errors()，把装配层那道注册门照脏。
	strategyFor func(id string) (strategies.Strategy, bool)
	// probe 是探测接缝，nil 表示用探测层的 HTTPS 那条，也就是真实外网。
	probe accelerator.ProbeFunc
	// results 收每轮的候选结论；nil 表示没人要结果（纯测试场景），执行器照跑不存。
	results *detectResultStore
	// pub 把候选级状态变化原样发给前端；nil 表示不发（测试和没接总线的场景）。
	pub *event.DetectEmitter
	// gate 是跨任务共享的网络并发闸：多条检测并行时，全进程活动网络请求仍不过闸容量。
	gate chan struct{}
	// rounds 是批量检测共享预算的账本；nil 等价于「每任务不受轮预算约束」（旧测试路径）。
	rounds *roundBudgets
	// appliedIPs 读「这些主机名当前写在系统 Hosts 上的是哪些地址」，读不出来就给空。
	// 留成接缝是为了让测试自己喂一份已知地址，不去碰真文件；线上由 Service 用它那一份 hostsPath 实现。
	appliedIPs func(hostnames []string) map[string][]string
	// save 把跑完的一轮写进候选底账（SQLite）。留成接缝：nil 表示没有底账
	// （库打不开或纯测试场景），照跑不落盘。写失败也不许影响检测结果——底账是「发生过什么的记录」，不是检测结果本身。
	save func(taskRound, scopeID, strategyID string, r *accelerator.Result)
	// defaultFor 给「用户还没选过方法」的范围一个出厂默认（GitHub 范围 → 全部公共源，拍板 2026-10-31）。
	// 留成接缝：nil 表示没有出厂默认，没选过就明确拒绝，不从注册表里随便挑一个。
	defaultFor func(scopeID string) (string, bool)
}

// newDetectExecutor 建检测执行器。调用方必须在产品目录和设置服务都准备好之后再来建：
// 目录读不出来时任何检测都没有可查的项目，那种情况队列该继续用 pendingExecutor。
func newDetectExecutor(c *catalog.Catalog, st *config.Store, probe accelerator.ProbeFunc, results *detectResultStore, pub *event.DetectEmitter, gate chan struct{}, rounds *roundBudgets, appliedIPs func(hostnames []string) map[string][]string, save func(taskRound, scopeID, strategyID string, r *accelerator.Result), defaultFor func(scopeID string) (string, bool)) *detectExecutor {
	return &detectExecutor{catalog: c, settings: st, strategyFor: strategies.Get, probe: probe, results: results, pub: pub, gate: gate, rounds: rounds, appliedIPs: appliedIPs, save: save, defaultFor: defaultFor}
}

// appliedFor 取出这些主机名当前已经写在管理区段里的地址，作为本轮的已知地址交给检测编排。
// 区段读不到、或读到了但判读不了（结构有毛病、标记成对不上）都返回 nil：
// 那种时候「现在生效的是哪个地址」本身就没有可信答案，宁可少一行候选，
// 也不把判读不出来的东西当成已生效——后者会让一条错误的行带着「当前生效」的字出现在界面上。
func appliedFor(block hosts.Block, hostnames []string) map[string][]string {
	if !block.Usable() {
		return nil
	}
	want := make(map[string]bool, len(hostnames))
	for _, h := range hostnames {
		want[hosts.NormalizeHostname(h)] = true
	}
	out := make(map[string][]string)
	for _, rec := range block.Records {
		for _, host := range rec.Hostnames {
			if want[host] {
				out[host] = append(out[host], rec.IP)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// detectItem 是一个项目在目录里跟这一轮有关的两件事：能检测的域名和它所属的加速范围。
type detectItem struct {
	hostnames []string
	scopeID   string
}

// Execute 实现 task.Executor：跑一条检测任务，返回它的结论。
func (e *detectExecutor) Execute(ctx context.Context, t task.Task, report task.Report) task.Outcome {
	if t.Kind != task.KindDetect {
		// 应用和恢复要写系统 Hosts，属阶段四的受限辅助服务，这层一句都不做。
		return detectFailure("这一类任务还没有接上执行动作，这条任务没有开始")
	}

	item, ok := e.item(t.ItemID)
	if !ok {
		return detectFailure("产品目录里没有这个项目，这一轮没有可检测的域名")
	}
	if len(item.hostnames) == 0 {
		// 没有域名不是失败：这一轮本来就没有要做的东西。
		return task.Outcome{Status: task.StatusSkipped, Message: "这个项目没有配置域名，这一轮没有要检测的东西"}
	}
	cfg, err := e.settings.Snapshot()
	if err != nil {
		return detectFailure("读不到当前设置，这一轮没有开始")
	}
	strategyID, ok := e.strategyID(t.StrategyID, item.scopeID, cfg)
	if !ok {
		return detectFailure("这个项目所在的加速范围还没选检测方法，先在设置里为它选一个再检测")
	}
	strategy, ok := e.strategyFor(strategyID)
	if !ok {
		return detectFailure("之前选的那个检测方法已经不在注册表里，重新选一个方法再检测")
	}
	if !strategyApplies(strategy, item.scopeID) {
		return detectFailure("那个检测方法不适用于这个项目所在的加速范围，换一个方法再检测")
	}
	budget, err := accelerator.BudgetFor(cfg, len(item.hostnames))
	if err != nil {
		return detectFailure("这一轮的检测预算和当前设置对不上，这一轮没有开始")
	}
	// 从本轮批量动作的共享预算里取自己这份：规划说「单项、批量、服务范围共用预算执行路径」，
	// 预算是每轮一份，不是每任务一份——54 项共享 108 次，不是各 108 次。
	// 取到 0 说明预算被前面的项目用完了：明确标「预算不足，未检测」，不静默跳过。
	grant := len(item.hostnames) * budget.PerHostname
	if e.rounds != nil && t.RoundID != "" {
		grant = e.rounds.take(t.RoundID, grant)
		if grant == 0 {
			return task.Outcome{Status: task.StatusSkipped, Message: "预算不足，未检测；调高每轮候选上限或缩小范围后重试"}
		}
		if grant < budget.MaxProbes {
			budget.MaxProbes = grant
		}
	}

	// 一轮一个 Runner：观察者的落点是这一条任务的进度回调，挂在队列那个唯一执行者给的 report 上；
	// 候选级事件经 pub 原样转发给前端。两个出口都在检测那把锁内被调，顺序稳定。
	// 已知地址在这一轮开始那一刻现读一次：读到的后一行也算这一轮的候选，中途 Hosts 被别人改了不追。
	var applied map[string][]string
	if e.appliedIPs != nil {
		applied = e.appliedIPs(item.hostnames)
	}
	runner := accelerator.NewRunnerGated(strategy.Lookup, e.probe, &progressBridge{report: report, pub: e.pub}, e.gate)
	result, err := runner.Run(ctx, accelerator.Input{ItemID: t.ItemID, Hostnames: item.hostnames, Applied: applied}, budget)
	if err != nil {
		// 一轮没跑起来的错误都是「调用方给错了东西」那几类（预算与范围对不上、方法没实现），
		// 原始错误里没有用户能用来下一步的信息，也不保证不含域名，所以这里只给人话不给 err.Error()。
		return detectFailure("这一轮检测没能跑起来，检查设置后重试")
	}
	// 没用完的预算还给轮：后面的项目能多测几个。取消/期限到点同样只还实际没用到的。
	if e.rounds != nil && t.RoundID != "" {
		e.rounds.release(t.RoundID, grant-result.ProbesUsed)
	}
	// 跑完的结论交给结果存储，详情页从那里取。取消的那轮也存：已完成的那部分候选
	// 是真实测出来的东西，界面上该留着看。nil 结果（没跑起来）存储自己会拒收。
	e.results.put(item.scopeID, strategyID, result)
	// 同一笔结论抄进候选底账；底账写失败只少一行记录，不回改这一轮的结论。
	if e.save != nil {
		e.save(t.RoundID, item.scopeID, strategyID, result)
	}
	if result.Cancelled {
		// 队列在取消那一刻已经把这条定成「已取消」，这里 returned 的结论不会再改写它；
		// 给失败而不是成功，是为了让单独调用这一层（没有队列）的调用方拿到不撒谎的结论。
		return detectFailure("这一轮检测已取消")
	}
	// 没查出可用地址是这一轮的结论，不是失败：一轮跑完了就算完成。
	// 可用几条、谁没测上属候选结果，界面上单独那一栏看，不塞进任务的错误文案里。
	return task.Outcome{Status: task.StatusDone}
}

// item 按项目 ID 取域名与范围。域名在这里就规范化去重，因为预算要按「本轮真的会解析几个域名」算，
// 拿目录里的原始条数算会把同一个域名的三种写法当成三个，预算给多、界面显示的范围也给多。
func (e *detectExecutor) item(itemID string) (detectItem, bool) {
	for _, category := range e.catalog.Categories {
		for _, item := range category.Items {
			if item.ID == itemID {
				return detectItem{hostnames: uniqueHostnames(item.Hostnames), scopeID: category.ScopeID}, true
			}
		}
	}
	return detectItem{}, false
}

// strategyID 定这一轮用哪个加速方法：任务上带的优先，其次取这个范围的默认方法，
// 再其次取出厂默认（GitHub 范围 → 全部公共源）。三个都没有就明确拒绝，
// 绝不从注册表里随便挑一个——用哪个方法查候选是用户定的事。
func (e *detectExecutor) strategyID(fromTask, scopeID string, cfg config.Config) (string, bool) {
	if id := strings.TrimSpace(fromTask); id != "" {
		return id, true
	}
	if id := strings.TrimSpace(cfg.Accelerator.DefaultStrategyByScope[scopeID]); id != "" {
		return id, true
	}
	if e.defaultFor != nil {
		return e.defaultFor(scopeID)
	}
	return "", false
}

// uniqueHostnames 规范化再去重，保持目录里的顺序。规范化用读写两侧共用的那一份实现，
// 这样「同一个域名的三种写法」在预算、解析和写盘三处算出来是同一个东西。
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

// progressBridge 把一轮检测里每条候选的状态变化分成两路：
// 队列的 report 只要整轮进度（阶段、完成数），候选行自己的样子经 pub 原样发给前端。
// 它在检测那把锁内被调用、同一轮只有一个现场，所以这里不再自己加锁；
// 往下走是队列 reporter 的锁，两把锁的先后顺序在任何路径上都只有一边，不会互相等。
type progressBridge struct {
	report task.Report
	pub    *event.DetectEmitter
}

func (b *progressBridge) Observe(p accelerator.Progress) {
	b.pub.Publish(p)
	b.report(detectPhase(p), detectPhaseText(p), detectPercent(p))
}

// detectPhase 给出这一条事件属于哪一段。检测编排只在解析时带阶段标识，
// 候选行自己的事件不带，但候选都排在探测这一段里，所以按「有没有候选 ID」归段。
func detectPhase(p accelerator.Progress) string {
	if p.Phase != "" {
		return p.Phase
	}
	return accelerator.PhaseProbing
}

// detectPhaseText 是给界面直接显示的一句话。三条收口（预算用完、期限到点、用户取消）
// 在候选行上有各自的说明，任务这一行只说整轮进度，具体那一条为什么没测属候选结果那一栏。
func detectPhaseText(p accelerator.Progress) string {
	switch {
	case p.Phase == accelerator.PhaseResolving:
		return "正在解析域名"
	case p.Status == accelerator.StatusQueued:
		return "正在列出候选地址"
	default:
		return fmt.Sprintf("正在检测候选地址（已完成 %d/%d）", p.Done, p.Total)
	}
}

// detectPercent 只按「本轮已定格的候选条数」算，不猜速度、不给剩余时间。
// 解析阶段整轮总数还没数出来（Total 为 0），这一段就是 0，等有候选了才往上涨。
// 收口路径会把没开始的候选逐条定格并计入 Done，所以取消和期限到点同样收在 100。
func detectPercent(p accelerator.Progress) int {
	if p.Total <= 0 {
		return 0
	}
	return p.Done * 100 / p.Total
}

// strategyApplies 核对方法自己声明的适用范围。范围对不上就拒绝：
// 配置里存的方法可能来自别的范围，或者方法后来收窄了适用范围，
// 这两种情况都不能拿「它总归能查点东西」当理由照跑。
func strategyApplies(s strategies.Strategy, scopeID string) bool {
	for _, got := range s.ScopeIDs {
		if got == scopeID {
			return true
		}
	}
	return false
}

// detectFailure 给一条以失败收口的结论。这里的文案不含任何域名和候选地址：
// 任务的错误展示会进界面，域名和地址只在候选结果那一侧出现。
func detectFailure(message string) task.Outcome {
	return task.Outcome{Status: task.StatusFailed, Message: message}
}
