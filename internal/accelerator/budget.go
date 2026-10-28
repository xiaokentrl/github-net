// Package accelerator 把一次检测串成一轮：先按档位算清「这一轮最多试多少个候选」，
// 再取候选、逐个验连通性和证书，并把每一条的状态变化立刻报出去。
//
// 分工按规划划清，别在本包里做别人的事：
// 「怎么取候选」属加速方法（internal/accelerator/strategies）；
// 「连不连得上、证书对不对」属探测层（internal/detector）；
// 本包只管编排——预算怎么扣、同一时刻开几路、超时算谁的、界面上那一行现在是什么状态。
// 本包不写 Hosts、不改系统任何状态，也还没有任何生产代码引用它：
// 真实网络放行的那一步才把它接进任务队列和界面。
package accelerator

import (
	"errors"
	"fmt"
	"time"

	"github.com/xiaokentrl/github-net/internal/config"
)

// MaxConcurrency 是规划定的安全硬上限：同一时刻最多 8 个网络请求。
// 提高并发只加大瞬时带宽和同时连接压力，换不来更多候选覆盖，所以任何档位都不许越过它。
const MaxConcurrency = 8

// ItemDeadline 是单项检测的整体期限：解析、候选探测和结果整理都算在这 5 秒里。
// 到点不是失败，而是「已完成的保留、没开始的标成因总时限未开始」。
const ItemDeadline = 5 * time.Second

// ErrBadBudget 表示这一轮的预算参数本身不合法（档位不认识、上限不在允许范围）。
// 这跟「检测没查出结果」是两回事：后者是本轮的结论，前者是调用方给错了东西。
var ErrBadBudget = errors.New("检测预算不合法")

// Budget 是冻结下来的一轮预算。任务开始时就定死，中途改设置不影响正在跑的这一轮。
type Budget struct {
	// Profile 是档位标识，界面据此显示「省流/标准/完整检测」。
	Profile string
	// PerHostname 是每个主机名最多试几个候选 IP，来自档位。
	PerHostname int
	// Concurrency 是同一时刻最多几个网络请求，来自档位，且不越过 MaxConcurrency。
	Concurrency int
	// MaxProbes 是本轮实际可用的候选探测次数：min(Needed, 用户设的总上限)。
	// 只按实际启动的探测扣减，解析次数单独算（见 Result.DNSQueries）。
	MaxProbes int
	// Needed 是「盖住本轮范围理论上要多少次」= 唯一主机名数 × 每主机名候选上限。
	Needed int
	// Short 为真表示用户设的总上限低于 Needed，这一轮盖不住全部范围。
	// 按规划，这种情况启动前要把差额显示出来并请求确认；确认后没排上的候选标成未检测，
	// 绝不静默跳过。本包只算出并如实标出，弹窗确认属界面那一步。
	Short bool

	// deadline 是本轮的期限，0 表示用规划定的 ItemDeadline。
	// 不导出是不想让任何人从配置或接口上调它：5 秒是规划口径，不是用户选项。
	// 同包的测试把它换小，才能在不真等 5 秒的前提下跑「因总时限未开始」那一条。
	deadline time.Duration
}

// effectiveDeadline 返回这一轮实际用的期限。
func (b Budget) effectiveDeadline() time.Duration {
	if b.deadline > 0 {
		return b.deadline
	}
	return ItemDeadline
}

// BudgetFor 按当前设置和本轮范围算预算。
// hostnameCount 必须是本轮去重后的唯一主机名数——用菜单项目数会算错，
// 因为一个项目可以有多个主机名，不同项目也能共用同一个主机名。
func BudgetFor(cfg config.Config, hostnameCount int) (Budget, error) {
	if hostnameCount < 0 {
		return Budget{}, fmt.Errorf("%w：本轮主机名数量不能是负数，收到 %d", ErrBadBudget, hostnameCount)
	}
	perHostname, ok := config.ProfileCandidateLimit(cfg.Network.Profile)
	if !ok {
		return Budget{}, fmt.Errorf("%w：不认识的网络档位 %q", ErrBadBudget, cfg.Network.Profile)
	}
	concurrency, ok := config.ProfileConcurrency(cfg.Network.Profile)
	if !ok {
		return Budget{}, fmt.Errorf("%w：网络档位 %q 没有对应的并发数", ErrBadBudget, cfg.Network.Profile)
	}
	userCap := cfg.Network.MaxCandidateProbesPerRun
	if userCap < config.MinCandidateProbes || userCap > config.MaxCandidateProbesLimit {
		return Budget{}, fmt.Errorf("%w：每轮候选探测上限 %d 不在 %d 到 %d",
			ErrBadBudget, userCap, config.MinCandidateProbes, config.MaxCandidateProbesLimit)
	}

	// 并发数的硬上限只在 validate 里查一次：BudgetFor 末尾本来就走那道门，
	// 在这里再写一遍是重复防御，改坏它没有任何测试能发现（实测过）。
	b := Budget{
		Profile:     cfg.Network.Profile,
		PerHostname: perHostname,
		Concurrency: concurrency,
		Needed:      hostnameCount * perHostname,
	}
	b.MaxProbes, b.Short = min(b.Needed, userCap), b.Needed > userCap
	return b, b.validate()
}

// validate 是执行前的最后一道门：预算是调用方算的，但真正开跑前必须自己再看一遍，
// 不能假设上游永远不会给错。硬上限在这里，不在前端。
func (b Budget) validate() error {
	if b.PerHostname < 1 {
		return fmt.Errorf("%w：每主机名候选上限必须大于 0，收到 %d", ErrBadBudget, b.PerHostname)
	}
	// 每主机名候选的硬上限就是完整档那个数，不在这里另抄一份字面量。
	if hard, ok := config.ProfileCandidateLimit(config.ProfileFull); ok && b.PerHostname > hard {
		return fmt.Errorf("%w：每主机名候选上限 %d 越过硬上限 %d", ErrBadBudget, b.PerHostname, hard)
	}
	if b.Concurrency < 1 || b.Concurrency > MaxConcurrency {
		return fmt.Errorf("%w：并发数 %d 不在 1 到 %d", ErrBadBudget, b.Concurrency, MaxConcurrency)
	}
	if b.MaxProbes < 0 {
		return fmt.Errorf("%w：本轮候选探测预算不能是负数，收到 %d", ErrBadBudget, b.MaxProbes)
	}
	if b.MaxProbes > b.Needed && b.Needed >= 0 {
		return fmt.Errorf("%w：本轮候选探测预算 %d 大于理论需要的 %d，说明范围与预算对不上",
			ErrBadBudget, b.MaxProbes, b.Needed)
	}
	return nil
}
