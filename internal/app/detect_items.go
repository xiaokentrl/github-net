package app

import (
	"strings"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/task"
)

// detectItemsEntry 是排检测任务的唯一入口。
//
// 入口和执行器（detectExecutor）必须同轮放行：只换装执行器不接入口，界面什么都不变，
// 却等于悄悄打开真实网络；这里挡住的是「没选方法就想测」的那一半。
// 入口做三轮校验（目录里有这个项目、这个范围已经选了默认方法、方法真的可用），
// 校验过才排队；执行时的判定链在 detect.go，两边共用同一套拒绝理由。

// DetectItems 把指定项目排进检测队列，返回它们的任务快照。
// 同一个项目已有等待中/执行中的同任务时直接返回已在的那条（队列去重），
// 所以连点两次「检测」不会出现两条一样的任务，也不会报错。
//
// 校验全部通过才开始排队：一项不合格就整批拒绝、什么都不排，
// 避免「测了三个、第四个因为没选方法被拒」这种半截状态。
// DetectItems 排一批检测任务。strategyID 给空就按各项目范围的默认方法来
// （配置里存的选择 → 出厂默认「全部公共源」，口径和执行器一致）；
// 给了就整批用它——选择区那颗「检测」按钮只检测下拉选中的那一个方法。
func (s *Service) DetectItems(itemIDs []string, strategyID string) ([]task.Task, error) {
	if s.productCatalogErr != nil {
		return nil, ToPublicError(s.productCatalogErr)
	}
	if s.settings == nil {
		return nil, s.settingsFailure()
	}

	ids := make([]string, 0, len(itemIDs))
	seen := make(map[string]bool, len(itemIDs))
	for _, raw := range itemIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, NewError(CodeInvalidItemID, "没有要检测的项目", nil)
	}

	// 先整批校验。每个项目校两件事：目录里有它，它所属的范围有可用的方法。
	// 方法的读取和执行器判定链同源（同一个配置快照、同一张注册表），
	// 这里过了、执行时再拒只剩「配置刚被改掉」这一种可能，那时任务会以失败落进队列被看见。
	cfg, err := s.settings.Snapshot()
	if err != nil {
		return nil, ToPublicError(err)
	}
	pinned := strings.TrimSpace(strategyID)
	if pinned != "" {
		if _, ok := s.strategyTable.lookup(pinned); !ok {
			return nil, NewError(CodeNoStrategySelected,
				"要检测的方法已经不在注册表里，换一个方法再检测", nil)
		}
	}
	batch := make([]detectCandidate, 0, len(ids))
	for _, id := range ids {
		item, scopeID, ok := s.findItem(id)
		if !ok {
			return nil, NewError(CodeInvalidItemID, "产品目录里没有这个项目，刷新界面后再试", nil)
		}
		used := pinned
		if used == "" {
			used = strings.TrimSpace(cfg.Accelerator.DefaultStrategyByScope[scopeID])
		}
		if used == "" {
			used, _ = factoryDefaultStrategy(scopeID)
		}
		if used == "" {
			return nil, NewError(CodeNoStrategySelected,
				"这个项目所在的服务还没选检测方法，先在详情顶部的加速方法区选一个", nil)
		}
		strategy, ok := s.strategyTable.lookup(used)
		if !ok {
			return nil, NewError(CodeNoStrategySelected,
				"之前选的那个检测方法已经不在注册表里，重新选一个方法再检测", nil)
		}
		if !strategyApplies(strategy, scopeID) {
			return nil, NewError(CodeNoStrategySelected,
				"选的方法不适用于这个项目所在的服务，换一个方法再检测", nil)
		}
		batch = append(batch, detectCandidate{id: id, name: item, scopeID: scopeID, strategyID: used})
	}

	// 一次入口调用就是一轮：这一批项目共享一份候选探测预算。
	// 预算 = min(本轮唯一域名数 × 档位每域名候选上限, 用户总上限)，账本建在轮上。
	// 轮的 ID 和名字都来自队列：ID 只有队列会发号，预算账本按同一个 ID 记账。
	roundName, roundScope := detectRoundLabel(batch, s.scopeName)
	roundID := s.tasks.BeginRound(task.KindDetect, roundScope, roundName)
	if s.rounds != nil {
		s.rounds.put(roundID, s.detectRoundBudget(cfg, batch))
	}
	tasks := make([]task.Task, 0, len(batch))
	for _, p := range batch {
		t, err := s.tasks.Enqueue(task.NewRequest{Kind: task.KindDetect, ItemID: p.id, Name: p.name, ScopeID: p.scopeID, StrategyID: p.strategyID, RoundID: roundID})
		if err != nil {
			// 排到一半失败只剩「应用正在退出」这一种：队列关了。前面已排进去的是有效任务，
			// 会随队列关闭被取消，不静默装作整批成功。
			return nil, ToPublicError(err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// GetDetectResult 返回一个项目最近一轮检测的候选结论；没有记录时给空形状，
// 「还没测过」不是错误。详情页先进这里拿现状，再用 detect:progress 事件追更新。
func (s *Service) GetDetectResult(itemID string) (DetectResultView, error) {
	id := strings.TrimSpace(itemID)
	if id == "" {
		return DetectResultView{}, NewError(CodeInvalidItemID, "项目 ID 是空的，没有要查哪一条", nil)
	}
	if view, ok := s.detectResults.view(id); ok {
		return view, nil
	}
	return detectResultEmptyView(id), nil
}

// lookupItem 按项目 ID 取显示名、所属加速范围和完整条目。
// 目录是唯一来源，这里不做第二份索引：目录规模是几十个条目，线性找足够快，
// 先不建索引制造第二份要同步的东西。
func (s *Service) lookupItem(itemID string) (name string, scopeID string, item catalog.Item, ok bool) {
	if s.productCatalog == nil {
		return "", "", catalog.Item{}, false
	}
	for _, category := range s.productCatalog.Categories {
		for _, it := range category.Items {
			if it.ID == itemID {
				return it.Name, category.ScopeID, it, true
			}
		}
	}
	return "", "", catalog.Item{}, false
}

// findItem 只要名字和范围的旧形状，给不需要域名清单的调用方。
func (s *Service) findItem(itemID string) (name string, scopeID string, ok bool) {
	name, scopeID, _, ok = s.lookupItem(itemID)
	return name, scopeID, ok
}

// detectCandidate 是入口校验后待排队的项目。
type detectCandidate struct {
	id         string
	name       string
	scopeID    string
	strategyID string
}

// detectRoundBudget 算这一轮共享的候选探测预算。
// 公式来自技术规划：min(本轮唯一目标域名数 × 档位每域名候选上限, 用户总上限)。
func (s *Service) detectRoundBudget(cfg config.Config, batch []detectCandidate) int {
	hostnames := make(map[string]bool)
	for _, p := range batch {
		_, _, item, ok := s.lookupItem(p.id)
		if !ok {
			continue
		}
		for _, h := range uniqueHostnames(item.Hostnames) {
			hostnames[h] = true
		}
	}
	count := len(hostnames)
	if count == 0 {
		return 0
	}
	b, err := accelerator.BudgetFor(cfg, count)
	if err != nil {
		// 预算算不出来就给最保守的一份：只够一个域名按档位走，别放开闸。
		return 1
	}
	return b.MaxProbes
}

// detectRoundLabel 定这一轮在队列里叫什么、属于哪个范围，规则照原型：
// 根节点按一级菜单分组，名字就是那个分类名。一次入口调用只覆盖一个范围时用分类名；
// 真跨了范围（现在的界面入口不会产生，服务检测和单项检测都只给一个范围）退回「批量检测」，
// 范围留空——宁可根节点少一句归属说明，也不把两个分类名拼成一条谁也对不上的句子。
func detectRoundLabel(batch []detectCandidate, scopeName func(string) string) (name string, scopeID string) {
	scopeID = ""
	for _, p := range batch {
		if scopeID == "" {
			scopeID = p.scopeID
			continue
		}
		if p.scopeID != scopeID {
			return "批量检测", ""
		}
	}
	if scopeID == "" {
		return "批量检测", ""
	}
	if n := scopeName(scopeID); n != "" {
		return n, scopeID
	}
	return "批量检测", scopeID
}

// scopeName 按加速范围 ID 取分类显示名；范围不在目录里就给空串，由调用方兜底。
func (s *Service) scopeName(scopeID string) string {
	if s.productCatalog == nil || scopeID == "" {
		return ""
	}
	for _, category := range s.productCatalog.Categories {
		if category.ScopeID == scopeID {
			return category.Name
		}
	}
	return ""
}

// DetectBudgetPreview 是「这批项目一起测要多少预算」的预估，给界面在启动前展示和确认。
type DetectBudgetPreview struct {
	// Needed 是盖住全部项目理论上要的探测次数 = 本轮唯一域名数 × 档位每域名候选上限。
	Needed int `json:"needed"`
	// Budget 是这一轮实际可用的预算 = min(Needed, 用户总上限)。
	Budget int `json:"budget"`
	// Shortfall 大于 0 表示预算盖不全：会有这么多探测排不上，对应项目标「预算不足，未检测」。
	Shortfall int `json:"shortfall"`
}

// DetectBudget 预估一批项目的共享预算，不排队、不发起检测。
// 界面在 Shortfall > 0 时先请用户确认，再调 DetectItems。
func (s *Service) DetectBudget(itemIDs []string) (DetectBudgetPreview, error) {
	if s.productCatalogErr != nil {
		return DetectBudgetPreview{}, ToPublicError(s.productCatalogErr)
	}
	if s.settings == nil {
		return DetectBudgetPreview{}, s.settingsFailure()
	}
	cfg, err := s.settings.Snapshot()
	if err != nil {
		return DetectBudgetPreview{}, ToPublicError(err)
	}
	seen := make(map[string]bool)
	hostnames := make(map[string]bool)
	for _, raw := range itemIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		_, _, item, ok := s.lookupItem(id)
		if !ok {
			return DetectBudgetPreview{}, NewError(CodeInvalidItemID, "产品目录里没有这个项目，刷新界面后再试", nil)
		}
		for _, h := range uniqueHostnames(item.Hostnames) {
			hostnames[h] = true
		}
	}
	count := len(hostnames)
	if count == 0 {
		return DetectBudgetPreview{}, nil
	}
	b, err := accelerator.BudgetFor(cfg, count)
	if err != nil {
		return DetectBudgetPreview{}, ToPublicError(err)
	}
	return DetectBudgetPreview{Needed: b.Needed, Budget: b.MaxProbes, Shortfall: b.Needed - b.MaxProbes}, nil
}
