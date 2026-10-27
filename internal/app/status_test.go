package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/config"
)

func newServiceWithStore(t *testing.T, scopeIDs, strategies map[string]bool) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config", "config.json")
	s := &Service{
		appName:  "GitHub Net",
		version:  "0.1.0",
		settings: config.NewStore(path, scopeIDs, strategies),
	}
	return s, path
}

func strPtr[T any](v T) *T { return &v }

func TestProbeHostsAccessClassifiesThreeWays(t *testing.T) {
	dir := t.TempDir()

	writable := filepath.Join(dir, "hosts-writable")
	if err := os.WriteFile(writable, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ProbeHostsAccess(writable); got != HostsAccessWritable {
		t.Errorf("能打开写的文件报 %q，期望 writable", got)
	}
	// 探测不许真的改动文件内容。
	raw, err := os.ReadFile(writable)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "127.0.0.1 localhost\n" {
		t.Errorf("权限探测改动了文件内容: %q", raw)
	}

	readOnly := filepath.Join(dir, "hosts-readonly")
	if err := os.WriteFile(readOnly, []byte("127.0.0.1 localhost\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root 下权限位拦不住写，这一条留给普通用户跑")
	}
	if got := ProbeHostsAccess(readOnly); got != HostsAccessReadOnly {
		t.Errorf("只读文件报 %q，期望 readOnly", got)
	}

	if got := ProbeHostsAccess(filepath.Join(dir, "不存在")); got != HostsAccessUnknown {
		t.Errorf("文件不存在报 %q，期望 unknown（不能当成没权限）", got)
	}
	if got := ProbeHostsAccess(""); got != HostsAccessUnknown {
		t.Errorf("路径为空报 %q，期望 unknown", got)
	}
}

func TestHostsPathMatchesPlatform(t *testing.T) {
	switch runtime.GOOS {
	case "windows":
		if p := HostsPath(); p != "" && !strings.HasSuffix(p, `\drivers\etc\hosts`) {
			t.Errorf("Windows Hosts 路径不对: %q", p)
		}
	case "linux", "darwin":
		if HostsPath() != "/etc/hosts" {
			t.Errorf("%s 的 Hosts 路径应为 /etc/hosts，得到 %q", runtime.GOOS, HostsPath())
		}
	}
}

func TestGetAppStatusReportsPlatformPrivilegeAndDNSChoice(t *testing.T) {
	s, path := newServiceWithStore(t, map[string]bool{"github": true}, nil)
	status, err := s.GetAppStatus()
	if err != nil {
		t.Fatalf("读状态不该失败: %v", err)
	}
	if status.App != "GitHub Net" || status.Version != "0.1.0" {
		t.Errorf("应用名或版本不对: %+v", status)
	}
	if status.OS != runtime.GOOS || status.Arch != runtime.GOARCH {
		t.Errorf("平台信息不是真实运行环境: %+v", status)
	}
	if status.DNSResolver != DNSResolverSystem || status.DNSDetectionReady {
		t.Errorf("DNS 一栏应报「系统解析器 + 检测未接入」，得到 %+v", status)
	}
	if status.HostsAccess != HostsAccessWritable && status.HostsAccess != HostsAccessReadOnly && status.HostsAccess != HostsAccessUnknown {
		t.Errorf("权限档位漂了: %q", status.HostsAccess)
	}
	if status.ConfigPath != path {
		t.Errorf("配置路径 = %q，期望 %q", status.ConfigPath, path)
	}
	if status.ConfigNotice != "" {
		t.Errorf("新临时目录里没文件，不该有重置提示: %q", status.ConfigNotice)
	}
}

func TestGetSettingsGivesDefaultsWithoutWriting(t *testing.T) {
	s, path := newServiceWithStore(t, nil, nil)
	cfg, err := s.GetSettings()
	if err != nil {
		t.Fatalf("没配置文件时应回默认值: %v", err)
	}
	if cfg.Revision != 0 || cfg.Network.Profile != config.ProfileLow || cfg.Network.MaxCandidateProbesPerRun != 108 {
		t.Errorf("默认值不对: %+v", cfg.Network)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("读取配置不该顺手创建文件")
	}
}

func TestSaveSettingsThroughServiceReturnsStableCodes(t *testing.T) {
	patchProfile := config.Patch{NetworkProfile: strPtr(config.ProfileStandard)}

	s, _ := newServiceWithStore(t, nil, nil)
	saved, err := s.SaveSettings(patchProfile, 0, "m1")
	if err != nil || saved.Network.Profile != config.ProfileStandard {
		t.Fatalf("正常保存应成功: %+v err %v", saved, err)
	}
	if got, err := s.SaveSettings(patchProfile, 0, "m2"); err == nil {
		t.Error("副本版本落后应报冲突")
	} else {
		var publicErr *Error
		if !errors.As(err, &publicErr) || publicErr.Code != CodeRevisionConflict {
			t.Errorf("错误码 = %v，期望 %q", err, CodeRevisionConflict)
		}
		if !errors.Is(err, config.ErrRevisionConflict) {
			t.Error("cause 丢了，日志里追不到原始冲突")
		}
		_ = got
	}

	if _, err := s.SaveSettings(config.Patch{NetworkProfile: strPtr("turbo")}, 1, "m3"); err == nil {
		t.Error("未知档位应被拒")
	} else {
		var publicErr *Error
		if !errors.As(err, &publicErr) || publicErr.Code != CodeInvalidSettings {
			t.Fatalf("错误码 = %v，期望 %q", err, CodeInvalidSettings)
		}
		// 可展示消息要说清是哪一条，且不带着内部哨兵前缀。
		if !strings.Contains(publicErr.Message, "档位") {
			t.Errorf("消息没落到具体字段: %q", publicErr.Message)
		}
		if strings.Contains(publicErr.Message, config.ErrInvalid.Error()) {
			t.Errorf("把内部哨兵文字透给了前端: %q", publicErr.Message)
		}
	}

	if _, err := s.SaveSettings(patchProfile, 1, "  "); err == nil {
		t.Error("没带幂等键的保存应被拒")
	} else {
		var publicErr *Error
		if !errors.As(err, &publicErr) || publicErr.Code != CodeInvalidSettings {
			t.Errorf("错误码 = %v", err)
		}
	}
}

func TestSaveSettingsFailureKeepsUnsaved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// 目录改成不可写，模拟磁盘/权限故障。
	if err := os.Chmod(filepath.Dir(path), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0o700) })
	if os.Geteuid() == 0 {
		t.Skip("root 不受目录权限位约束")
	}

	s := &Service{appName: "GitHub Net", version: "0.1.0", settings: config.NewStore(path, nil, nil)}
	_, err := s.SaveSettings(config.Patch{NetworkProfile: strPtr(config.ProfileFull)}, 0, "m1")
	if err == nil {
		t.Fatal("写盘失败必须报错")
	}
	var publicErr *Error
	if !errors.As(err, &publicErr) || publicErr.Code != CodeSettingsSaveFailed {
		t.Fatalf("错误码 = %v，期望 %q", err, CodeSettingsSaveFailed)
	}
	if strings.Contains(publicErr.Message, path) || strings.Contains(publicErr.Message, "permission") {
		t.Errorf("内部细节透给了前端: %q", publicErr.Message)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("失败的保存不该留下文件")
	}
	current, err := s.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != 0 || current.Network.Profile != config.ProfileLow {
		t.Errorf("保存失败后内存状态被改了: %+v", current.Network)
	}
}

func TestGetSettingsRefusesNewerSchemaWithStableCode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	newer := config.Default()
	newer.SchemaVersion = config.SchemaVersion + 1
	raw, err := json.Marshal(newer)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s := &Service{appName: "GitHub Net", version: "0.1.0", settings: config.NewStore(path, nil, nil)}
	if _, err := s.GetSettings(); err == nil {
		t.Fatal("高版本配置应明确报错，不能静默当默认值")
	} else {
		var publicErr *Error
		if !errors.As(err, &publicErr) || publicErr.Code != CodeConfigSchemaNewer {
			t.Errorf("错误码 = %v，期望 %q", err, CodeConfigSchemaNewer)
		}
	}
	if _, err := s.SaveSettings(config.Patch{Theme: strPtr("oled")}, 0, "m1"); err == nil {
		t.Error("旧程序不该改写高版本配置")
	}
	status, err := s.GetAppStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status.ConfigNotice, "更新的程序版本") {
		t.Errorf("状态里应带上这条用户可见说明: %q", status.ConfigNotice)
	}
}

func TestServiceWithoutSettingsStoreAlwaysReturnsError(t *testing.T) {
	s := &Service{appName: "GitHub Net", version: "0.1.0"}
	if _, err := s.GetSettings(); err == nil {
		t.Error("没有配置服务时不能返回 (空配置, nil)")
	}
	if _, err := s.SaveSettings(config.Patch{}, 0, "m1"); err == nil {
		t.Error("保存同样要报错")
	}
	status, err := s.GetAppStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.ConfigNotice == "" {
		t.Error("状态里要说明配置服务不可用")
	}
}

func TestStrategyChoicesStayRejectedUntilRegistryExists(t *testing.T) {
	s, _ := newServiceWithStore(t, map[string]bool{"github": true}, nil)
	_, err := s.SaveSettings(config.Patch{DefaultStrategyByScope: &map[string]string{"github": "dns-hosts"}}, 0, "m1")
	if err == nil {
		t.Fatal("方法注册表还没实现，任何默认方法选择都不该被接受")
	}
	var publicErr *Error
	if !errors.As(err, &publicErr) || publicErr.Code != CodeInvalidSettings {
		t.Errorf("错误码 = %v", err)
	}
	if !strings.Contains(publicErr.Message, "没有注册") {
		t.Errorf("消息该说明方法没注册，得到 %q", publicErr.Message)
	}
}
