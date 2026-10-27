package app

import (
	"runtime"
	"strings"
	"time"
)

// Service 是暴露给前端的唯一门面；这里只放前端确实需要的读操作。
type Service struct {
	appName   string
	version   string
	startedAt time.Time
	sequence  func() int64
}

func NewService(appName, version string, sequence func() int64) *Service {
	if sequence == nil {
		sequence = func() int64 { return 0 }
	}
	return &Service{
		appName:   appName,
		version:   version,
		startedAt: time.Now(),
		sequence:  sequence,
	}
}

// RuntimeInfo 返回当前进程的真实运行环境，供前端显示与绑定链路自检。
func (s *Service) RuntimeInfo() (RuntimeInfo, error) {
	return RuntimeInfo{
		AppName:        s.appName,
		Version:        s.version,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		StartedAt:      s.startedAt.UTC().Format(time.RFC3339),
		StatusSequence: s.sequence(),
	}, nil
}

// Ping 只为验证「后端错误能传回前端」这条路：空令牌一律拒绝，不做任何检测。
func (s *Service) Ping(token string) (string, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return "", &Error{Code: "empty_token", Message: "令牌不能为空"}
	}
	return trimmed, nil
}
