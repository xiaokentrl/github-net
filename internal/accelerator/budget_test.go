package accelerator

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/config"
)

func settingsWith(profile string, userCap int) config.Config {
	cfg := config.Default()
	cfg.Network.Profile = profile
	cfg.Network.MaxCandidateProbesPerRun = userCap
	return cfg
}

func TestBudgetForDefaultProfileCoversFullRound(t *testing.T) {
	// 目录实数是 63 个去重主机名，默认档省流（每主机名 2 个候选、并发 2）。
	b, err := BudgetFor(config.Default(), 63)
	if err != nil {
		t.Fatalf("默认设置算预算失败：%v", err)
	}
	if b.PerHostname != 2 || b.Concurrency != 2 {
		t.Errorf("省流档数字不对：每主机名 %d、并发 %d", b.PerHostname, b.Concurrency)
	}
	if b.Needed != 126 || b.MaxProbes != 126 {
		t.Errorf("默认应该正好盖住一轮全量：理论 %d、实际 %d（期望都 126）", b.Needed, b.MaxProbes)
	}
	if b.Short {
		t.Error("默认档位默认盖得住一轮全量，不该标成「盖不住」")
	}
	if b.Profile != config.ProfileLow {
		t.Errorf("档位标识丢了：%q", b.Profile)
	}
}

func TestBudgetForTakesMinOfNeedAndUserCap(t *testing.T) {
	cases := []struct {
		name      string
		profile   string
		userCap   int
		hosts     int
		wantProbe int
		wantShort bool
	}{
		{"标准档被用户上限裁", config.ProfileStandard, 100, 63, 100, true},
		{"标准档用户上限够不着理论需求", config.ProfileStandard, 504, 63, 504, false},
		{"省流档不会因为上限大而变大", config.ProfileLow, 1008, 63, 126, false},
		{"完整档一轮全量正好配满", config.ProfileFull, 1008, 63, 1008, false},
		{"完整档超出上限就标盖不住", config.ProfileFull, 1007, 63, 1007, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := BudgetFor(settingsWith(tc.profile, tc.userCap), tc.hosts)
			if err != nil {
				t.Fatalf("算预算失败：%v", err)
			}
			if b.MaxProbes != tc.wantProbe || b.Short != tc.wantShort {
				t.Errorf("本轮预算 %d（短口 %v），期望 %d（短口 %v）", b.MaxProbes, b.Short, tc.wantProbe, tc.wantShort)
			}
			if b.MaxProbes > b.Needed {
				t.Errorf("预算 %d 大于理论需要的 %d，等于凭空多出不存在的候选", b.MaxProbes, b.Needed)
			}
		})
	}
}

func TestBudgetForProfileConcurrency(t *testing.T) {
	cases := []struct {
		profile string
		per     int
		conc    int
	}{
		{config.ProfileLow, 2, 2},
		{config.ProfileStandard, 8, 8},
		{config.ProfileFull, 16, 8},
	}
	for _, tc := range cases {
		t.Run(tc.profile, func(t *testing.T) {
			b, err := BudgetFor(settingsWith(tc.profile, 1008), 10)
			if err != nil {
				t.Fatalf("算预算失败：%v", err)
			}
			if b.PerHostname != tc.per || b.Concurrency != tc.conc {
				t.Errorf("档位 %s 得到每主机名 %d、并发 %d，期望 %d、%d", tc.profile, b.PerHostname, b.Concurrency, tc.per, tc.conc)
			}
			if b.Concurrency > MaxConcurrency {
				t.Errorf("并发 %d 越过安全硬上限 %d", b.Concurrency, MaxConcurrency)
			}
		})
	}
}

// 其余用例都只引用 MaxConcurrency 这个常量，不引用数字本身——所以光有它们拦不住把常量改成 9。
// 8 是规划定的安全硬上限（同一时刻最多 8 个网络请求），这条把它钉成字面量。
func TestMaxConcurrencyIsThePlanCeiling(t *testing.T) {
	if MaxConcurrency != 8 {
		t.Errorf("活动请求安全硬上限应为 8，实得 %d", MaxConcurrency)
	}
}

func TestBudgetForRejectsBadSettings(t *testing.T) {
	cases := []struct {
		name  string
		cfg   config.Config
		hosts int
		want  string // 那句话说清是哪一个参数错，界面和日志才分得开
	}{
		{"档位不认识", settingsWith("turbo", 130), 10, "不认识的网络档位"},
		{"上限为 0", settingsWith(config.ProfileLow, 0), 10, "每轮候选探测上限"},
		{"上限超过可填最大值", settingsWith(config.ProfileLow, config.MaxCandidateProbesLimit+1), 10, "每轮候选探测上限"},
		{"主机名数为负", settingsWith(config.ProfileLow, 130), -1, "本轮主机名数量不能是负数"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BudgetFor(tc.cfg, tc.hosts)
			if !errors.Is(err, ErrBadBudget) {
				t.Fatalf("应当判预算不合法，实得 %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("那句说明应包含 %q，实得 %q", tc.want, err.Error())
			}
		})
	}
}

// 执行前那道门不看来源：手改过的预算同样拦。并发和每主机名候选的硬上限在后端，不在前端。
func TestBudgetValidateEnforcesHardCaps(t *testing.T) {
	cases := []struct {
		name   string
		budget Budget
	}{
		{"并发越过硬上限", Budget{PerHostname: 2, Concurrency: MaxConcurrency + 1, MaxProbes: 4, Needed: 4}},
		{"并发为 0", Budget{PerHostname: 2, Concurrency: 0, MaxProbes: 4, Needed: 4}},
		{"每主机名候选越过硬上限", Budget{PerHostname: 17, Concurrency: 2, MaxProbes: 4, Needed: 4}},
		{"每主机名候选为 0", Budget{PerHostname: 0, Concurrency: 2, MaxProbes: 4, Needed: 4}},
		{"预算大于理论需要", Budget{PerHostname: 2, Concurrency: 2, MaxProbes: 5, Needed: 4}},
		{"预算为负", Budget{PerHostname: 2, Concurrency: 2, MaxProbes: -1, Needed: 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.budget.validate(); !errors.Is(err, ErrBadBudget) {
				t.Errorf("应当拦下，实得 %v", err)
			}
		})
	}
}

func TestBudgetForEmptyRoundIsValid(t *testing.T) {
	b, err := BudgetFor(config.Default(), 0)
	if err != nil {
		t.Fatalf("本轮没有主机名不该算调用错误：%v", err)
	}
	if b.Needed != 0 || b.MaxProbes != 0 || b.Short {
		t.Errorf("空轮应当是 0 次预算且不标短口，实得 %+v", b)
	}
}

// 期限是执行现场的内部旋钮：生产走规划定的 5 秒，测试换小才能不真等。
func TestItemDeadlineIsTheDefault(t *testing.T) {
	b, err := BudgetFor(config.Default(), 3)
	if err != nil {
		t.Fatalf("算预算失败：%v", err)
	}
	if got := b.effectiveDeadline(); got != ItemDeadline {
		t.Errorf("默认本轮期限 %v，期望 %v", got, ItemDeadline)
	}
	if ItemDeadline != 5*time.Second {
		t.Errorf("单项检测期限被改成 %v，规划定的是 5 秒", ItemDeadline)
	}
}
