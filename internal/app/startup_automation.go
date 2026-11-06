package app

import (
	"context"
	"errors"
	"strings"

	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 启动自动化：开机起来后按设置里勾的主菜单跑一遍检测，再按开关自动应用，
// 应用那一轮收口后可以真退出进程。三个开关都在配置里，一个菜单都没勾＝这条链没开。
//
// 这条链是包级函数、不是 Service 的方法：Wails 会把 Service 的每个导出方法绑成前端
// 能调的命令，而「重跑一遍启动自动化」不该是界面点得动的东西——它只在进程开局跑一次。

// automationBatch 是一个主菜单在这一轮里要检测的项目。
type automationBatch struct {
	MenuID  string
	ItemIDs []string
}

// automationPlan 是开局要知道的三件事：三个开关、按菜单分好组的项目、以及读它们时有没有出错。
type automationPlan struct {
	Startup config.StartupConfig
	// Batches 只装「勾了、而且菜单里有可检测项目」的那些菜单。
	// 按菜单分组是因为一次 DetectItems 就是一轮：队列根节点本来就按一级菜单分组，
	// 而且某一个范围没选加速方法时，只拒它自己那一批，不牵连别的菜单。
	Batches []automationBatch
}

// automationReport 是「能不能退出」这一门前要从队列读到的三个数。
type automationReport struct {
	// AppliedOK 是给定这几轮里应用成功的条数。
	AppliedOK int
	// Active 是这几轮里还没跑完的条数；WaitRound 正常返回时它应该是 0，读出来只为把话说实。
	Active int
	// OtherActive 是队列里另外还挂着的在途任务——用户自己点的检测、恢复这一类。
	// 有它们就说明此刻退出会把别人的活打断，这条链不代为决定。
	OtherActive int
}

// automationDeps 是这条链对外的全部接缝。装配层给真的，测试给假的；
// 链本身只管顺序和判定，不碰配置目录、不碰网络、不碰 Hosts。
type automationDeps struct {
	plan   func() (automationPlan, error)
	detect func(itemIDs []string) ([]string, error)
	apply  func(itemIDs []string) ([]string, int, error)
	wait   func(ctx context.Context, roundID string) error
	report func(roundIDs []string) automationReport
	quit   func()
	status func(status event.AutomationStatus)
}

// runStartupAutomation 把这条链走一遍。每一步都先问一次 ctx：进程正在退出时
// 既不该再往总线上发状态，也不该在退出流程里抢着再排一批任务。
func runStartupAutomation(ctx context.Context, d automationDeps) {
	say := func(status event.AutomationStatus) {
		if ctx.Err() != nil {
			return
		}
		d.status(status)
	}
	plan, err := d.plan()
	if err != nil {
		say(event.AutomationStatus{
			Stage:  event.AutomationStageFailed,
			Reason: event.AutomationReasonPlanFailed,
			Detail: unwrapMessage(err),
		})
		return
	}
	menus := plan.Startup.AutoDetectMenus
	if len(menus) == 0 {
		// 一个菜单都没勾＝这条没开：不发状态、不排队，界面上不留一行「没启动」的噪音。
		return
	}
	if ctx.Err() != nil {
		return
	}
	if len(plan.Batches) == 0 {
		say(event.AutomationStatus{
			Stage:  event.AutomationStageSkipped,
			Reason: event.AutomationReasonNoItems,
			Menus:  menus,
		})
		return
	}

	accepted := make([]string, 0, len(plan.Batches))
	rejected := make([]string, 0, len(plan.Batches))
	var detail strings.Builder
	var rounds []string
	detectCount := 0
	for _, batch := range plan.Batches {
		roundIDs, err := d.detect(batch.ItemIDs)
		if err != nil {
			// 一个菜单被拒（多半是那个范围还没选加速方法）不牵连别的菜单：
			// 整批一起拒才是这里最要避免的「一个没选、全都白跑」。
			rejected = append(rejected, batch.MenuID)
			if detail.Len() > 0 {
				detail.WriteString("；")
			}
			detail.WriteString(batch.MenuID + "：" + unwrapMessage(err))
			continue
		}
		accepted = append(accepted, batch.MenuID)
		rounds = append(rounds, roundIDs...)
		detectCount += len(batch.ItemIDs)
	}
	if len(rounds) == 0 {
		say(event.AutomationStatus{
			Stage:         event.AutomationStageFailed,
			Reason:        event.AutomationReasonDetectRejected,
			Detail:        detail.String(),
			RejectedMenus: rejected,
		})
		return
	}
	say(event.AutomationStatus{
		Stage:         event.AutomationStageDetecting,
		Menus:         accepted,
		RejectedMenus: rejected,
		DetectCount:   detectCount,
	})

	if err := waitAll(ctx, d.wait, rounds); err != nil {
		waitReason, waitDetail := waitFailure(err)
		if waitReason == "" {
			return // 进程正在退出，这时候的失败没人看
		}
		say(event.AutomationStatus{
			Stage:         event.AutomationStageFailed,
			Reason:        waitReason,
			Detail:        waitDetail,
			Menus:         accepted,
			RejectedMenus: rejected,
			DetectCount:   detectCount,
		})
		return
	}

	if !plan.Startup.AutoApplyOnStartup {
		say(event.AutomationStatus{
			Stage:         event.AutomationStageDone,
			Menus:         accepted,
			RejectedMenus: rejected,
			DetectCount:   detectCount,
		})
		return
	}

	allItems := make([]string, 0, detectCount)
	for _, batch := range plan.Batches {
		allItems = append(allItems, batch.ItemIDs...)
	}
	applyRounds, queued, err := d.apply(allItems)
	if err != nil {
		say(event.AutomationStatus{
			Stage:         event.AutomationStageFailed,
			Reason:        event.AutomationReasonApplyRejected,
			Detail:        unwrapMessage(err),
			Menus:         accepted,
			RejectedMenus: rejected,
			DetectCount:   detectCount,
		})
		return
	}
	if queued == 0 {
		// 检测跑完但一项可用候选都没有：没什么可应用，也就谈不上「应用完自动退出」。
		// 窗口留着，让用户看见这一轮为什么是空的。
		say(event.AutomationStatus{
			Stage:         event.AutomationStageDone,
			Reason:        event.AutomationReasonNoCandidates,
			Menus:         accepted,
			RejectedMenus: rejected,
			DetectCount:   detectCount,
		})
		return
	}
	rounds = append(rounds, applyRounds...)
	say(event.AutomationStatus{
		Stage:         event.AutomationStageApplying,
		Menus:         accepted,
		RejectedMenus: rejected,
		DetectCount:   detectCount,
		ApplyCount:    queued,
	})

	if err := waitAll(ctx, d.wait, applyRounds); err != nil {
		waitReason, waitDetail := waitFailure(err)
		if waitReason == "" {
			return
		}
		say(event.AutomationStatus{
			Stage:         event.AutomationStageFailed,
			Reason:        waitReason,
			Detail:        waitDetail,
			Menus:         accepted,
			RejectedMenus: rejected,
			DetectCount:   detectCount,
			ApplyCount:    queued,
		})
		return
	}

	rep := d.report(rounds)
	base := event.AutomationStatus{
		Stage:         event.AutomationStageDone,
		Menus:         accepted,
		RejectedMenus: rejected,
		DetectCount:   detectCount,
		ApplyCount:    queued,
		AppliedCount:  rep.AppliedOK,
		OtherActive:   rep.OtherActive,
	}
	if !plan.Startup.AutoCloseAfterApply {
		say(base)
		return
	}
	// 退出这一门按三道顺序过：先确认队列里没有别人的活，再确认这一轮真应用成了东西。
	// 任一条件不满足都不关进程——「自动退出」是把窗口收走，用户再也看不到失败现场，
	// 只有确实跑成功了才值得替用户做这个决定。
	if rep.OtherActive > 0 {
		base.Reason = event.AutomationReasonCloseBusy
		say(base)
		return
	}
	if rep.AppliedOK == 0 {
		base.Reason = event.AutomationReasonCloseNoResult
		say(base)
		return
	}
	base.Stage = event.AutomationStageClosing
	say(base)
	d.quit()
}

// waitAll 逐个等这几轮收口。轮与轮之间是并行的（检测车道），所以先排完再依次等，
// 总时长等于最慢的那一轮，不是各轮相加。
func waitAll(ctx context.Context, wait func(context.Context, string) error, roundIDs []string) error {
	for _, id := range roundIDs {
		if err := wait(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// waitFailure 把等待失败翻成「界面要不要说一句」。
// 进程正在退出时的失败没人会看到，这里给空原因码，调用方据此静默收场。
func waitFailure(err error) (reason string, detail string) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", ""
	}
	return event.AutomationReasonWaitFailed, unwrapMessage(err)
}

// StartStartupAutomation 是这条链的唯一入口，由 main.go 在窗口起来后放进后台跑。
// quit 传「结束整个进程」那个动作（真退出，不是收进托盘，也不是只关窗口）；
// emit 是事件总线，给 nil 就只留最近一份结论、不往前端推。
func StartStartupAutomation(ctx context.Context, s *Service, quit func(), emit func(name string, payload any)) {
	runStartupAutomation(ctx, newAutomationDeps(s, quit, emit))
}

// newAutomationDeps 把链接到真实世界：开关和项目清单从配置现读，
// 检测和应用走那两个入口命令，等待走队列的 WaitRound，退出和发事件由装配层注入。
func newAutomationDeps(s *Service, quit func(), emit func(name string, payload any)) automationDeps {
	return automationDeps{
		plan:   s.automationPlan,
		detect: s.automationDetect,
		apply:  s.automationApply,
		wait:   s.tasks.WaitRound,
		report: s.automationReport,
		quit:   quit,
		status: func(status event.AutomationStatus) {
			// 先存后发：界面挂载时补读的最近一份，不能比事件慢一拍。
			s.automationStatus.Store(&status)
			if emit != nil {
				emit(event.AutomationStatusName, status)
			}
		},
	}
}

// automationPlan 读三个开关，并把勾了的主菜单展开成按菜单分组的项目清单。
func (s *Service) automationPlan() (automationPlan, error) {
	if s.productCatalogErr != nil {
		return automationPlan{}, s.productCatalogErr
	}
	if s.settings == nil {
		return automationPlan{}, s.settingsFailure()
	}
	cfg, err := s.settings.Snapshot()
	if err != nil {
		return automationPlan{}, err
	}
	plan := automationPlan{Startup: cfg.Startup}
	for _, menuID := range cfg.Startup.AutoDetectMenus {
		ids := s.itemsOfMenu(menuID)
		if len(ids) == 0 {
			continue
		}
		plan.Batches = append(plan.Batches, automationBatch{MenuID: menuID, ItemIDs: ids})
	}
	return plan, nil
}

// itemsOfMenu 把一个主菜单展开成这一轮要检测的项目 ID。
// 没有域名的条目（手动映射）跳过：它没有可探测的目标，混进去会让整批检测
// 在入口校验那一起被拒，白搭上下文同一个菜单里那些正常项目。
func (s *Service) itemsOfMenu(menuID string) []string {
	if s.productCatalog == nil {
		return nil
	}
	var ids []string
	for _, category := range s.productCatalog.Categories {
		if category.ID != menuID {
			continue
		}
		for _, item := range category.Items {
			if len(uniqueHostnames(item.Hostnames)) == 0 {
				continue
			}
			ids = append(ids, item.ID)
		}
	}
	return ids
}

// automationDetect 按菜单排一批检测，用这个范围的默认方法（配置里存的选择 → 出厂默认）。
// 返回值是这批任务落在的轮 ID；队列去重时可能带出别的轮，所以按任务逐条取，不假设只有一个。
func (s *Service) automationDetect(itemIDs []string) ([]string, error) {
	tasks, err := s.DetectItems(itemIDs, "", "")
	if err != nil {
		return nil, err
	}
	return roundIDsOf(tasks), nil
}

// automationApply 只把「最近一轮检测里拿到可用候选」的项目排进应用队列。
// ApplyItems 现在会逐项判定、做不了的以「跳过」进同一轮，所以这里先筛一遍不是为了躲报错，
// 而是为了不让自动化给自己造一堆「跳过的任务」刷屏——检测失败的那几个本来就在检测那一轮里看得见。
// 一个都没筛中时返回 0 条、不开轮——空轮不该在界面上占一个点不开的空根节点。
func (s *Service) automationApply(itemIDs []string) ([]string, int, error) {
	ready := make([]string, 0, len(itemIDs))
	for _, id := range itemIDs {
		view, ok := s.detectResults.view(id)
		if ok && hasAvailable(view) {
			ready = append(ready, id)
		}
	}
	if len(ready) == 0 {
		return nil, 0, nil
	}
	tasks, err := s.ApplyItems(ready)
	if err != nil {
		return nil, 0, err
	}
	return roundIDsOf(tasks), len(tasks), nil
}

// automationReport 从队列快照数出退出那一门要的三个数。
func (s *Service) automationReport(roundIDs []string) automationReport {
	mine := make(map[string]bool, len(roundIDs))
	for _, id := range roundIDs {
		if id != "" {
			mine[id] = true
		}
	}
	var rep automationReport
	for _, t := range s.tasks.Snapshot().Tasks {
		switch {
		case !mine[t.RoundID]:
			if task.IsActive(t.Status) {
				rep.OtherActive++
			}
		case task.IsActive(t.Status):
			rep.Active++
		case t.Kind == task.KindApply && t.Status == task.StatusDone:
			rep.AppliedOK++
		}
	}
	return rep
}

// roundIDsOf 取这批任务所在的轮 ID，去掉空值和重复。
func roundIDsOf(tasks []task.Task) []string {
	seen := make(map[string]bool, len(tasks))
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		if t.RoundID == "" || seen[t.RoundID] {
			continue
		}
		seen[t.RoundID] = true
		ids = append(ids, t.RoundID)
	}
	return ids
}

// GetAutomationStatus 返回这条链最近一次的结论；还没跑过时 stage 是空串。
// 界面挂载时先读这一份再订阅事件：自动化可能在窗口刚起来那一刻就跑完了，
// 只靠事件会漏掉前面几句，用户看到的是「勾了设置却什么都没发生」。
func (s *Service) GetAutomationStatus() event.AutomationStatus {
	if last := s.automationStatus.Load(); last != nil {
		return *last
	}
	return event.AutomationStatus{}
}
