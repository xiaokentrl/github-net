package main

import (
	"errors"
	"github.com/xiaokentrl/github-net/internal/config"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/task"
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

// 托盘提示是原型那颗「运行中」小圆点的等价物：空闲只能写应用名，有活才多说一句。
// 数任务时只认正在执行的那几档，排队中和已结束的都不算，否则关着窗口也会一直显示在跑。
func TestTrayTooltipFollowsRunningTasks(t *testing.T) {
	if got := trayTooltipText("GitHub Net", 0); got != "GitHub Net" {
		t.Errorf("空闲时只该写应用名，实际 %q", got)
	}
	if got := trayTooltipText("GitHub Net", 2); !strings.Contains(got, "进行中 2 项") {
		t.Errorf("有任务在跑时要写清几项，实际 %q", got)
	}
	snap := task.Snapshot{Tasks: []task.Task{
		{ID: "a", Status: task.StatusRunning},
		{ID: "b", Status: task.StatusRestoring},
		{ID: "c", Status: task.StatusWaiting},
		{ID: "d", Status: task.StatusDone},
		{ID: "e", Status: task.StatusFailed},
	}}
	if got := runningTasks(snap); got != 2 {
		t.Errorf("只该数执行中的任务，实际 %d", got)
	}
}

// 托盘主题子菜单要盖满 config 支持的全部主题：allowedThemes 加了新主题而托盘忘了，
// 用户就切不到那个主题。齐性对照《主题与色彩方案》的验收清单（对照复核 2026-11-13 中危）。
func TestTrayThemesCoverAllAllowedThemes(t *testing.T) {
	want := config.AllowedThemeIDs()
	got := make([]string, 0, len(trayThemes))
	for _, th := range trayThemes {
		got = append(got, th.id)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("托盘主题清单 %v 与 config 支持的主题 %v 不一致", got, want)
	}
}
