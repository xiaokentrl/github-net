package app

import (
	"github.com/xiaokentrl/github-net/internal/catalog"
)

// RuntimeInfo 是后端交给前端的真实运行环境信息，不含任何演示数据。
type RuntimeInfo struct {
	AppName        string `json:"appName"`
	Version        string `json:"version"`
	OS             string `json:"os"`
	Arch           string `json:"arch"`
	StartedAt      string `json:"startedAt"`
	StatusSequence int64  `json:"statusSequence"`
}

// 目录对外形状与内部数据分开：内部字段（原型行号等审计信息）不出现在这里，
// JSON 标签统一用前端习惯的小驼峰 id，跟 RuntimeInfo 的 appName 同一风格。

// CatalogCategory 是一个服务分类；ScopeID 是它共享的加速范围。
type CatalogCategory struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	ScopeID string        `json:"scopeId"`
	Order   int           `json:"order"`
	Items   []CatalogItem `json:"items"`
}

// CatalogItem 是一个可单独检测的项目；Hostnames 永远是数组，没有预设域名时是空数组而不是 null。
type CatalogItem struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Order        int      `json:"order"`
	Hostnames    []string `json:"hostnames"`
	UserSupplied bool     `json:"userSupplied"`
}

// HostsStatus 是后端读出来的真实 Hosts 状态。前端只能显示这里的值，不能自己推断。
type HostsStatus string

const (
	// HostsUnknown 表示后端还没读过系统 Hosts。当前阶段只能返回这一档。
	HostsUnknown HostsStatus = "unknown"
	// HostsNone 表示已读过，管理区里没有本工具为该项目的记录。
	HostsNone HostsStatus = "none"
	// HostsPartial 表示已读过，该项目只有部分 hostname 有本工具记录。
	HostsPartial HostsStatus = "partial"
	// HostsApplied 表示已读过，该项目全部 hostname 都有本工具记录且区段摘要校验通过。
	HostsApplied HostsStatus = "applied"
	// HostsConflict 表示管理区被外部改动或摘要不匹配，后端已停止写入并保留现场。
	HostsConflict HostsStatus = "conflict"
	// HostsReadFailed 表示读不到 Hosts（权限不足、文件占用等）；它和 HostsNone 是两回事，不能合并显示。
	HostsReadFailed HostsStatus = "readFailed"
)

// HostsState 是某个项目的 Hosts 状态快照。
// Revision 单调递增，前端只接受更高的值；Message 只在冲突和读取失败时给出可展示原因。
type HostsState struct {
	ItemID   string      `json:"itemId"`
	Status   HostsStatus `json:"status"`
	Revision int64       `json:"revision"`
	ReadAt   string      `json:"readAt"`
	Message  string      `json:"message,omitempty"`
}

// ItemsSnapshot 是 GetItems 的返回形状：目录 + 每项的真实 Hosts 状态。
// 目录部分来自 internal/catalog，状态部分只能由后端 Hosts 读取服务填写。
type ItemsSnapshot struct {
	Categories []CatalogCategory `json:"categories"`
	Hosts      []HostsState      `json:"hosts"`
}

// CatalogToCategories 把内部目录转成对外形状，保留原型显示顺序，并保证数组字段不为 null。
func CatalogToCategories(c *catalog.Catalog) []CatalogCategory {
	out := make([]CatalogCategory, 0, len(c.Categories))
	for _, cat := range c.Categories {
		items := make([]CatalogItem, 0, len(cat.Items))
		for _, item := range cat.Items {
			hostnames := make([]string, 0, len(item.Hostnames))
			hostnames = append(hostnames, item.Hostnames...)
			items = append(items, CatalogItem{
				ID:           item.ID,
				Name:         item.Name,
				Order:        item.Order,
				Hostnames:    hostnames,
				UserSupplied: item.UserSupplied,
			})
		}
		out = append(out, CatalogCategory{
			ID:      cat.ID,
			Name:    cat.Name,
			ScopeID: cat.ScopeID,
			Order:   cat.Order,
			Items:   items,
		})
	}
	return out
}

// UnknownHosts 给目录里的每个项目生成「后端还没读过」的状态占位，不伪装成已读结果。
func UnknownHosts(c *catalog.Catalog) []HostsState {
	out := make([]HostsState, 0)
	for _, cat := range c.Categories {
		for _, item := range cat.Items {
			out = append(out, HostsState{ItemID: item.ID, Status: HostsUnknown})
		}
	}
	return out
}
