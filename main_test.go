package main

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

// 启动失败是从桌面图标点进来最容易撞上的一种「什么都没发生」：stderr 没人看。
// 这里钉两件事：底层的原话必须一句不丢地带出来；只有确认总线变量为空时才补那句原因，
// 变量在但总线已经死了的情况判不出来，就不许乱猜。
func TestStartupFailureTextNamesTheBusOnlyWhenItIsMissing(t *testing.T) {
	base := startupFailureText(errors.New("dbus: connection refused"))
	if !strings.Contains(base, "界面没能启动") {
		t.Errorf("没有说明这是启动失败：%q", base)
	}
	if !strings.Contains(base, "dbus: connection refused") {
		t.Errorf("底层原话被吞了：%q", base)
	}
	if runtime.GOOS != "linux" {
		t.Skip("会话总线只在 Linux 上判")
	}

	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "")
	if !strings.Contains(startupFailureText(errors.New("boom")), "会话总线") {
		t.Error("变量为空时应给出「没有会话总线」这句可照着查的原因")
	}

	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	if strings.Contains(startupFailureText(errors.New("boom")), "会话总线") {
		t.Error("变量在的时候不该断言总线缺失，那是另一种故障")
	}
}
