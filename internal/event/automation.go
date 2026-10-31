package event

// AutomationStatusName 是「启动后自动检测 → 自动应用 → 自动退出」这条链的状态事件名。
// 全仓库只有这一处写它，前端订阅用的必须是同一个字符串。
const AutomationStatusName = "automation:status"

// 阶段码。句子由界面按当前语言自己拼：后端写死一句中文，繁体界面就只能跟着看简体。
const (
	AutomationStageDetecting = "detecting" // 检测任务已排进队列
	AutomationStageApplying  = "applying"  // 检测跑完，应用任务已排进队列
	AutomationStageClosing   = "closing"   // 应用那一轮收口，正在退出进程
	AutomationStageDone      = "done"      // 这条链走完了
	AutomationStageSkipped   = "skipped"   // 条件不满足，什么都没排
	AutomationStageFailed    = "failed"    // 半路被拒或等不下去
)

// 原因码：一个阶段为什么停在这里，界面据此挑那一句提示。
const (
	AutomationReasonPlanFailed     = "plan_failed"     // 配置或目录没读出来
	AutomationReasonNoItems        = "no_items"        // 勾了菜单，但里面没有可检测的项目
	AutomationReasonDetectRejected = "detect_rejected" // 每个菜单都在入口被拒，一条检测都没排上
	AutomationReasonWaitFailed     = "wait_failed"     // 等不到这一轮收口（队列停了）
	AutomationReasonApplyRejected  = "apply_rejected"  // 应用任务没排进去
	AutomationReasonNoCandidates   = "no_candidates"   // 检测跑完，没有一项拿到可用候选
	AutomationReasonCloseBusy      = "close_busy"      // 队列里还挂着别人排的活，不退出
	AutomationReasonCloseNoResult  = "close_no_result" // 一项都没应用成功，不退出
)

// AutomationStatus 是这条链每次状态变化推给界面的一份结论。
// 只带机器码和计数，不带拼好的句子。数组字段宁可给空也不给 null。
type AutomationStatus struct {
	Stage  string `json:"stage"`
	Reason string `json:"reason,omitempty"`
	// Detail 是后端原话（入口拒绝、队列报错这些），只在失败时带：
	// 这些消息本来就带着用户要照著查的字段名，翻成界面语言反而丢了线索。
	Detail string `json:"detail,omitempty"`
	// Menus 是这次真的排进了检测的菜单 ID。
	Menus []string `json:"menus,omitempty"`
	// RejectedMenus 是菜单里被入口拒掉的那些（多半是那个范围还没选加速方法）。
	RejectedMenus []string `json:"rejectedMenus,omitempty"`
	// DetectCount、ApplyCount、AppliedCount 分别是排进检测的项目数、排进应用的项目数、
	// 应用成功的项目数。
	DetectCount  int `json:"detectCount"`
	ApplyCount   int `json:"applyCount"`
	AppliedCount int `json:"appliedCount"`
	// OtherActive 是队列里不属于这条链、还在跑的活，只有「该退出但退不了」时带。
	OtherActive int `json:"otherActive"`
}
