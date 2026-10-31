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

func str(s string) *string { return &s }
func num(i int) *int       { return &i }
func flag(b bool) *bool    { return &b }

func TestDefaultIsValidAndUnsaved(t *testing.T) {
	c := Default()
	if err := c.Validate(map[string]bool{"github": true}, nil); err != nil {
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
	if c.Layout.SidebarWidth != 240 || c.Layout.QueueHeight != QueueHeightSmall || c.Layout.QueueListWidth != 220 {
		t.Errorf("布局默认值不对: %+v", c.Layout)
	}
}

func TestSnapshotWritesNothingWhenFileMissing(t *testing.T) {
	path := newPath(t)
	s := NewStore(path, nil, nil)
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
	s := NewStore(path, map[string]bool{"github": true}, nil)
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
	s := NewStore(path, nil, nil)
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
	if got.Layout.SidebarWidth != 240 || got.Appearance.Zoom != 1 {
		t.Errorf("未提交的字段被改了: %+v", got)
	}
}

func TestRepeatedMutationIDIsIdempotent(t *testing.T) {
	s := NewStore(newPath(t), nil, nil)
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
	s := NewStore(path, nil, nil)
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
	s := NewStore(path, nil, nil)
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
		{"队列过矮", Patch{QueueHeight: num(60)}},
		{"任务列表过窄", Patch{QueueListWidth: num(149)}},
		{"任务列表过宽", Patch{QueueListWidth: num(1201)}},
		{"自动应用没开检测", Patch{AutoApplyOnStartup: flag(true)}},
		{"范围不在目录里", Patch{DefaultStrategyByScope: &map[string]string{"nope": "dns-hosts"}}},
		{"方法还没注册", Patch{DefaultStrategyByScope: &map[string]string{"github": "dns-hosts"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewStore(newPath(t), scopeIDs, nil)
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
		s := NewStore(newPath(t), nil, nil)
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
		s := NewStore(path, nil, nil)
		got, err := s.Save(Patch{QueueListWidth: num(value)}, 0, "m-width")
		if err != nil {
			t.Fatalf("任务列表宽度 %d 应能保存: %v", value, err)
		}
		if got.Layout.QueueListWidth != value {
			t.Errorf("保存回包里的宽度 = %d，期望 %d", got.Layout.QueueListWidth, value)
		}
		// 重新打开一份 store 读同一份盘：宽度必须还在，不能只活在内存里。
		reopened := NewStore(path, nil, nil)
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
	s := NewStore(path, nil, nil)
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

func TestAutoApplyRequiresAutoDetectAndIsNotSilentlyRewritten(t *testing.T) {
	s := NewStore(newPath(t), nil, nil)
	if _, err := s.Save(Patch{AutoDetectOnStartup: flag(true), AutoApplyOnStartup: flag(true)}, 0, "m1"); err != nil {
		t.Fatalf("两个一起开应通过: %v", err)
	}
	// 只关检测：后端不把自动应用顺手关掉，而是明确拒绝，让用户在同一批里处理干净。
	if _, err := s.Save(Patch{AutoDetectOnStartup: flag(false)}, 1, "m2"); err == nil {
		t.Fatal("依赖没解开就保存应被拒绝")
	} else if !errors.Is(err, ErrInvalid) {
		t.Errorf("错误类型不对: %v", err)
	} else if !strings.Contains(err.Error(), "自动检测") {
		t.Errorf("消息没说清是哪条依赖: %v", err)
	}
	got, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Startup.AutoDetectOnStartup || !got.Startup.AutoApplyOnStartup {
		t.Errorf("被拒的保存在内存里生效了: %+v", got.Startup)
	}
	// 两个一起关是合法提交。
	if _, err := s.Save(Patch{AutoDetectOnStartup: flag(false), AutoApplyOnStartup: flag(false)}, 1, "m3"); err != nil {
		t.Errorf("一起关应能保存: %v", err)
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
	s := NewStore(path, nil, nil)
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
	s := NewStore(path, nil, nil)
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
	s := NewStore(path, nil, nil)
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
	s := NewStore(path, nil, nil)
	if _, err := s.Save(Patch{SidebarWidth: num(340), NetworkProfile: str(ProfileStandard)}, 0, "m1"); err != nil {
		t.Fatal(err)
	}
	reopened := NewStore(path, nil, nil)
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
	s := NewStore(newPath(t), nil, nil)
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
