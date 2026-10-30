package app

import (
	"fmt"
	"time"

	"github.com/xiaokentrl/github-net/internal/config"
)

// ThemeController 是托盘菜单专用的主题切换入口：不注册为 Wails 服务，
// 前端拿不到它——界面切主题只能走 SaveSettings 的 patch 链（带 revision 和幂等键），
// 托盘走这里（后端内部调用，拿不到前端的 revision，用当前快照的 revision 走同一条保存链）。
// 两条路最后都进 config.Store.Save，校验和落盘纪律完全一样。
type ThemeController struct {
	settings *config.Store
}

func NewThemeController(settings *config.Store) *ThemeController {
	return &ThemeController{settings: settings}
}

// Set 切主题：同主题不写盘直接返回现状；成功返回保存后的完整配置。
func (c *ThemeController) Set(themeID string) (config.Config, error) {
	cfg, err := c.settings.Snapshot()
	if err != nil {
		return config.Config{}, ToPublicError(err)
	}
	if cfg.Appearance.Theme == themeID {
		return cfg, nil
	}
	patch := config.Patch{Theme: &themeID}
	mutationID := fmt.Sprintf("tray-theme-%s-%d", themeID, time.Now().UnixNano())
	out, err := c.settings.Save(patch, cfg.Revision, mutationID)
	if err != nil {
		return config.Config{}, ToPublicError(err)
	}
	return out, nil
}
