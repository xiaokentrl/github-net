package app

import (
	"context"
	"fmt"

	"github.com/xiaokentrl/github-net/internal/task"
)

// 单项「应用」按钮的流水线（拍板 2026-11-03）：应用 = 自动预检 + 写入。
// 用户只理解两个词：应用（让它生效）、还原（退回去）；检测内置在应用里做。
//
// 链路：排检测轮（strategyID 空串=按范围保存的方法执行时现解析，重试自然用当前源）
//   → WaitRound 等该轮收口 → 有可用候选才排应用轮；没有就排一条「跳过」的应用任务，
//     原因随任务进队列和右下角面板。
//
// 冲突规则：
//   - 该项目已有检测任务在排队/执行：不重复排轮，挂到那个轮上等收口再接应用（挂起应用意图）
//   - 该项目已有应用/还原任务在排队/执行：拒绝（防重复写入轮）
//   - 同一项已有一条智能流水线在等：拒绝（防双链）

// activeTaskFor 找该项目当前还没落定的任务；没有给 nil。
func (s *Service) activeTaskFor(itemID string) *task.Task {
	snap := s.tasks.Snapshot()
	for i, tk := range snap.Tasks {
		if tk.ItemID == itemID && !task.IsTerminal(tk.Status) {
			t := snap.Tasks[i]
			return &t
		}
	}
	return nil
}

// activeDetectRoundOf 找该项目在排队/执行的检测任务所属的轮；没有给空串。
func (s *Service) activeDetectRoundOf(itemID string) string {
	snap := s.tasks.Snapshot()
	for _, tk := range snap.Tasks {
		if tk.ItemID == itemID && tk.Kind == task.KindDetect && (tk.Status == task.StatusWaiting || tk.Status == task.StatusRunning) {
			return tk.RoundID
		}
	}
	return ""
}

// ApplyItemSmart 单项应用入口：检测（或挂到正在跑的检测）→ 收口后写入最优可用。
// strategyID 给空按范围默认；给了就整条链用这一个方法。
func (s *Service) ApplyItemSmart(itemID, strategyID string) ([]task.Task, error) {
	if s.productCatalogErr != nil {
		return nil, ToPublicError(s.productCatalogErr)
	}
	if s.settings == nil {
		return nil, s.settingsFailure()
	}
	if _, _, ok := s.findItem(itemID); !ok {
		return nil, NewError(CodeInvalidItemID, "产品目录里没有这个项目，刷新界面后再试", nil)
	}
	if active := s.activeTaskFor(itemID); active != nil {
		return nil, NewError(CodeQueueRunning, fmt.Sprintf("这个项目有一条%s任务还没落定，等它结束再操作", taskKindName(active.Kind)), nil)
	}

	var detectRound string
	if existing := s.activeDetectRoundOf(itemID); existing != "" {
		// 已有检测在跑（多半是 ⋯ 里的单独检测）：不重复排轮，挂起应用意图等它收口。
		detectRound = existing
	} else {
		tasks, err := s.DetectItems([]string{itemID}, strategyID)
		if err != nil {
			return nil, err
		}
		detectRound = tasks[0].RoundID
	}

	go s.runApplyPipeline(itemID, detectRound)
	return nil, nil
}

// runApplyPipeline 等检测轮收口，然后按结果接应用：有可用候选排应用轮，
// 没有就排一条跳过的应用任务（原因随任务进队列和面板，任务到终态时前端会递进右下角）。
// 进程退出时 WaitRound 返回错误， goroutine 安静收场——流水线不跨进程续跑。
func (s *Service) runApplyPipeline(itemID, detectRound string) {
	ctx := context.Background()
	if err := s.tasks.WaitRound(ctx, detectRound); err != nil {
		return
	}
	if len(s.usableMappingsFor(itemID)) == 0 {
		s.enqueueSkippedApply(itemID, "检测完成：没有可用的候选地址，未写入")
		return
	}
	_, _ = s.ApplyItems([]string{itemID})
}

// enqueueSkippedApply 排一条以「跳过」落定的应用任务：流水线走到头却没有产出时，
// 队列和右下角面板都要有一行说得清的记录，而不是静默消失。
func (s *Service) enqueueSkippedApply(itemID, reason string) {
	name, scope, _ := s.findItem(itemID)
	roundID := s.tasks.BeginRound(task.KindApply, scope, applyRoundName(1))
	req := task.NewRequest{Kind: task.KindApply, ItemID: itemID, Name: name, ScopeID: scope, RoundID: roundID}
	if _, err := s.tasks.EnqueueSkipped(req, reason); err != nil {
		return
	}
}

// taskKindName 把任务类型翻成用户能读懂的一个词（按钮/提示用）。
func taskKindName(kind task.Kind) string {
	switch kind {
	case task.KindDetect:
		return "检测"
	case task.KindApply:
		return "应用"
	case task.KindRestore:
		return "还原"
	}
	return "任务"
}
