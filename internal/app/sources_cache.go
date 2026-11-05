package app

import (
	"strings"

	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/sources/github"
)

// 这一条命令只管一件事：把勾中项目所涉及的那几份公共源的「当天已下载」记录抹掉，
// 下一轮检测就会重新下载源文件（拍板 2026-11-02：源一天只下载一次，清理后才重新下载）。
// 它不删候选底账、不碰 Hosts、不清任务队列——那些各有各的入口，混在这里就说不清「清掉的是什么」。

// sourcesClearer 是「清哪几份源缓存」这一步的接缝。
// 零值就是生产行为：动 github.Default 那一份共用拉源器（检测用的也是它，清完才会真的重下）。
// 留成可注入是为了让这条命令的单测不碰真目录、不发真请求。
// PruneCache 是启动时那一次「超过保留期限的落盘缓存删掉」（拍板 2026-11-05），走的还是同一份共用拉源器。
type sourcesClearer interface {
	ClearCache(srcs []github.Source) ([]string, error)
	CacheEnabled() bool
	PruneCache() error
}

func (s *Service) sources() sourcesClearer {
	if s.sourceCache != nil {
		return s.sourceCache
	}
	return github.Default
}

// SourcesCacheResult 是「清理已选缓存」的回执，只报事实、界面按数字说话。
// 两个数组永远可解成数组（没清到是空数组不是 null），界面无需判空指针。
//
// Sources 是这批勾中项目涉及到的公共源（可读名字）——勾的菜单里没有用公共源的，这里就是空的；
// Cleared 是真删掉当天缓存的那几份。Cleared 比 Sources 少是正常情况：那几份今天还没下载过，本来就没有可清的文件。
type SourcesCacheResult struct {
	Items   int      `json:"items"`
	Sources []string `json:"sources"`
	Cleared []string `json:"cleared"`
}

// ClearSourcesCache 清掉勾中项目所涉及的那几份公共源的当天缓存。
// 粒度按「勾了哪个菜单就清那个菜单的源」：项目 → 所在加速范围 → 这个范围用哪几份源，去重后一起清。
// 校验和检测入口同口径——整批先看，一项对不上就整批拒绝、一份都不清，不留「清了一半」的状态。
func (s *Service) ClearSourcesCache(itemIDs []string) (SourcesCacheResult, error) {
	if s.productCatalogErr != nil {
		return SourcesCacheResult{}, ToPublicError(s.productCatalogErr)
	}

	seen := make(map[string]bool, len(itemIDs))
	ids := make([]string, 0, len(itemIDs))
	for _, raw := range itemIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return SourcesCacheResult{}, NewError(CodeInvalidItemID, "没有勾中任何项目，这次没有要清的缓存", nil)
	}

	// 每个项目归到它所在的加速范围；范围去重，顺序按勾进来的先后。
	scopes := make([]string, 0, len(ids))
	inScope := make(map[string]bool, len(ids))
	for _, id := range ids {
		_, scopeID, ok := s.findItem(id)
		if !ok {
			return SourcesCacheResult{}, NewError(CodeInvalidItemID, "产品目录里没有这个项目，刷新界面后再试", nil)
		}
		if !inScope[scopeID] {
			inScope[scopeID] = true
			scopes = append(scopes, scopeID)
		}
	}

	// 范围 → 这个范围的候选来自哪几份源，去重后就是这次要清的清单。
	sources := make([]github.Source, 0, len(scopes)*3)
	seenSource := make(map[string]bool, len(scopes)*3)
	for _, scopeID := range scopes {
		for _, src := range strategies.SourcesForScope(scopeID) {
			if seenSource[src.ID] {
				continue
			}
			seenSource[src.ID] = true
			sources = append(sources, src)
		}
	}

	result := SourcesCacheResult{Items: len(ids), Sources: sourceNames(sources), Cleared: []string{}}
	if len(sources) == 0 {
		// 勾中的菜单都不用公共源（比如只有 Steam 那几项）：这不是失败，如实回「没得清」。
		return result, nil
	}
	clearer := s.sources()
	if !clearer.CacheEnabled() {
		// 落盘这一层没接上（拿不到配置目录）。这时候说「清掉了 0 份」是骗人的，
		// 真相是这台机器上源缓存根本没落到盘上，界面上要能看到这句。
		return SourcesCacheResult{}, NewError(CodeInternal, "源缓存目录没接上，这次清理没有生效", nil)
	}
	removed, err := clearer.ClearCache(sources)
	if err != nil {
		return SourcesCacheResult{}, NewError(CodeInternal, "有几份源缓存没能删掉，稍后再试一次", err)
	}
	result.Cleared = sourceNamesByID(sources, removed)
	return result, nil
}

func sourceNames(srcs []github.Source) []string {
	out := make([]string, 0, len(srcs))
	for _, src := range srcs {
		out = append(out, src.Name)
	}
	return out
}

// sourceNamesByID 把清理返回的源 ID 换成可读名字，顺序跟着清单走——
// 删文件的先后是磁盘给的，按源清单排序才不会同一次操作两次报出不同的顺序。
func sourceNamesByID(srcs []github.Source, ids []string) []string {
	removed := make(map[string]bool, len(ids))
	for _, id := range ids {
		removed[id] = true
	}
	out := make([]string, 0, len(ids))
	for _, src := range srcs {
		if removed[src.ID] {
			out = append(out, src.Name)
		}
	}
	return out
}
