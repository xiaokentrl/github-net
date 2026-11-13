package main

import (
	"errors"
	"github.com/xiaokentrl/github-net/internal/config"
	"net/http"
	"net/http/httptest"
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

// 首帧主题注入：模板写死的 data-theme/lang 被配置值原地替换，正文按新长度发、
// 状态码透传。替换是稳的选择：不依赖「HTML 同名属性先到先得」这种解析器行为。
func TestInjectThemeAndLangReplacesTemplateAttrs(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<!DOCTYPE html>\n<html lang=\"zh-CN\" data-theme=\"light\">\n  <head>"))
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	out, code := injectThemeAndLang("midnight", "zh-TW", next, w, r)
	if code != http.StatusOK {
		t.Fatalf("next 隐式 200 时注入后该发 200，实际 %d", code)
	}
	if !strings.Contains(string(out), `data-theme="midnight"`) {
		t.Errorf("模板的 data-theme 该被替换成配置值，实际 %s", out)
	}
	if !strings.Contains(string(out), `lang="zh-TW"`) {
		t.Errorf("模板的 lang 该被替换成配置值，实际 %s", out)
	}
	if strings.Contains(string(out), `data-theme="light"`) {
		t.Error("不允许残留模板旧值：同名属性先到先得，残留的旧值会赢")
	}
	if w.Header().Get("Content-Type") != "text/html" {
		t.Errorf("响应头要原样透传，实际 %v", w.Header())
	}

	// 模板没有这两个属性时（比如构建模板改了）要追加而不是丢掉。
	next2 := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><head></head></html>"))
	})
	out2, code2 := injectThemeAndLang("forest", "zh-CN", next2, w, httptest.NewRequest(http.MethodGet, "/", nil))
	if code2 != http.StatusOK {
		t.Fatalf("隐式 200 该归位，实际 %d", code2)
	}
	if !strings.Contains(string(out2), `data-theme="forest"`) || !strings.Contains(string(out2), `lang="zh-CN"`) {
		t.Errorf("无属性模板该追加两个属性，实际 %s", out2)
	}
}
