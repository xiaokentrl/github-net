package app

import (
	"context"
	"errors"

	"github.com/xiaokentrl/github-net/internal/updater"
)

// 更新检查的稳定错误码：前端按码分支给不同的话，不解析中文。
const (
	CodeUpdateNoRelease = "update_no_release"
	CodeUpdateLimited   = "update_rate_limited"
	CodeUpdateNetwork   = "update_network"
	CodeUpdateBadData   = "update_bad_data"
)

// CheckForUpdates 查 GitHub 最新稳定版并与当前版本比较。
// 只比较和显示；下载、校验、安装属安全更新流程，这里一步都不做。
func (s *Service) CheckForUpdates() (updater.Info, error) {
	info, err := s.updater.Check(context.Background(), s.version)
	if err != nil {
		return updater.Info{}, updatePublicError(err)
	}
	return info, nil
}

// updatePublicError 把 updater 的四类失败翻成带稳定码的公开错误，原因留在 cause 里供日志。
func updatePublicError(err error) *Error {
	switch {
	case errors.Is(err, updater.ErrNoStableRelease):
		return NewError(CodeUpdateNoRelease, "这个仓库还没有已发布的稳定版", err)
	case errors.Is(err, updater.ErrRateLimited):
		return NewError(CodeUpdateLimited, "GitHub 限流了这次检查，稍后再试", err)
	case errors.Is(err, updater.ErrNetwork):
		return NewError(CodeUpdateNetwork, "网络检查失败，确认能访问 GitHub 后重试", err)
	case errors.Is(err, updater.ErrBadData):
		return NewError(CodeUpdateBadData, "GitHub 返回的数据不认识，可能需要升级本应用", err)
	default:
		return NewError(CodeInternal, "更新检查失败，请稍后重试", err)
	}
}
