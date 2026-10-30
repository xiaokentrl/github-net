package app

import (
	"path/filepath"
	"testing"

	"github.com/xiaokentrl/github-net/internal/config"
)

func TestSetThemeFromTray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.json")
	ctl := NewThemeController(config.NewStore(path, map[string]bool{}, map[string]bool{}))

	// 合法主题：保存成功且读回一致
	cfg, err := ctl.Set("midnight")
	if err != nil {
		t.Fatalf("切主题不该失败: %v", err)
	}
	if cfg.Appearance.Theme != "midnight" {
		t.Fatalf("主题应已是 midnight，实际 %s", cfg.Appearance.Theme)
	}

	// 同主题再切：不写盘直接返回现状
	if _, err := ctl.Set("midnight"); err != nil {
		t.Fatalf("同主题幂等: %v", err)
	}

	// 非法主题：配置校验拒绝
	if _, err := ctl.Set("neon"); err == nil {
		t.Fatal("不认识的主题该被拒")
	}
}
