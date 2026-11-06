package app

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xiaokentrl/github-net/internal/config"
)

// 「记住选项」要跨重启、跨重装：重启＝换一个 Service 重读，重装＝用户目录里那份小文件还在。
// 同时必须不碰 config.json，设置中心那个档位一个字不改。
func TestRememberedDetectProfileSurvivesRestartAndReinstall(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("这里按 XDG_CONFIG_HOME 隔离目录，只在 Linux 验")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	path, err := rememberedProfilePath()
	if err != nil {
		t.Fatal(err)
	}
	if got := ReadRememberedProfile(); got != "" {
		t.Fatalf("没记住过却读到了 %q", got)
	}
	if err := WriteRememberedProfile("full"); err != nil {
		t.Fatalf("记住档位失败: %v", err)
	}

	// 重启：进程内存全丢，换一个 Service 从盘上读。
	if got := (&Service{}).GetLastDetectProfile(); got != "full" {
		t.Errorf("重启后读到 %q，期望 full", got)
	}
	// 重装：安装包只换包内文件，用户目录里这份小文件原样还在。
	if filepath.Base(path) != rememberedProfileFile {
		t.Errorf("偏好文件名 = %q，期望 %q", filepath.Base(path), rememberedProfileFile)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("重装后偏好文件该还在: %v", err)
	}
	// 没改设置：config.json 根本没被创建，更没有写档位。
	configPath, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("记弹框档位不该动 config.json")
	}

	// 取消「记住选项」再检测＝忘掉：文件删掉，下次开弹框回到省流。
	if err := WriteRememberedProfile(""); err != nil {
		t.Fatalf("忘掉档位失败: %v", err)
	}
	if got := (&Service{}).GetLastDetectProfile(); got != "" {
		t.Errorf("忘掉后仍读到 %q", got)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("忘掉后偏好文件该被删掉")
	}
}

// 垃圾值不写盘、坏文件不当真：这个偏好不能把检测入口拖下水。
func TestRememberedDetectProfileRejectsBadValues(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("这里按 XDG_CONFIG_HOME 隔离目录，只在 Linux 验")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path, err := rememberedProfilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteRememberedProfile("bogus"); err == nil {
		t.Error("不认得的档位不该写盘")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadRememberedProfile(); got != "" {
		t.Errorf("坏文件读出了 %q，应当当没记住过", got)
	}
}
