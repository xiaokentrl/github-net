package event

import (
	"github.com/xiaokentrl/github-net/internal/config"
)

// SettingsChangedName 是「配置被后端自己改了」事件的名字（目前唯一来源：托盘菜单切主题）。
// 前端收到后用里面的配置整体刷新（语言/主题/缩放走 applyConfig 同一条路）。
const SettingsChangedName = "settings:changed"

// SettingsChanged 是托盘改主题后推给前端的事件载荷。失败不发事件——
// 托盘是后端自己的动作，失败只写日志，界面保持旧值即可。
type SettingsChanged struct {
	Config config.Config `json:"config"`
}
