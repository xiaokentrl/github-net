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
	// CachedToday 只读报出这些源里今天已经有落盘缓存的 ID，给预览标「会不会真删」。
	CachedToday(srcs []github.Source) []string
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

// SourceCachePreview 是「清理已选缓存源」预览里的一行：源名字，以及今天是否已经下载过。
// Downloaded 为 true 才会被「立即清理」真删掉；false 的今天本来就没有文件，点了也不会动它。
// 预览列全部 N 份、执行只删其中一部分，这个口径差由这一列说清。
type SourceCachePreview struct {
	Name       string `json:"name"`
	Downloaded bool   `json:"downloaded"`
}

// involvedSources 把勾中的项目归并到它们所在的加速范围，再收集这些范围用到的公共源（去重）。
// 只算「要清哪几份源」，不碰磁盘、不删文件；清理和预览共用同一份口径。
// 返回去重后的源列表和去重后的项目数；项目在目录里查不到就整批报错。
func (s *Service) involvedSources(itemIDs []string) ([]github.Source, int, error) {
	if s.productCatalogErr != nil {
		return nil, 0, ToPublicError(s.productCatalogErr)
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
		return nil, 0, nil
	}
	scopes := make([]string, 0, len(ids))
	inScope := make(map[string]bool, len(ids))
	for _, id := range ids {
		_, scopeID, ok := s.findItem(id)
		if !ok {
			return nil, 0, NewError(CodeInvalidItemID, "产品目录里没有这个项目，刷新界面后再试", nil)
		}
		if !inScope[scopeID] {
			inScope[scopeID] = true
			scopes = append(scopes, scopeID)
		}
	}
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
	return sources, len(ids), nil
}

// ClearSourcesCache 清掉勾中项目所涉及的那几份公共源的当天缓存。
// 粒度按「勾了哪个菜单就清那个菜单的源」：项目 → 所在加速范围 → 这个范围用哪几份源，去重后一起清。
// 校验和检测入口同口径——整批先看，一项对不上就整批拒绝、一份都不清，不留「清了一半」的状态。
func (s *Service) ClearSourcesCache(itemIDs []string) (SourcesCacheResult, error) {
	sources, itemCount, err := s.involvedSources(itemIDs)
	if err != nil {
		return SourcesCacheResult{}, err
	}
	if itemCount == 0 {
		return SourcesCacheResult{}, NewError(CodeInvalidItemID, "没有勾中任何项目，这次没有要清的缓存", nil)
	}
	result := SourcesCacheResult{Items: itemCount, Sources: sourceNames(sources), Cleared: []string{}}
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
	result.Cleared = sourceNamesByID(sources, removed)
	if err != nil {
		// 逐份删文件没法整笔回滚：已经删掉的那几份不会退回，必须连同失败一起说清楚，
		// 不能只丢一句「稍后再试」让用户以为一个字都没动。
		msg := "有几份源缓存没能删掉，稍后再试一次"
		if len(result.Cleared) > 0 {
			msg += "；已经删掉的 " + strings.Join(result.Cleared, "、") + " 不会退回"
		}
		return SourcesCacheResult{}, NewError(CodeInternal, msg, err)
	}
	return result, nil
}

// PreviewSourcesCache 只读回「清理已选缓存源」会涉及哪几份公共源，以及每份今天是否已经下载过。
// 不删任何文件。今天没下载过的那份本来就没有可清的文件，预览里要标出来，
// 免得界面列了 N 份、执行只删掉 M 份时看着像漏了一半。
func (s *Service) PreviewSourcesCache(itemIDs []string) ([]SourceCachePreview, error) {
	sources, _, err := s.involvedSources(itemIDs)
	if err != nil {
		return nil, err
	}
	cached := make(map[string]bool, len(sources))
	for _, id := range s.sources().CachedToday(sources) {
		cached[id] = true
	}
	out := make([]SourceCachePreview, 0, len(sources))
	for _, src := range sources {
		out = append(out, SourceCachePreview{Name: src.Name, Downloaded: cached[src.ID]})
	}
	return out, nil
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
