package app

import (
	"context"
	"errors"
	"strings"

	"github.com/xiaokentrl/github-net/internal/privilege"
	"github.com/xiaokentrl/github-net/internal/task"
)

// dispatchExecutor 是队列的统一执行入口：按任务类型把活分给对应执行器。
// 队列只认一个 Executor，而三类任务的「做事」分属三处（检测在加速编排，应用/恢复在特权服务），
// 这里就是那个分拨台。谁没接上（nil）就给可见的拒答，绝不 panic。
type dispatchExecutor struct {
	detect task.Executor
	// priv 是特权服务客户端接缝；nil 表示这一层还没接（测试场景），
	// 应用/恢复任务会以「失败」落进队列被看见。
	priv privilege.Caller
	// results 是检测结果存储：应用/还原收尾时按 Hosts 现状重标「当前生效」。
	results *detectResultStore
	// appliedIPs 读「这些主机名当前写在系统 Hosts 上的是哪些地址」；应用/还原收尾时
	// 用它把候选的「当前生效」标记重标一遍，详情页的「当前写入 IP」才有实时事实。
	appliedIPs func(hostnames []string) map[string][]string
	// usablePicks 和入口的 applyBlocker 同源：内存优先、候选池兜底的候选地址，
	// 每条带「结论新不新」。挑地址只有这一处，执行器不再自己写第二套标准。
	usablePicks func(itemID string) []usablePick
	// verify 是写盘前那道复验：现场把不新鲜的候选探一遍，验通才准写。
	verify candidateVerifier
}

// candidateVerifier 是写盘前那道复验的接缝：给一批候选地址，返回当场验通的那几条，
// 并把每条结论（通与不通）回写到它来的地方（内存那一轮 + 候选池）。
// 线上传 Service.verifyCandidates；测试传假件，才能不碰外网跑遍通、不通和取消三种分支。
type candidateVerifier func(ctx context.Context, itemID string, picks []usablePick) []usablePick

func newDispatchExecutor(detect task.Executor, priv privilege.Caller, results *detectResultStore,
	appliedIPs func(hostnames []string) map[string][]string,
	usablePicks func(itemID string) []usablePick, verify candidateVerifier) *dispatchExecutor {
	return &dispatchExecutor{detect: detect, priv: priv, results: results, appliedIPs: appliedIPs, usablePicks: usablePicks, verify: verify}
}

func (e *dispatchExecutor) Execute(ctx context.Context, t task.Task, report task.Report) task.Outcome {
	switch t.Kind {
	case task.KindDetect:
		if e.detect == nil {
			return task.Outcome{Status: task.StatusFailed, Message: pendingExecutorMessage}
		}
		return e.detect.Execute(ctx, t, report)
	case task.KindApply:
		return e.apply(ctx, t, report)
	case task.KindRestore:
		return e.restore(t, report)
	default:
		return task.Outcome{Status: task.StatusFailed, Message: "这一类任务还没有接上执行动作，这条任务没有开始"}
	}
}

// apply 把一个项目的候选地址写进 Hosts。
// 选址来源和入口的 applyBlocker 同一条（内存那一轮优先，重启后退回候选池），
// 「检测一次、重启也能应用」就是这么来的。
// 但挑中的地址不直接写：结论放超过 applyFreshWindow 的那些，先现场复验一遍，
// 验通的才交给特权服务（拍板 2026-11-06：永远不把不能正常加速的地址写进系统 Hosts）。
// 一个都没验通就一个字不写，任务以失败落进队列——「应用成功」从此只可能是「当场验过能加速」。
func (e *dispatchExecutor) apply(ctx context.Context, t task.Task, report task.Report) task.Outcome {
	if e.priv == nil {
		return task.Outcome{Status: task.StatusFailed, Message: pendingExecutorMessage}
	}
	if e.usablePicks == nil {
		return task.Outcome{Status: task.StatusFailed, Message: "挑地址那一层还没接上，这一条没有开始"}
	}
	picks := e.usablePicks(t.ItemID)
	if len(picks) == 0 {
		return task.Outcome{Status: task.StatusFailed, Message: "内存和候选缓存里都没有可用的地址，先检测再应用"}
	}
	writable := picks
	stale := false
	for _, p := range picks {
		if !p.fresh {
			stale = true
			break
		}
	}
	if stale {
		report("verify", "正在复验候选地址", 0)
		if e.verify != nil {
			writable = e.verify(ctx, t.ItemID, picks)
		} else {
			// 复验这层没接上（纯测试场景）：不敢拿放旧的结论写盘，只放过窗口内的那几条。
			writable = freshOnly(picks)
		}
	}
	if len(writable) == 0 {
		return task.Outcome{Status: task.StatusFailed, Message: "候选地址现在都连不通，先重新检测再应用"}
	}
	mappings := mappingsOf(writable)
	report("apply", "正在写入系统 Hosts", 0)
	if err := e.priv.Apply(t.ItemID, mappings); err != nil {
		return task.Outcome{Status: task.StatusFailed, Message: privilegeErrorText(err)}
	}
	// 写完把「当前生效」标记按 Hosts 现状重标一遍：检测那轮的标记是写入前的旧事实。
	hostnames := make([]string, 0, len(writable))
	for _, p := range writable {
		hostnames = append(hostnames, p.hostname)
	}
	e.markApplied(t.ItemID, hostnames)
	return task.Outcome{Status: task.StatusDone}
}

// freshOnly 是复验没接上时的兜底：只放过窗口内的结论，过期的整条丢掉。
// 宁可少写几个域名，也不替一个说不清多旧的地址作保。
func freshOnly(picks []usablePick) []usablePick {
	out := make([]usablePick, 0, len(picks))
	for _, p := range picks {
		if p.fresh {
			out = append(out, p)
		}
	}
	return out
}

// markApplied 用 appliedIPs 接缝现读 Hosts，重标该项目候选的「当前生效」；
// 接缝没接（纯测试场景）就跳过。全部还原不走队列，它的标记由 RestoreAllHosts 统一清掉。
func (e *dispatchExecutor) markApplied(itemID string, hostnames []string) {
	if e.appliedIPs == nil {
		return
	}
	e.results.markApplied(itemID, e.appliedIPs(hostnames))
}

// restore 把一个项目退回它应用前的映射；写系统的事在特权服务那边，这里只管叫它做并翻译失败。
func (e *dispatchExecutor) restore(t task.Task, report task.Report) task.Outcome {
	if e.priv == nil {
		return task.Outcome{Status: task.StatusFailed, Message: pendingExecutorMessage}
	}
	report("restore", "正在还原系统 Hosts", 0)
	if err := e.priv.RestoreItem(t.ItemID); err != nil {
		return task.Outcome{Status: task.StatusFailed, Message: privilegeErrorText(err)}
	}
	// 还原完清掉「当前生效」标记：这些地址已经不在 Hosts 上了。
	view, ok := e.results.view(t.ItemID)
	if ok {
		hostnames := make([]string, 0, len(view.Hosts))
		for _, h := range view.Hosts {
			hostnames = append(hostnames, h.Hostname)
		}
		e.markApplied(t.ItemID, hostnames)
	}
	return task.Outcome{Status: task.StatusRestored}
}

// privilegeErrorText 把特权层的三类失败翻译成任务行上的一句话；
// 服务拒绝时带上服务给的可展示原因（写失败、元数据损坏等都在那边重写成过人话）。
func privilegeErrorText(err error) string {
	switch {
	case errors.Is(err, privilege.ErrHelperUnavailable):
		return "特权辅助服务没在运行，无法修改系统 Hosts；重新安装或修复本应用可以把它带起来"
	case errors.Is(err, privilege.ErrHelperRefused):
		return "特权辅助服务拒绝了这次操作：" + unwrapMessage(err)
	case errors.Is(err, privilege.ErrProtocol):
		return "和特权辅助服务的通信对不上版本，重装本应用可以修复"
	default:
		return "修改系统 Hosts 失败：" + unwrapMessage(err)
	}
}

// unwrapMessage 取错误链里最上层那句人话。服务拒绝的错误形如「ErrHelperRefused: 原因」，
// 界面只要原因那截。
func unwrapMessage(err error) string {
	text := err.Error()
	if idx := strings.Index(text, ": "); idx >= 0 {
		return text[idx+2:]
	}
	return text
}
