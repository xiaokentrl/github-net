package app

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/xiaokentrl/github-net/internal/hosts"
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
	// results 是检测结果存储：应用前从「最近一轮」取可用候选。
	results *detectResultStore
}

func newDispatchExecutor(detect task.Executor, priv privilege.Caller, results *detectResultStore) *dispatchExecutor {
	return &dispatchExecutor{detect: detect, priv: priv, results: results}
}

func (e *dispatchExecutor) Execute(ctx context.Context, t task.Task, report task.Report) task.Outcome {
	switch t.Kind {
	case task.KindDetect:
		if e.detect == nil {
			return task.Outcome{Status: task.StatusFailed, Message: pendingExecutorMessage}
		}
		return e.detect.Execute(ctx, t, report)
	case task.KindApply:
		return e.apply(t, report)
	case task.KindRestore:
		return e.restore(t, report)
	default:
		return task.Outcome{Status: task.StatusFailed, Message: "这一类任务还没有接上执行动作，这条任务没有开始"}
	}
}

// apply 把一个项目最近一轮的可用候选写进 Hosts。
// 选址规则：每个主机名取延迟最低的可用候选（并列时 IPv4 优先，写 Hosts 的兼容面更大）。
// 应用前在这里再验一遍「有结果、有可用候选」——入口（ApplyItems）验的是形状，这里验的是内容。
func (e *dispatchExecutor) apply(t task.Task, report task.Report) task.Outcome {
	if e.priv == nil {
		return task.Outcome{Status: task.StatusFailed, Message: pendingExecutorMessage}
	}
	view, ok := e.results.view(t.ItemID)
	if !ok {
		return task.Outcome{Status: task.StatusFailed, Message: "这个项目还没有检测结果，先检测再应用"}
	}
	mappings := bestMappings(view)
	if len(mappings) == 0 {
		return task.Outcome{Status: task.StatusFailed, Message: "最近一轮检测没有可用的候选地址，先重新检测"}
	}
	report("apply", "正在写入系统 Hosts", 0)
	if err := e.priv.Apply(t.ItemID, mappings); err != nil {
		return task.Outcome{Status: task.StatusFailed, Message: privilegeErrorText(err)}
	}
	return task.Outcome{Status: task.StatusDone}
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

// bestMappings 从一轮结果里给每个主机名挑一个「最好用」的可用候选并按 IP 分组。
// 没有可用候选的主机名不进映射：写一个连不上的 IP 比不写更糟。
func bestMappings(view DetectResultView) []hosts.Mapping {
	type pick struct {
		ip      string
		latency int64
		ipv6    bool
	}
	best := make(map[string]pick)
	for _, h := range view.Hosts {
		for _, c := range h.Candidates {
			if c.Status != "available" {
				continue
			}
			current, ok := best[c.Hostname]
			better := !ok || c.LatencyMS < current.latency || (c.LatencyMS == current.latency && current.ipv6 && !c.IPv6)
			if better {
				best[c.Hostname] = pick{ip: c.IP, latency: c.LatencyMS, ipv6: c.IPv6}
			}
		}
	}
	if len(best) == 0 {
		return nil
	}
	byIP := make(map[string][]string)
	for hostname, p := range best {
		byIP[p.ip] = append(byIP[p.ip], hostname)
	}
	ips := make([]string, 0, len(byIP))
	for ip := range byIP {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	out := make([]hosts.Mapping, 0, len(ips))
	for _, ip := range ips {
		names := byIP[ip]
		sort.Strings(names)
		out = append(out, hosts.Mapping{IP: ip, Hostnames: names})
	}
	return out
}
