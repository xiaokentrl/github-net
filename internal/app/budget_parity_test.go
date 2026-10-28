package app

import (
	"testing"

	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
)

// 默认 130、可填上限 1040 不是拍出来的数，是目录实数推出来的：
// 去重主机名数 × 档位每主机名候选上限（省流 2、完整 16）。
// 目录哪天加了域名，这两个数就盖不住一轮全量，而界面上的提示语还在说「够覆盖一轮全量」——
// 所以这条门先红，逼着改目录的人同时重算预算，而不是等用户按一轮才发现漏检。
func TestCandidateBudgetMatchesCatalogSize(t *testing.T) {
	c, err := catalog.Load()
	if err != nil {
		t.Fatalf("内嵌目录读不出来：%v", err)
	}
	hostnames := len(c.UniqueHostnames())
	low, ok := config.ProfileCandidateLimit(config.ProfileLow)
	if !ok {
		t.Fatalf("省流档没有候选上限")
	}
	full, ok := config.ProfileCandidateLimit(config.ProfileFull)
	if !ok {
		t.Fatalf("完整档没有候选上限")
	}
	if want := hostnames * low; config.MaxCandidateProbesDefault != want {
		t.Errorf("默认总上限 = %d，按目录算应为 %d（%d 个主机名 × 省流档 %d 个候选）",
			config.MaxCandidateProbesDefault, want, hostnames, low)
	}
	if want := hostnames * full; config.MaxCandidateProbesLimit != want {
		t.Errorf("可填上限 = %d，按目录算应为 %d（%d 个主机名 × 完整档 %d 个候选）",
			config.MaxCandidateProbesLimit, want, hostnames, full)
	}
	// 上限必须还容得下标准档一轮全量，否则「默认盖不住、手动也调不满」是同一类坑。
	standard, ok := config.ProfileCandidateLimit(config.ProfileStandard)
	if !ok {
		t.Fatal("标准档没有候选上限")
	}
	if config.MaxCandidateProbesLimit < hostnames*standard {
		t.Errorf("可填上限 %d 小于标准档一轮全量所需的 %d", config.MaxCandidateProbesLimit, hostnames*standard)
	}
}
