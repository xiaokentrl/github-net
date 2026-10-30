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
	// HostsPartial 表示已读过，该项目只有部分 hostname 是它自己认领的、且区段摘要对得上的映射。
	HostsPartial HostsStatus = "partial"
	// HostsApplied 表示已读过，该项目全部 hostname 都由它认领，且区段摘要与最后一次成功写入一致。
	HostsApplied HostsStatus = "applied"
	// HostsConflict 表示没法确认这段映射是不是本工具写的：区段判读不了、被外部改过、
	// 或拿不到特权服务记录的写入摘要。后端停在这里，不覆盖、不假装已生效。
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

// AppliedState 是「最后一次成功写入」的读侧结论：知道，或者不知道且带原因。
// 写入记录在特权服务那边（root 目录里），界面进程自己读不到，所以每次读目录都问一次。
type AppliedState struct {
	Known   bool
	Written hosts.Written
	Reason  string
}

// HostsStates 把「读了一次 Hosts」和「问了一次写入记录」摊成每个项目的状态，顺序跟目录一致。
// 结论分两类：
//   - 全区一个说法：读不到、区段判读不了——这些情况下逐个判断没有意义；
//   - 区段能读时就按项目看：区段和写入记录对得上时，按项目认领了哪些主机名分
//     applied / partial / none；对不上时（被外部改过、或问不着写入记录），区段里有这个项目
//     记录才报 conflict，一个记录都没有的仍是 none——没写过就是没写过，不跟着一起报警。
func HostsStates(c *catalog.Catalog, snap hosts.Snapshot, applied AppliedState, revision int64) []HostsState {
	out := make([]HostsState, 0)
	readAt := snap.ReadAt.UTC().Format(time.RFC3339)
	status, message := hostsVerdict(snap, applied)
	// 对得上：按认领关系逐项分档。
	byOwnership := status == ""
	// 对不上但区段读得懂：区段里有这个项目的记录才报冲突。
	byRecord := status == HostsConflict && snap.Block.Present && snap.Block.Usable()
	for _, cat := range c.Categories {
		for _, item := range cat.Items {
			state := HostsState{
				ItemID:   item.ID,
				Status:   status,
				Message:  message,
				Revision: revision,
				ReadAt:   readAt,
			}
			switch {
			case byOwnership:
				covered, total := itemCoverage(applied.Written.Entries, item.ID, item.Hostnames)
				switch {
				case covered == 0 || total == 0:
					state.Status = HostsNone
					state.Message = ""
				case covered == total:
					state.Status = HostsApplied
				default:
					state.Status = HostsPartial
				}
			case byRecord:
				if !itemHasRecord(snap.Block, item.Hostnames) {
					state.Status = HostsNone
					state.Message = ""
				}
			}
			// 拍板 1b（区段外同名行照实写入、只提示）：区段之前的手写行里如果有同名的
			// 主机名，写入的映射会被它挡住、实际不生效——状态仍是「已应用」，但把事实说清。
			if (state.Status == HostsApplied || state.Status == HostsPartial) && itemShadowsOutside(snap, item.Hostnames) {
				state.Message = "区段外有同名的手写行，这条映射被它挡住、暂不生效"
			}
			out = append(out, state)
		}
	}
	return out
}

// itemCoverage 数这个项目的主机名里有几条是它自己认领的工具映射。
// 认领关系从写入记录来，不从区段猜：同一个主机名的 IP 可能被别的项目改写过，
// 那时区段里有记录，但已经不是这个项目的映射了。
func itemCoverage(entries []hosts.Entry, itemID string, hostnames []string) (covered, total int) {
	owned := make(map[string]bool, len(entries))
	for _, e := range entries {
		for _, owner := range e.Owners {
			if owner == itemID {
				owned[e.Hostname] = true
				break
			}
		}
	}
	seen := make(map[string]bool, len(hostnames))
	for _, h := range hostnames {
		name := hosts.NormalizeHostname(h)
		if seen[name] {
			continue
		}
		seen[name] = true
		total++
		if owned[name] {
			covered++
		}
	}
	return covered, total
}

// itemShadowsOutside 判断这个项目的域名里有没有被区段外手写行挡住的。
// 只在区段可判读时有意义；Shadowed 为空时 O(1) 返回。
func itemShadowsOutside(snap hosts.Snapshot, hostnames []string) bool {
	if len(snap.Shadowed) == 0 {
		return false
	}
	for _, h := range hostnames {
		if snap.Shadowed[hosts.NormalizeHostname(h)] {
			return true
		}
	}
	return false
}

// hostsVerdict 给出这一份 Hosts 的整体结论。空状态表示区段和写入记录对得上，
// 由调用方按项目各自认领了哪些主机名去分 applied / partial / none。
// 读不到和区段判读不了是全区一个说法：这两种情况下逐个判断都没有意义。
func hostsVerdict(snap hosts.Snapshot, applied AppliedState) (HostsStatus, string) {
	if !snap.OK() {
		return HostsReadFailed, snap.ReadErr
	}
	if snap.Block.Present && !snap.Block.Usable() {
		return HostsConflict, snap.Block.Reason
	}
	if !applied.Known {
		// 服务问不着时只知道「文件里有没有区段」，判不了它是不是本工具最后写的那批。
		// 没有区段仍是 none；有区段就按冲突报，不假装已生效。
		if !snap.Block.Present {
			return HostsNone, ""
		}
		return HostsConflict, "拿不到特权服务记录的写入摘要（" + applied.Reason + "），没法确认这段映射是不是本工具写的，先按冲突处理。"
	}
	switch check, reason := hosts.CheckBlock(snap.Block, applied.Written); check {
	case hosts.CheckMatched:
		return "", ""
	case hosts.CheckConflict:
		return HostsConflict, reason
	default:
		return HostsNone, ""
	}
}

// itemHasRecord 报告这个项目的域名里有没有出现在管理区段里的。
// 区段读得懂、但没法确认是不是本工具写的那批时，用它决定哪些项目该报冲突。
func itemHasRecord(block hosts.Block, hostnames []string) bool {
	for _, hostname := range hostnames {
		if block.HasRecord(hostname) {
			return true
		}
	}
	return false
}
