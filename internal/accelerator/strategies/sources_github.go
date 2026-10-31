package strategies

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/xiaokentrl/github-net/internal/hosts"
	github "github.com/xiaokentrl/github-net/internal/sources/github"
)

// GitHub 公共源方法：候选不来自 DNS 解析，而来自三份社区维护的公共 hosts 源
// （定义和拉取逻辑在 internal/sources/github）。拉源是对源站的普通 HTTPS GET
// （几十 KB 文本），不是 DNS 查询，流量开销已拍板接受——所以这些方法不带
// CapabilitySystemDNSOnly：它们根本不碰解析器，也用不上那条承诺。
//
// 分工：sources/github 管取数和整理，本文件只管接成方法、登记进注册表。
// 「全部」是 GitHub 范围的默认方法（拍板 2026-10-31）：三份源并行取，
// 合并去重后按源优先序给出候选。
const (
	StrategyIDGitCDN      = "gitcdn"
	StrategyIDHelloGitHub = "hellogithub"
	StrategyIDGitLab      = "gitlab"
	StrategyIDAllSources  = "all-sources"
)

// scopeGitHub 是这组方法唯一的适用范围：公共 hosts 源的内容就是 GitHub 的地址，
// 别的范围（Steam）里没有对应条目，注册时就不声明。
const scopeGitHub = "github"

const sourceCommonNote = "候选只用于展示，不会自动写进系统。"

var sourceCaps = CapabilityCandidateLookup | CapabilityKeepsTLSHostname

func init() {
	for _, s := range []Strategy{
		{
			ID: StrategyIDGitCDN, ScopeIDs: []string{scopeGitHub},
			Name:        github.GitCDN.Name,
			Description: "从 hosts.gitcdn.top 拉取公共 hosts 映射，按域名取候选地址。" + sourceCommonNote,
			Caps:        sourceCaps,
			Lookup:      lookupOne(github.GitCDN),
		},
		{
			ID: StrategyIDHelloGitHub, ScopeIDs: []string{scopeGitHub},
			Name:        github.HelloGitHub.Name,
			Description: "从 raw.hellogithub.com 拉取公共 hosts 映射，按域名取候选地址。" + sourceCommonNote,
			Caps:        sourceCaps,
			Lookup:      lookupOne(github.HelloGitHub),
		},
		{
			ID: StrategyIDGitLab, ScopeIDs: []string{scopeGitHub},
			Name:        github.GitLab.Name,
			Description: "从 GitLab 上 ineo6 维护的 hosts 仓库拉取映射，按域名取候选地址。" + sourceCommonNote,
			Caps:        sourceCaps,
			Lookup:      lookupOne(github.GitLab),
		},
		{
			ID: StrategyIDAllSources, ScopeIDs: []string{scopeGitHub},
			Name:        "全部公共源",
			Description: "同时取三份公共 hosts 源，合并去重后按源顺序给出候选，是 GitHub 范围的默认方法。" + sourceCommonNote,
			Caps:        sourceCaps,
			Lookup:      lookupAll(),
		},
	} {
		if err := Register(s); err != nil {
			recordInitError(err)
		}
	}
}

// lookupOne 从单一公共源里取一个主机名的候选。
func lookupOne(src github.Source) func(context.Context, Input) (Output, error) {
	return func(ctx context.Context, in Input) (Output, error) {
		if err := checkSourceInput(in); err != nil {
			return Output{}, err
		}
		data, err := github.Default.Hosts(ctx, src)
		if err != nil {
			return Output{}, sourceFailure(ctx, err)
		}
		rows := data[hosts.NormalizeHostname(in.Hostname)]
		if len(rows) == 0 {
			return Output{}, &LookupError{Kind: KindNoRecords, Detail: "公共源里没有这个域名的条目"}
		}
		return capCandidates(entriesToOutput(rows), in.Max), nil
	}
}

// lookupAll 把三份源并行取齐、合并后取一个主机名的候选。
// 个别源拉砸了不挡合并：手头有几份用几份；三份全砸才报错，
// 和「不搞自动回退链」的拍板一致——合并本身已经是最宽的取法。
func lookupAll() func(context.Context, Input) (Output, error) {
	return func(ctx context.Context, in Input) (Output, error) {
		if err := checkSourceInput(in); err != nil {
			return Output{}, err
		}
		sources := github.All()
		results := make([]map[string][]github.Entry, len(sources))
		var wg sync.WaitGroup
		for i, src := range sources {
			wg.Add(1)
			go func(i int, src github.Source) {
				defer wg.Done()
				if data, err := github.Default.Hosts(ctx, src); err == nil {
					results[i] = data
				}
			}(i, src)
		}
		wg.Wait()

		merged := github.Merge(results...)
		rows := merged[hosts.NormalizeHostname(in.Hostname)]
		if len(rows) > 0 {
			return capCandidates(entriesToOutput(rows), in.Max), nil
		}
		// 一条都没有，分两种说：源全没取回来是「取不到」，取回来了但没这个域名是「没有条目」。
		for _, got := range results {
			if got != nil {
				return Output{}, &LookupError{Kind: KindNoRecords, Detail: "公共源里没有这个域名的条目"}
			}
		}
		return Output{}, &LookupError{Kind: KindOther, Detail: "三份公共源都没取回来，稍后再试"}
	}
}

// checkSourceInput 把输入形状挡在进源数据之前，口径和 DNS 方法一致。
func checkSourceInput(in Input) error {
	if hosts.NormalizeHostname(in.Hostname) == "" {
		return fmt.Errorf("%w：主机名为空", ErrBadInput)
	}
	if in.Max <= 0 {
		return fmt.Errorf("%w：候选上限必须大于 0，收到 %d", ErrBadInput, in.Max)
	}
	return nil
}

func entriesToOutput(rows []github.Entry) Output {
	out := Output{Candidates: make([]Candidate, 0, len(rows))}
	for _, e := range rows {
		out.Candidates = append(out.Candidates, Candidate{IP: e.IP, IPv6: e.IPv6})
	}
	return out
}

// capCandidates 按 Max 裁候选并如实标记「其实不止这些」。
func capCandidates(out Output, max int) Output {
	if len(out.Candidates) > max {
		out.Candidates = out.Candidates[:max]
		out.Truncated = true
	}
	return out
}

// sourceFailure 把拉源失败归成注册表那套稳定错误分类。
// 错误文案只说源的状态，不带域名——任务文案会进界面。
func sourceFailure(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return &LookupError{Kind: KindCancelled, Detail: "拉取公共源已取消"}
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return &LookupError{Kind: KindTimeout, Detail: "拉取公共源超时"}
	default:
		return &LookupError{Kind: KindOther, Detail: "公共源暂时取不回来，稍后再试"}
	}
}
