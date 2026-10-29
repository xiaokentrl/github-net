package event

import (
	"github.com/xiaokentrl/github-net/internal/updater"
)

// UpdateStatusName 是更新检查结果事件的名字。全仓库只有这一处写它。
// 启动后的自动检查走这条事件把结论推给界面；设置页里的手动检查走命令返回值，不走事件。
const UpdateStatusName = "update:status"

// UpdateNotice 是更新检查事件的总载荷：成功带 Info，失败带一句可展示的 Error。
// 两者都空的情况不存在——检查要么有结论要么有失败原因。
type UpdateNotice struct {
	Info  *updater.Info `json:"info,omitempty"`
	Error string        `json:"error,omitempty"`
}
