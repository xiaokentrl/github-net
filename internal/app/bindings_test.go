package app

import (
	"errors"
	"testing"
)

func TestRuntimeInfoReturnsRealEnvironment(t *testing.T) {
	svc := NewService("GitHub Net", "0.1.0", func() int64 { return 7 })

	info, err := svc.RuntimeInfo()
	if err != nil {
		t.Fatalf("RuntimeInfo 返回错误: %v", err)
	}
	if info.AppName != "GitHub Net" || info.Version != "0.1.0" {
		t.Errorf("应用名或版本不对: %+v", info)
	}
	if info.OS == "" || info.Arch == "" || info.StartedAt == "" {
		t.Errorf("系统信息不完整: %+v", info)
	}
	if info.StatusSequence != 7 {
		t.Errorf("应转交事件序号 7，实际 %d", info.StatusSequence)
	}
}

func TestRuntimeInfoWithoutSequenceProvider(t *testing.T) {
	svc := NewService("GitHub Net", "0.1.0", nil)

	info, err := svc.RuntimeInfo()
	if err != nil || info.StatusSequence != 0 {
		t.Errorf("未提供序号来源时应为 0，得到 %+v err %v", info, err)
	}
}

func TestPingRejectsEmptyToken(t *testing.T) {
	svc := NewService("GitHub Net", "0.1.0", nil)

	if _, err := svc.Ping("   "); err == nil {
		t.Fatal("空令牌应当返回错误")
	} else {
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != "empty_token" {
			t.Errorf("错误码不是 empty_token，实际: %v", err)
		}
	}

	echo, err := svc.Ping("abc")
	if err != nil || echo != "abc" {
		t.Errorf("正常令牌应原样返回，得到 %q err %v", echo, err)
	}
}
