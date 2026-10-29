// Package platform 是跨平台差异的唯一适配层：自启动、系统路径这类「每个系统一套做法」的能力。
//
// 约定：业务包只调本包的稳定接口，不自己判断平台路径、不起系统命令。
// 每个能力一个文件，平台差异用 Go 的文件后缀隔离（autostart_linux.go 等）；
// 某平台暂时做不到的，返回明确的「不支持」错误，绝不静默假装成功。
package platform

import "errors"

// ErrUnsupported 表示当前平台没有实现这个能力。界面拿到它要给出实话，
// 不能把它和「失败」混成一句话。
var ErrUnsupported = errors.New("当前平台不支持这个能力")

// Autostart 开关：开机自启动一个桌面入口。
// enabled 为 true 时写入（或更新）自启动项；false 时移除，本来就没有也算成功。
type Autostart interface {
	Enable() error
	Disable() error
	// Enabled 报告当前状态；读不到状态按未启用处理并把原因附上。
	Enabled() (bool, error)
}
