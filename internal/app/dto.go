package app

import (
	"time"

	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/hosts"
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
	// HostsUnknown 表示后端还没读过系统 Hosts。
	// Hosts 只读链路已经接上，后端不再返回这一档；留着是因为它仍是对外契约的一部分，
	// 前端把它和「后端没给状态」一样按「没读过」显示，不许当成一种检测结论。
	HostsUnknown HostsStatus = "unknown"
	// HostsNone 表示已读过，管理区段里没有本工具为该项目的记录。
	// 区段外用户手写的同名行不算本工具记录，所以「系统里已有解析」和这一档可以并存。
	HostsNone HostsStatus = "none"
	// HostsPartial 表示已读过，该项目只有部分 hostname 有本工具记录。
	// 这一档要拿写入摘要比对才能确认，等 Hosts 写入接上才会由后端产出。
	HostsPartial HostsStatus = "partial"
	// HostsApplied 表示已读过，该项目全部 hostname 都有本工具记录且区段摘要校验通过。
	// 同上：没有写入摘要就没有可比对的东西，这一档目前不由后端产出。
	HostsApplied HostsStatus = "applied"
	// HostsConflict 表示区段判读不了（标记异常或被外部改过），
	// 或者区段里有本工具映射却拿不到可比对的写入摘要；后端停在这里，不覆盖、不假装已生效。
	HostsConflict HostsStatus = "conflict"
	// HostsReadFailed 表示读不到 Hosts（文件不存在、没有读权限、路径拿不到等）；
	// 它和 HostsNone 是两回事，不能合并显示。
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

// DNSResolverSystem 表示按隐私规则使用操作系统配置的解析器，不转发给公共 DNS。
const DNSResolverSystem = "system"

// AppStatus 是 GetAppStatus 的返回形状：平台、权限、DNS 服务和应用版本。
// HostsAccess 只报探测结果，不代表本工具已具备写 Hosts 的能力——写入仍要等特权服务阶段。
// DNSDetectionReady 为 false 表示解析器选型已定，但检测任务还没接入。
// ConfigNotice 是配置加载时的用户可见说明，例如原文件损坏已隔离并用默认值运行。
// BuildTime/BuildRevision/BuildModified 是给「关于我们」用的构建事实，来自二进制自带的 VCS 标记：
// 它是构建所用源码的提交时间与哈希，不是打包那一刻的钟表时间，也没有第二个地方能读到。
// 标记没打进来时前两个是空串、第三个是 false，前端显示「未提供」，不许拿当前时间顶上去。
type AppStatus struct {
	App               string      `json:"app"`
	Version           string      `json:"version"`
	OS                string      `json:"os"`
	Arch              string      `json:"arch"`
	HostsPath         string      `json:"hostsPath"`
	HostsAccess       HostsAccess `json:"hostsAccess"`
	DNSResolver       string      `json:"dnsResolver"`
	DNSDetectionReady bool        `json:"dnsDetectionReady"`
	ConfigPath        string      `json:"configPath"`
	ConfigNotice      string      `json:"configNotice,omitempty"`
	BuildTime         string      `json:"buildTime,omitempty"`
	BuildRevision     string      `json:"buildRevision,omitempty"`
	BuildModified     bool        `json:"buildModified"`
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

// HostsStates 把「读了一次 Hosts」的结果摊成每个项目的状态，顺序跟目录一致。
// 现在后端能产出的档位是 none、conflict、readFailed 三种：
// applied 和 partial 要求拿最后一次成功写入的区段摘要来比对，而写入还没接，
// 所以「区段里有本工具映射、却没有可比对的摘要」按冲突报——这是规划定下来的处理，不是猜的。
func HostsStates(c *catalog.Catalog, snap hosts.Snapshot, revision int64) []HostsState {
	out := make([]HostsState, 0)
	readAt := snap.ReadAt.UTC().Format(time.RFC3339)
	status, message := hostsVerdict(snap)
	partialVerdict := status == HostsConflict && snap.Block.Present && snap.Block.Usable()
	for _, cat := range c.Categories {
		for _, item := range cat.Items {
			state := HostsState{
				ItemID:   item.ID,
				Status:   status,
				Message:  message,
				Revision: revision,
				ReadAt:   readAt,
			}
			// 区段能判读时逐个项目看自己有没有映射：没有的就是「已读过、没替过」。
			if partialVerdict && !itemHasRecord(snap.Block, item.Hostnames) {
				state.Status = HostsNone
				state.Message = ""
			}
			out = append(out, state)
		}
	}
	return out
}

// hostsVerdict 给出这一份 Hosts 的整体结论。
// 读不到和区段判读不了是全区一个说法：这两种情况下逐个判断都没有意义。
func hostsVerdict(snap hosts.Snapshot) (HostsStatus, string) {
	switch {
	case !snap.OK():
		return HostsReadFailed, snap.ReadErr
	case snap.Block.Present && !snap.Block.Usable():
		return HostsConflict, snap.Block.Reason
	case !snap.Block.Present:
		return HostsNone, ""
	default:
		return HostsConflict, "区段里有本工具写的映射，但还没有可比对的写入摘要（Hosts 写入还没接），先按冲突处理，不假装已生效。"
	}
}

func itemHasRecord(block hosts.Block, hostnames []string) bool {
	for _, hostname := range hostnames {
		if block.HasRecord(hostname) {
			return true
		}
	}
	return false
}
