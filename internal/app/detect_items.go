package app

import (
	"strings"

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
func (s *Service) DetectItems(itemIDs []string) ([]task.Task, error) {
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

	// 先整批校验。每个项目校两件事：目录里有它，它所属的范围有可用的默认方法。
	// 方法的读取和执行器判定链同源（同一个配置快照、同一张注册表），
	// 这里过了、执行时再拒只剩「配置刚被改掉」这一种可能，那时任务会以失败落进队列被看见。
	cfg, err := s.settings.Snapshot()
	if err != nil {
		return nil, ToPublicError(err)
	}
	type pending struct {
		id      string
		name    string
		scopeID string
	}
	batch := make([]pending, 0, len(ids))
	for _, id := range ids {
		item, scopeID, ok := s.findItem(id)
		if !ok {
			return nil, NewError(CodeInvalidItemID, "产品目录里没有这个项目，刷新界面后再试", nil)
		}
		strategyID := strings.TrimSpace(cfg.Accelerator.DefaultStrategyByScope[scopeID])
		if strategyID == "" {
			return nil, NewError(CodeNoStrategySelected,
				"这个项目所在的服务还没选检测方法，先在详情顶部的加速方法区选一个", nil)
		}
		strategy, ok := s.strategyTable.lookup(strategyID)
		if !ok {
			return nil, NewError(CodeNoStrategySelected,
				"之前选的那个检测方法已经不在注册表里，重新选一个方法再检测", nil)
		}
		if !strategyApplies(strategy, scopeID) {
			return nil, NewError(CodeNoStrategySelected,
				"选的方法不适用于这个项目所在的服务，换一个方法再检测", nil)
		}
		batch = append(batch, pending{id: id, name: item, scopeID: scopeID})
	}

	tasks := make([]task.Task, 0, len(batch))
	for _, p := range batch {
		t, err := s.tasks.Enqueue(task.NewRequest{Kind: task.KindDetect, ItemID: p.id, Name: p.name, ScopeID: p.scopeID})
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

// findItem 按项目 ID 取显示名和所属加速范围。目录是唯一来源，这里不做第二份索引：
// 目录规模是几十个条目，线性找足够快，先不建索引制造第二份要同步的东西。
func (s *Service) findItem(itemID string) (name string, scopeID string, ok bool) {
	if s.productCatalog == nil {
		return "", "", false
	}
	for _, category := range s.productCatalog.Categories {
		for _, item := range category.Items {
			if item.ID == itemID {
				return item.Name, category.ScopeID, true
			}
		}
	}
	return "", "", false
}
