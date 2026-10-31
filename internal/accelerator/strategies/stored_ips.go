package strategies

import (
	"context"

	"github.com/xiaokentrl/github-net/internal/hosts"
)

// lookupStored 的 ctx 保留在签名里（Lookup 的形状统一），本方法不发起网络请求，用不上取消。

// 「使用已有储存」：候选不来自 DNS、也不来自拉源，而是上一轮检测存在本机候选库里的可用地址。
// 检测编排照常会把这些地址再探一遍——这一方法的用途是「不重新拉源，验证手里已有的 IP 还能不能用」。
//
// 储存的长相由装配层注入（StoredCandidates 接缝）：候选库在 Service 手里，
// 策略注册发生在 init，那一刻还没有库的实例，和 DNS 方法用 systemResolver 接缝是同一个思路。

// StrategyIDStoredIPs 是写进用户配置的稳定 ID，改名会让已保存的选择失效。
const StrategyIDStoredIPs = "stored-ips"

// StoredCandidates 是「主机名 → 已储存的可用候选」的接缝。装配层接上候选库；
// nil 表示没有储存（库没开成），这一方法会明确报错而不是静默给空。
var StoredCandidates func(hostname string) []Candidate

func init() {
	if err := Register(Strategy{
		ID:       StrategyIDStoredIPs,
		ScopeIDs: []string{scopeGitHub},
		Name:     "使用已有储存",
		Description: "用上一轮检测存在本机候选库里的可用地址作为候选，不重新拉取公共源；" +
			"检测时会对这些地址重新验一遍连通性。还没检测过或库里没有可用地址时报错。" + sourceCommonNote,
		Caps:   sourceCaps,
		Lookup: lookupStored,
	}); err != nil {
		recordInitError(err)
	}
}

func lookupStored(_ context.Context, in Input) (Output, error) {
	if err := checkSourceInput(in); err != nil {
		return Output{}, err
	}
	if StoredCandidates == nil {
		return Output{}, &LookupError{Kind: KindOther, Detail: "候选库还没接上，先用「全部公共源」检测一轮"}
	}
	rows := StoredCandidates(hosts.NormalizeHostname(in.Hostname))
	if len(rows) == 0 {
		return Output{}, &LookupError{Kind: KindNoRecords, Detail: "储存里还没有这个域名的可用地址，先用「全部公共源」检测一轮"}
	}
	out := Output{Candidates: rows}
	return capCandidates(out, in.Max), nil
}
