package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config", "config.json")
}

func str(s string) *string       { return &s }
func num(i int) *int             { return &i }
func flag(b bool) *bool          { return &b }
func menus(v []string) *[]string { return &v }

func TestDefaultIsValidAndUnsaved(t *testing.T) {
	c := Default()
	if err := c.Validate(map[string]bool{"github": true}, nil, nil); err != nil {
		t.Fatalf("默认值应通过校验: %v", err)
	}
	if c.Revision != 0 {
		t.Errorf("默认值 revision = %d，期望 0（还没保存过）", c.Revision)
	}
	// 126 = 目录去重后 63 个主机名 × 省流档每项 2 个候选：默认值就该盖住默认档位的一轮全量。
	if c.Network.Profile != ProfileLow || c.Network.MaxCandidateProbesPerRun != 126 {
		t.Errorf("首次安装必须是省流档、总上限 126，得到 %+v", c.Network)
	}
	// 队列三个尺寸各有默认值：左栏 220 是原型定稿的标准宽度；队列高度默认「小」档（拍板 2026-10-31）。
	if c.Layout.SidebarWidth != 224 || c.Layout.QueueHeight != QueueHeightSmall || c.Layout.QueueListWidth != 220 {
		t.Errorf("布局默认值不对: %+v", c.Layout)
	}
	if !c.Layout.ToastDisabled {
		t.Error("提示面板出厂默认应关闭（设置里那颗开关默认不勾选）")
	}
}

// 提示面板出厂默认关掉；老配置里没写过这个字段时也要按默认走，
// 显式写过 true/false 的则尊重用户自己选的。
func TestToastDisabledDefaultAndMissingField(t *testing.T) {
	for _, tc := range []struct {
		name     string
		explicit *bool
		want     bool
	}{
		{"缺字段按出厂默认关闭", nil, true},
		{"显式 false 尊重用户选择", flag(false), false},
		{"显式 true 尊重用户选择", flag(true), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := newPath(t)
			// 以能通过校验的默认配置为底，只抹掉／改写 layout.toastDisabled。
			raw, err := json.Marshal(Default())
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			layout := doc["layout"].(map[string]any)
			delete(layout, "toastDisabled")
			if tc.explicit != nil {
				layout["toastDisabled"] = *tc.explicit
			}
			raw, err = json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := NewStore(path, map[string]bool{"github": true}, nil, nil).Snapshot()
			if err != nil {
				t.Fatalf("读取配置失败: %v", err)
			}
			if got.Layout.ToastDisabled != tc.want {
				t.Errorf("ToastDisabled = %v，期望 %v", got.Layout.ToastDisabled, tc.want)
			}
		})
	}
}

// 队列高度锁：锁定那一刻的高度和锁定状态都要落盘，重开后原样回来。
func TestQueueHeightLockSurvivesReopen(t *testing.T) {
	if Default().Layout.QueueHeightLocked {
		t.Fatal("出厂默认不该锁住队列高度")
	}
	path := newPath(t)
	s := NewStore(path, map[string]bool{"github": true}, nil, nil)
	first, err := s.Save(Patch{QueueHeight: num(300)}, 0, "m1")
	if err != nil {
		t.Fatalf("调高度失败: %v", err)
	}
	if _, err := s.Save(Patch{QueueHeightLocked: flag(true)}, first.Revision, "m2"); err != nil {
		t.Fatalf("锁定失败: %v", err)
	}

	// 换一个 Store 读同一份文件，等价于重启应用。
	reopened := NewStore(path, map[string]bool{"github": true}, nil, nil)
	got, err := reopened.Snapshot()
	if err != nil {
		t.Fatalf("重开后读配置失败: %v", err)
	}
	if !got.Layout.QueueHeightLocked {
		t.Error("重开后高度锁没保住")
	}
	if got.Layout.QueueHeight != 300 {
		t.Errorf("重开后队列高度 = %d，期望锁定时的 300", got.Layout.QueueHeight)
	}
}

func TestSnapshotWritesNothingWhenFileMissing(t *testing.T) {
	path := newPath(t)
	s := NewStore(path, nil, nil, nil)
	if _, err := s.Snapshot(); err != nil {
		t.Fatalf("读取默认值不该失败: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Error("规划要求「不因为读取而强制写盘」，目录不该被创建")
	}
	if s.Notice() != "" {
		t.Errorf("没有文件不是异常，不该有提示: %q", s.Notice())
	}
}

func TestSaveWritesFileAndBumpsRevision(t *testing.T) {
	path := newPath(t)
	s := NewStore(path, map[string]bool{"github": true}, nil, nil)
	saved, err := s.Save(Patch{NetworkProfile: str(ProfileStandard)}, 0, "m1")
	if err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if saved.Revision != 1 || saved.Network.Profile != ProfileStandard {
		t.Fatalf("保存结果不对: %+v", saved)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("配置文件没写出来: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("配置文件权限 = %o，期望只有当前用户可读写 600", perm)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk Config
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("落盘内容不是合法 JSON: %v", err)
	}
	if onDisk.SchemaVersion != SchemaVersion || onDisk.Revision != 1 || onDisk.Network.Profile != ProfileStandard {
		t.Errorf("磁盘内容与快照不一致: %+v", onDisk)
	}
	// 目录里不该留临时文件半成品。
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("配置目录里有 %d 个条目，期望只有 config.json: %v", len(entries), entries)
	}
}

func TestPatchOnlyTouchesSubmittedFields(t *testing.T) {
	path := newPath(t)
	s := NewStore(path, nil, nil, nil)
	if _, err := s.Save(Patch{Theme: str("oled")}, 0, "m1"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Save(Patch{NetworkProfile: str(ProfileFull)}, 1, "m2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Appearance.Theme != "oled" {
		t.Errorf("只改档位的保存把主题冲掉了: %+v", got.Appearance)
	}
	if got.Layout.SidebarWidth != 224 || got.Appearance.Zoom != 1 {
		t.Errorf("未提交的字段被改了: %+v", got)
	}
}

func TestRepeatedMutationIDIsIdempotent(t *testing.T) {
	s := NewStore(newPath(t), nil, nil, nil)
	first, err := s.Save(Patch{NetworkProfile: str(ProfileStandard)}, 0, "same")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Save(Patch{NetworkProfile: str(ProfileStandard)}, 0, "same")
	if err != nil {
		t.Fatalf("重复 mutationID 应幂等返回原结果，得到错误 %v", err)
	}
	if second.Revision != first.Revision || second.Network.Profile != ProfileStandard {
		t.Errorf("重复提交推进了 revision: %+v", second)
	}
	if _, err := s.Save(Patch{NetworkProfile: str(ProfileFull)}, 1, "next"); err != nil {
		t.Errorf("换 mutationID 应正常保存: %v", err)
	}
}

func TestEmptyMutationIDRejected(t *testing.T) {
	path := newPath(t)
	s := NewStore(path, nil, nil, nil)
	if _, err := s.Save(Patch{Theme: str("oled")}, 0, "  "); err == nil {
		t.Fatal("没带幂等键的保存必须被拒")
	} else if !errors.Is(err, ErrEmptyMutationID) {
		t.Errorf("错误类型不对: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("被拒的保存不该落盘")
	}
}

func TestRevisionConflictDoesNotWrite(t *testing.T) {
	path := newPath(t)
	s := NewStore(path, nil, nil, nil)
	if _, err := s.Save(Patch{Theme: str("oled")}, 0, "m1"); err != nil {
		t.Fatal(err)
	}
	// 前端还拿着第 0 版副本，不该覆盖第 1 版。
	if _, err := s.Save(Patch{Theme: str("forest")}, 0, "m2"); err == nil {
		t.Fatal("revision 落后应报冲突")
	} else if !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("错误类型不对: %v", err)
	}
	got, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got.Appearance.Theme != "oled" || got.Revision != 1 {
		t.Errorf("冲突的保存在内存里生效了: %+v", got)
	}
}

func TestInvalidValuesRejected(t *testing.T) {
	scopeIDs := map[string]bool{"github": true}
	cases := []struct {
		name  string
		patch Patch
	}{
		{"未知档位", Patch{NetworkProfile: str("turbo")}},
		{"总上限为 0", Patch{MaxCandidateProbesPerRun: num(0)}},
		{"总上限超过 1008", Patch{MaxCandidateProbesPerRun: num(1009)}},
		{"未正式支持的语言", Patch{Language: str("fr-FR")}},
		{"未知主题", Patch{Theme: str("neon")}},
		{"不在档位的缩放", Patch{Zoom: func() *float64 { z := 1.5; return &z }()}},
		{"侧栏过窄", Patch{SidebarWidth: num(120)}},
		{"侧栏过宽", Patch{SidebarWidth: num(900)}},
		{"队列过矮", Patch{QueueHeight: num(44)}},
		{"任务列表过窄", Patch{QueueListWidth: num(149)}},
		{"任务列表过宽", Patch{QueueListWidth: num(1201)}},
		{"自动应用没选主菜单", Patch{AutoApplyOnStartup: flag(true)}},
		{"自动退出没开自动应用", Patch{AutoCloseAfterApply: flag(true)}},
		{"范围不在目录里", Patch{DefaultStrategyByScope: &map[string]string{"nope": "dns-hosts"}}},
		{"方法还没注册", Patch{DefaultStrategyByScope: &map[string]string{"github": "dns-hosts"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore(newPath(t), scopeIDs, nil, nil)
			if _, err := s.Save(tc.patch, 0, "m-"+tc.name); err == nil {
				t.Fatalf("%s 应被拒绝", tc.name)
			} else if !errors.Is(err, ErrInvalid) {
				t.Errorf("错误应为 ErrInvalid，得到 %v", err)
			}
		})
	}
}

// 边界值本身必须能用：126 是省流档一轮全量刚好盖住的那个数，1008 是完整档一轮全量的理论值。
// 卡在边界上被拒，等于把「最大档位配满」这条路堵死，而界面输入框的 max 正是它。
func TestCandidateBudgetBoundaryValuesAreAccepted(t *testing.T) {
	for _, value := range []int{MinCandidateProbes, 126, 1008} {
		s := NewStore(newPath(t), nil, nil, nil)
		got, err := s.Save(Patch{MaxCandidateProbesPerRun: num(value)}, 0, "m-boundary")
		if err != nil {
			t.Fatalf("总上限 %d 应能保存: %v", value, err)
		}
		if got.Network.MaxCandidateProbesPerRun != value {
			t.Errorf("总上限 = %d，期望 %d", got.Network.MaxCandidateProbesPerRun, value)
		}
	}
}

// 队列左栏「任务列表」的宽度：界面拖拽和布局屏预设都往这里写。
// 下限 150 与上限 1200 本身必须能用——界面按「队列区宽的 45%」再收一道，
// 但存储这层只认这两个死数，越界就整批拒，不能悄悄夹成边界值。
func TestQueueListWidthBoundsAndRoundTrip(t *testing.T) {
	for _, value := range []int{QueueListWidthMin, 220, QueueListWidthMax} {
		path := newPath(t)
		s := NewStore(path, nil, nil, nil)
		got, err := s.Save(Patch{QueueListWidth: num(value)}, 0, "m-width")
		if err != nil {
			t.Fatalf("任务列表宽度 %d 应能保存: %v", value, err)
		}
		if got.Layout.QueueListWidth != value {
			t.Errorf("保存回包里的宽度 = %d，期望 %d", got.Layout.QueueListWidth, value)
		}
		// 重新打开一份 store 读同一份盘：宽度必须还在，不能只活在内存里。
		reopened := NewStore(path, nil, nil, nil)
		again, err := reopened.Snapshot()
		if err != nil {
			t.Fatalf("重读配置失败: %v", err)
		}
		if again.Layout.QueueListWidth != value {
			t.Errorf("落盘再读回的宽度 = %d，期望 %d", again.Layout.QueueListWidth, value)
		}
	}
	// 没提交这个字段时不许多写一刀：宽度保持上一次保存的值。
	path := newPath(t)
	s := NewStore(path, nil, nil, nil)
	if _, err := s.Save(Patch{QueueListWidth: num(300)}, 0, "m1"); err != nil {
		t.Fatalf("先存一个宽度: %v", err)
	}
	got, err := s.Save(Patch{Theme: str("oled")}, 1, "m2")
	if err != nil {
		t.Fatalf("只改主题的保存应成功: %v", err)
	}
	if got.Layout.QueueListWidth != 300 {
		t.Errorf("只改主题不该动宽度，实际 %d", got.Layout.QueueListWidth)
	}
}

// 队列高度：展开态和收起那一条都往这同一个字段写（拍板 2026-11-02 把下限从 120 降到 45，
// 45 = 队列头 44px + 1px 下边框，收起那条要能拖到这么薄）。下限这一档必须真存得下、
// 再往下一个像素必须被拒：只测拒绝那一侧的话，哪天有人把下限改回去，界面拖到 45–119
// 会被后端悄悄拒掉，看上去就是「拖不动」。
func TestQueueHeightBoundsAndRoundTrip(t *testing.T) {
	for _, value := range []int{QueueHeightMin, QueueHeightSmall, QueueHeightMax} {
		path := newPath(t)
		s := NewStore(path, nil, nil, nil)
		got, err := s.Save(Patch{QueueHeight: num(value)}, 0, "m-height")
		if err != nil {
			t.Fatalf("队列高度 %d 应能保存: %v", value, err)
		}
		if got.Layout.QueueHeight != value {
			t.Errorf("保存回包里的高度 = %d，期望 %d", got.Layout.QueueHeight, value)
		}
		reopened := NewStore(path, nil, nil, nil)
		again, err := reopened.Snapshot()
		if err != nil {
			t.Fatalf("重读配置失败: %v", err)
		}
		if again.Layout.QueueHeight != value {
			t.Errorf("落盘再读回的高度 = %d，期望 %d", again.Layout.QueueHeight, value)
		}
	}
	s := NewStore(newPath(t), nil, nil, nil)
	if _, err := s.Save(Patch{QueueHeight: num(QueueHeightMin - 1)}, 0, "m-low"); err == nil {
		t.Errorf("队列高度 %d 低于下限 %d，应被拒绝", QueueHeightMin-1, QueueHeightMin)
	} else if !errors.Is(err, ErrInvalid) {
		t.Errorf("低于下限该报 ErrInvalid，实际 %v", err)
	}
	if _, err := s.Save(Patch{QueueHeight: num(QueueHeightMax + 1)}, 0, "m-high"); err == nil {
		t.Errorf("队列高度 %d 超过上限 %d，应被拒绝", QueueHeightMax+1, QueueHeightMax)
	}
}

func TestStartupAutomationChainIsRejectedAndNeverSilentlyRewritten(t *testing.T) {
	// 启动自动化是一条链：菜单清单 → 自动应用 → 自动退出，下游依赖上游。
	// 后端从不替用户顺手关掉下游——矛盾组合整批拒，让用户在同一批里处理干净。
	menuIDs := map[string]bool{"github": true, "steam": true}
	path := newPath(t)
	s := NewStore(path, nil, nil, menuIDs)

	if _, err := s.Save(Patch{
		AutoDetectMenus:     menus([]string{"github", "steam"}),
		AutoApplyOnStartup:  flag(true),
		AutoCloseAfterApply: flag(true),
	}, 0, "m1"); err != nil {
		t.Fatalf("三级一起开应通过: %v", err)
	}
	// 只关中间那环：自动退出还开着，依赖断了就拒。
	if _, err := s.Save(Patch{AutoApplyOnStartup: flag(false)}, 1, "m2"); err == nil {
		t.Fatal("自动退出没跟着关就改自动应用应被拒绝")
	} else if !errors.Is(err, ErrInvalid) {
		t.Errorf("错误类型不对: %v", err)
	} else if !strings.Contains(err.Error(), "自动应用") {
		t.Errorf("消息没说清是哪条依赖: %v", err)
	}
	// 只清空清单：下游两个还开着，同样拒。
	if _, err := s.Save(Patch{AutoDetectMenus: menus([]string{})}, 1, "m3"); err == nil {
		t.Fatal("清空清单而下游没关应被拒绝")
	}
	got, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Startup.AutoDetectMenus) != 2 || !got.Startup.AutoApplyOnStartup || !got.Startup.AutoCloseAfterApply {
		t.Errorf("被拒的保存在内存里生效了: %+v", got.Startup)
	}
	// 一次把三级都关掉是合法提交；重新开档读同一份盘，关掉的确实是关掉的。
	if _, err := s.Save(Patch{
		AutoDetectMenus:     menus([]string{}),
		AutoApplyOnStartup:  flag(false),
		AutoCloseAfterApply: flag(false),
	}, 1, "m4"); err != nil {
		t.Errorf("一起关应能保存: %v", err)
	}
	again, err := NewStore(path, nil, nil, menuIDs).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Startup.AutoDetectMenus) != 0 || again.Startup.AutoApplyOnStartup || again.Startup.AutoCloseAfterApply {
		t.Errorf("落盘再读没关上: %+v", again.Startup)
	}
}

func TestAutoDetectMenusMustComeFromCatalog(t *testing.T) {
	// 清单里的 ID 必须是目录里真有的主菜单；校验取原样字符串，
	// 所以带空格的、重复的、空的、不存在的都一律拒。
	menuIDs := map[string]bool{"github": true}
	cases := []struct {
		name string
		list []string
	}{
		{"不在目录里", []string{"nope"}},
		{"重复出现", []string{"github", "github"}},
		{"空项", []string{""}},
		{"带空格的 ID", []string{" github "}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore(newPath(t), nil, nil, menuIDs)
			if _, err := s.Save(Patch{AutoDetectMenus: menus(tc.list)}, 0, "m-"+tc.name); err == nil {
				t.Fatalf("%s 应被拒绝", tc.name)
			} else if !errors.Is(err, ErrInvalid) {
				t.Errorf("错误应为 ErrInvalid，得到 %v", err)
			}
		})
	}
	// 校验集合传空就是「一个菜单都不认」——装配漏传目录时这道门不会静默放行。
	empty := NewStore(newPath(t), nil, nil, nil)
	if _, err := empty.Save(Patch{AutoDetectMenus: menus([]string{"github"})}, 0, "m-no-set"); err == nil {
		t.Fatal("校验集合为空时任何主菜单都应被拒")
	}
}

func TestCorruptConfigIsQuarantinedAndDefaultsUsed(t *testing.T) {
	path := newPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ 这不是 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path, nil, nil, nil)
	got, err := s.Snapshot()
	if err != nil {
		t.Fatalf("坏配置应回退默认值继续运行，不是报错: %v", err)
	}
	if got.Network.Profile != ProfileLow {
		t.Errorf("没回到默认值: %+v", got.Network)
	}
	if s.Notice() == "" {
		t.Error("必须给出用户可见的「已重置并备份到何处」说明")
	}
	bad, err := os.ReadFile(BackupPath(path))
	if err != nil {
		t.Fatalf("原文件没被隔离备份: %v", err)
	}
	if string(bad) != "{ 这不是 JSON" {
		t.Errorf("备份内容不对: %q", bad)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("原件应已从原位移走，避免下一次保存写进内容不明的文件")
	}
}

func TestInvalidConfigContentIsQuarantined(t *testing.T) {
	path := newPath(t)
	broken := Default()
	broken.Network.Profile = "turbo"
	raw, _ := json.Marshal(broken)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path, nil, nil, nil)
	got, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got.Network.Profile != ProfileLow || s.Notice() == "" {
		t.Errorf("不合格内容应回默认值并说明: %+v notice=%q", got.Network, s.Notice())
	}
	if _, err := os.Stat(BackupPath(path)); err != nil {
		t.Errorf("不合格内容也要留隔离副本: %v", err)
	}
}

func TestHigherSchemaVersionIsRefusedNotRewritten(t *testing.T) {
	path := newPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	newer := Default()
	newer.SchemaVersion = SchemaVersion + 1
	raw, _ := json.Marshal(newer)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path, nil, nil, nil)
	if _, err := s.Snapshot(); err == nil {
		t.Fatal("高于当前版本的配置应明确报错")
	} else if !errors.Is(err, ErrSchemaTooNew) {
		t.Errorf("错误类型不对: %v", err)
	}
	if _, err := s.Save(Patch{Theme: str("oled")}, 0, "m1"); !errors.Is(err, ErrSchemaTooNew) {
		t.Errorf("拒绝改写高版本配置，保存也应报同样的错，得到 %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(raw) {
		t.Error("旧程序改写了新配置文件")
	}
}

func TestReloadFromDiskKeepsSavedValues(t *testing.T) {
	path := newPath(t)
	s := NewStore(path, nil, nil, nil)
	if _, err := s.Save(Patch{SidebarWidth: num(340), NetworkProfile: str(ProfileStandard)}, 0, "m1"); err != nil {
		t.Fatal(err)
	}
	reopened := NewStore(path, nil, nil, nil)
	got, err := reopened.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout.SidebarWidth != 340 || got.Network.Profile != ProfileStandard || got.Revision != 1 {
		t.Errorf("重开读到的不是保存过的值: %+v", got)
	}
}

func TestProfileCandidateLimits(t *testing.T) {
	for profile, want := range map[string]int{ProfileLow: 2, ProfileStandard: 8, ProfileFull: 16} {
		got, ok := ProfileCandidateLimit(profile)
		if !ok || got != want {
			t.Errorf("档位 %s 的每主机名候选上限 = %d，期望 %d", profile, got, want)
		}
	}
	if _, ok := ProfileCandidateLimit("turbo"); ok {
		t.Error("未知档位不该有上限")
	}
}

func TestZeroValuePatchFieldsAreDistinguishable(t *testing.T) {
	// 提交 false / 0 这类零值必须真的生效，不能被当成「没提交」。
	s := NewStore(newPath(t), nil, nil, nil)
	got, err := s.Save(Patch{ConfirmUpdateDownloadOnMetered: flag(false), MaxCandidateProbesPerRun: num(1)}, 0, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Network.ConfirmUpdateDownloadOnMetered {
		t.Error("提交了 false 却没落下去")
	}
	if got.Network.MaxCandidateProbesPerRun != 1 {
		t.Errorf("总上限 = %d，期望 1", got.Network.MaxCandidateProbesPerRun)
	}
}
