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
	// Code 是失败时的稳定错误码：前端按码出界面语言文案（自动检查失败不走命令
	// 返回值，没有这一条繁体用户就只能看简体原文）。
	Code string `json:"code,omitempty"`
}

// UpdateProgressName 是更新下载进度事件的名字。和检查结论（update:status）分开：
// 一个说「这一轮下载收到多少了」，一个说「检查的结论是什么」，消费方和节奏都不同。
// 下载可能持续几分钟，进度按 200ms 节拍来，不逐块刷。
const UpdateProgressName = "update:download-progress"

// UpdateProgress 是下载进度事件的载荷。Total 为 0 表示服务器没给总长（理论上不会，
// GitHub 发布资产都带大小；真发生时界面按「已收 N 字节」显示，不算百分比）。
type UpdateProgress struct {
	Name     string `json:"name"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
}
