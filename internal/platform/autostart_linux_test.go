//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/identity"
)

// 自启动测试把 XDG_CONFIG_HOME 指到临时目录，真实自启动配置一个字节不碰。
func testAutostart(t *testing.T) Autostart {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return NewAutostart()
}

func TestAutostartEnableDisableRoundTrip(t *testing.T) {
	a := testAutostart(t)

	enabled, err := a.Enabled()
	if err != nil || enabled {
		t.Fatalf("初始该是未启用 (%v, %v)", enabled, err)
	}
	if err := a.Enable(); err != nil {
		t.Fatalf("启用不该失败: %v", err)
	}
	enabled, err = a.Enabled()
	if err != nil || !enabled {
		t.Fatalf("启用后该读到已启用 (%v, %v)", enabled, err)
	}

	// 入口内容：Exec 是绝对路径、名字是产品身份、带 autostart 标记。
	dir, _ := autostartDir()
	raw, err := os.ReadFile(filepath.Join(dir, identity.AppID+".desktop"))
	if err != nil {
		t.Fatalf("读自启动入口: %v", err)
	}
	content := string(raw)
	for _, want := range []string{"[Desktop Entry]", "Exec=/usr/local/bin/" + identity.Slug, "X-GNOME-Autostart-enabled=true"} {
		if !strings.Contains(content, want) {
			t.Fatalf("自启动入口缺 %q：%s", want, content)
		}
	}

	// 再启用一次是幂等的覆盖，不是报错。
	if err := a.Enable(); err != nil {
		t.Fatalf("重复启用该幂等: %v", err)
	}

	if err := a.Disable(); err != nil {
		t.Fatalf("停用不该失败: %v", err)
	}
	enabled, err = a.Enabled()
	if err != nil || enabled {
		t.Fatalf("停用后该是未启用 (%v, %v)", enabled, err)
	}
	// 再停用一次：本来就没有也算成功。
	if err := a.Disable(); err != nil {
		t.Fatalf("重复停用该幂等: %v", err)
	}
}

func TestAutostartEnableLeavesNoTempFile(t *testing.T) {
	a := testAutostart(t)
	if err := a.Enable(); err != nil {
		t.Fatalf("启用: %v", err)
	}
	dir, _ := autostartDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读目录: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("临时文件没清干净: %s", e.Name())
		}
	}
}

func TestAutostartWithoutHomeFails(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	a := NewAutostart()
	if _, err := a.Enabled(); err == nil {
		t.Fatal("拿不到主目录时该报错，不能静默当未启用")
	}
	if err := a.Enable(); err == nil {
		t.Fatal("拿不到主目录时启用该失败")
	}
}
