package event

// NoticePushName 是后端主动推送的用户提示事件名字。全仓库只有这一处写它。
// 维护循环（巡检/自动补位）这类「不是用户点出来的动作」靠它把结果告知界面，
// 前端收到后弹右下角提示（pushNotice）。Kind 是提示级别：warning / success / info。
const NoticePushName = "notice:push"

// NoticePush 是提示事件的载荷。
type NoticePush struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}
