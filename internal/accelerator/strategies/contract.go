// Package strategies 是加速方法的自注册表：每个方法在自己的文件里声明
// 稳定 ID、适用的加速范围、面向用户的文案、能力和实现，通过 init 登记进来。
//
// 这里只放「怎么取得候选结果」，不放任务队列、不写 Hosts、也不碰系统状态——
// 检测和写盘的分工按规划文档：本包只产出候选，Hosts 写入属后面的特权服务阶段。
package strategies

import (
	"context"
	"fmt"
)

// Capability 描述一个方法能做到什么。装配层用它判断「这个方法够不够格进检测」，
// 而不是靠方法名字猜。
type Capability uint32

const (
	// CapabilityCandidateLookup：能给出域名对应的候选 IP。
	CapabilityCandidateLookup Capability = 1 << iota
	// CapabilityKeepsTLSHostname：候选结果要连同目标 hostname 一起交给后续校验，
	// 不能只留一个裸 IP（否则 TLS 证书没法验证）。
	CapabilityKeepsTLSHostname
	// CapabilitySystemDNSOnly：查询只走系统配置的解析器，绝不自动发给第三方 DNS。
	// 这条是总纲第 4 条 DNS 隐私规则的代码化，注册表要求首期方法必须带上它。
	CapabilitySystemDNSOnly
)

const knownCapabilities = CapabilityCandidateLookup | CapabilityKeepsTLSHostname | CapabilitySystemDNSOnly

func (c Capability) has(want Capability) bool { return c&want == want }

// Input 是一次候选取得请求。Hostname 是已经规范化的单个主机名，
// Max 是这一轮允许返回的候选条数上限（来自档位设置），必须大于 0。
type Input struct {
	Hostname string
	Max      int
}

// Candidate 是一条候选结果。IP 是不带 zone 的纯地址，可以直接进 Hosts 管理区段。
type Candidate struct {
	IP   string
	IPv6 bool
}

// Output 是一次检测的候选集合。Truncated 为真表示地址其实不止这些，
// 是 Max 把它们裁掉了——界面必须能区分「只有两个候选」和「被裁到两个」。
type Output struct {
	Candidates []Candidate
	Truncated  bool
}

// Strategy 是一个加速方法的描述符。字段对外可读，实现在 Lookup 里。
type Strategy struct {
	// ID 是写进用户配置的稳定标识，改名会让已保存的选择失效，所以不许动。
	ID string
	// ScopeIDs 是它适用的加速范围，取值必须来自产品目录的 scopeID。
	ScopeIDs []string
	// Name 和 Description 是面向用户的文案。
	Name        string
	Description string
	Caps        Capability
	Lookup      func(ctx context.Context, in Input) (Output, error)
}

// ErrBadInput 表示调用方给的主机名或上限本身不合法，不是查询失败。
// 单独一个哨兵错误，是为了让上层能把它归成「不用重试」的那一类。
var ErrBadInput = fmt.Errorf("输入不合法")
