package strategies

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/hosts"
)

// StrategyIDDNSHosts 是写进用户配置的稳定 ID，改名会让已保存的选择失效。
const StrategyIDDNSHosts = "dns-hosts"

// dnsBudget 是总纲定的解析预算：一次 DNS 查询最多 2 秒。
const dnsBudget = 2 * time.Second

// 域名形状的 RFC 1035 上限，超了就不必发查询。
const (
	maxNameLength  = 253
	maxLabelLength = 63
)

// Resolver 是解析动作的接缝：线上用 net.DefaultResolver（也就是系统配置的 DNS），
// 测试换成假的，才能在不碰网络的前提下跑超时、取消和各类错误分支。
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

var systemResolver Resolver = net.DefaultResolver

func init() {
	scopes, err := catalogScopeIDs()
	if err != nil {
		// 目录读不出来就没有「适用范围」可声明，宁可不注册，也不写一份范围不明的方法。
		recordInitError(fmt.Errorf("加速方法 %s 未注册：产品目录读不出来，%w", StrategyIDDNSHosts, err))
		return
	}
	if err := Register(Strategy{
		ID:       StrategyIDDNSHosts,
		ScopeIDs: scopes,
		Name:     "自动查找可用 IP",
		Description: "用系统配置的 DNS 解析域名取候选地址。查询不转发给任何第三方解析器，" +
			"也不记录完整查询域名；候选只用于展示，不会自动写进系统。",
		Caps:   CapabilityCandidateLookup | CapabilityKeepsTLSHostname | CapabilitySystemDNSOnly,
		Lookup: newDNSLookup(systemResolver, dnsBudget).run,
	}); err != nil {
		recordInitError(err)
	}
}

// catalogScopeIDs 把产品目录里的全部加速范围取成排序后的切片。
// 范围列表唯一来源是目录，这里不抄一份，以后目录加分类也不用改策略文件。
func catalogScopeIDs() ([]string, error) {
	cat, err := catalog.Load()
	if err != nil {
		return nil, err
	}
	set := cat.ScopeIDs()
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

type dnsLookup struct {
	res    Resolver
	budget time.Duration
}

func newDNSLookup(res Resolver, budget time.Duration) *dnsLookup {
	return &dnsLookup{res: res, budget: budget}
}

// run 取一个主机名的候选地址。只读，不写 Hosts、不改系统任何状态。
func (d *dnsLookup) run(ctx context.Context, in Input) (Output, error) {
	hostname := hosts.NormalizeHostname(in.Hostname)
	if hostname == "" {
		return Output{}, fmt.Errorf("%w：主机名为空", ErrBadInput)
	}
	if in.Max <= 0 {
		return Output{}, fmt.Errorf("%w：候选上限必须大于 0，收到 %d", ErrBadInput, in.Max)
	}
	if err := checkNameShape(hostname); err != nil {
		return Output{}, err
	}

	queryCtx, cancel, budgetFired := d.withBudget(ctx)
	defer cancel()

	addrs, err := d.res.LookupIPAddr(queryCtx, hostname)
	if err != nil {
		return Output{}, d.classify(err, budgetFired(), hostname)
	}
	candidates := dedupeAndSort(addrs)
	if len(candidates) == 0 {
		return Output{}, &LookupError{Kind: KindNoRecords, Detail: "解析有回话，但没有地址记录"}
	}
	out := Output{Candidates: candidates}
	if len(candidates) > in.Max {
		out.Candidates = candidates[:in.Max]
		out.Truncated = true
	}
	return out, nil
}

// withBudget 只在「上层没给期限」或「给的期限比解析预算长」时套一层自己的闹钟。
// budgetFired 用来分清超时是谁造成的：自己的闹钟响了就是解析超时，
// 上层的期限到了就归到取消/超时本身，不重复计时。
func (d *dnsLookup) withBudget(ctx context.Context) (context.Context, context.CancelFunc, func() bool) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= d.budget {
		return ctx, func() {}, func() bool { return false }
	}
	queryCtx, cancel := context.WithTimeout(ctx, d.budget)
	return queryCtx, cancel, func() bool { return errors.Is(queryCtx.Err(), context.DeadlineExceeded) }
}

// checkNameShape 拦掉根本不可能是合法域名的输入，省掉一次无用的查询。
// 用户自填的域名就是这里挡的：带端口、带空格、下划线开头的标签都不发查询。
func checkNameShape(hostname string) error {
	if len(hostname) > maxNameLength {
		return &LookupError{Kind: KindInvalidName, Detail: fmt.Sprintf("域名长度 %d 超过上限 %d", len(hostname), maxNameLength)}
	}
	for _, label := range strings.Split(hostname, ".") {
		if label == "" {
			return &LookupError{Kind: KindInvalidName, Detail: "域名里有空标签"}
		}
		if len(label) > maxLabelLength {
			return &LookupError{Kind: KindInvalidName, Detail: fmt.Sprintf("单个标签长度 %d 超过上限 %d", len(label), maxLabelLength)}
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return &LookupError{Kind: KindInvalidName, Detail: "域名标签不能以短横线开头或结尾"}
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			default:
				return &LookupError{Kind: KindInvalidName, Detail: "域名里有非法字符"}
			}
		}
	}
	return nil
}

// LookupErrorKind 是给上层映射稳定错误码用的分类，界面按它取文案，不解析句子。
type LookupErrorKind string

const (
	KindTimeout       LookupErrorKind = "timeout"
	KindCancelled     LookupErrorKind = "cancelled"
	KindNotFound      LookupErrorKind = "notFound"
	KindNoRecords     LookupErrorKind = "noRecords"
	KindServerFailure LookupErrorKind = "serverFailure"
	KindInvalidName   LookupErrorKind = "invalidName"
	KindOther         LookupErrorKind = "other"
)

// LookupError 是解析失败的对外形状。
//
// 这里刻意不保存原始 error：Go 的 *net.DNSError 打印出来会带上被查的域名，
// 而总纲第 4 条要求不记录完整查询域名。所以只留分类和一句不含域名的说明，
// Detail 进构造函数前再过一道 safeDetail，把万一混进来的域名抹掉。
type LookupError struct {
	Kind   LookupErrorKind
	Detail string
}

func (e *LookupError) Error() string {
	return fmt.Sprintf("dns_%s: %s", e.Kind, e.Detail)
}

func (d *dnsLookup) classify(err error, budgetFired bool, hostname string) error {
	kind := KindOther
	detail := "解析失败"

	switch {
	case budgetFired:
		kind = KindTimeout
		detail = fmt.Sprintf("解析超过 %d 秒预算", int(d.budget/time.Second))
	case errors.Is(err, context.Canceled):
		kind = KindCancelled
		detail = "查询已取消"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		kind = KindTimeout
		detail = "解析超时"
	}

	var dnsErr *net.DNSError
	if kind == KindOther && errors.As(err, &dnsErr) {
		switch {
		case dnsErr.IsNotFound:
			kind = KindNotFound
			detail = "域名不存在"
		case strings.Contains(dnsErr.Err, "timeout"):
			kind = KindTimeout
			detail = "解析器没有按时回答"
		default:
			kind = KindServerFailure
			detail = safeDetail(dnsErr.Err, hostname)
		}
	}

	return &LookupError{Kind: kind, Detail: detail}
}

// safeDetail 保证往界面和日志里去的句子不会出现被查域名。
func safeDetail(text, hostname string) string {
	if hostname == "" {
		return text
	}
	return strings.ReplaceAll(strings.ToLower(text), strings.ToLower(hostname), "该域名")
}

// dedupeAndSort 把解析结果收成可比较的候选列表：
// 同一地址只留一条（IPv4 映射地址和它的 v6 写法算同一条）、IPv4 排在前、同族按字节升序，
// 并且去掉 IPv6 的 zone——带 zone 的地址没法进 Hosts。
func dedupeAndSort(addrs []net.IPAddr) []Candidate {
	type row struct {
		c    Candidate
		addr []byte
	}
	seen := map[string]bool{}
	rows := make([]row, 0, len(addrs))
	for _, a := range addrs {
		var c Candidate
		var addr []byte
		if v4 := a.IP.To4(); v4 != nil {
			c, addr = Candidate{IP: v4.String()}, v4
		} else if v6 := a.IP.To16(); v6 != nil {
			c, addr = Candidate{IP: v6.String(), IPv6: true}, v6
		} else {
			continue
		}
		if seen[c.IP] {
			continue
		}
		seen[c.IP] = true
		rows = append(rows, row{c: c, addr: addr})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].c.IPv6 != rows[j].c.IPv6 {
			return !rows[i].c.IPv6
		}
		return bytes.Compare(rows[i].addr, rows[j].addr) < 0
	})
	out := make([]Candidate, len(rows))
	for i, r := range rows {
		out[i] = r.c
	}
	return out
}
