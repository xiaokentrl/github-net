package app

import (
	"reflect"
	"sort"
	"testing"
)

// exposureList 是 Service 导出方法的权威名单，与 docs/绑定暴露面清单.md 逐条对应。
// 这道门是《绑定层契约与生成物方案》的验收项「暴露面清单与导出方法数一致」的落地：
// 新增/删除导出方法而不更新清单与这道门，测试直接红——别让「能被前端调到的方法」
// 悄悄变多（暴露面只许有意扩大）。
var exposureList = []string{
	"ApplyItems",
	"CheckForUpdates",
	"ClearAllIPCache",
	"ClearFinishedTasks",
	"ClearSourcesCache",
	"DetectBudget",
	"DetectItems",
	"DownloadUpdate",
	"GetAppStatus",
	"GetAutomationStatus",
	"GetCandidatePool",
	"GetDetectResult",
	"GetItems",
	"GetLastDetectProfile",
	"GetSettings",
	"GetStrategies",
	"GetTasks",
	"OpenHosts",
	"Ping",
	"PreviewIPCache",
	"PreviewRestoreHosts",
	"PreviewRestoreHostsItem",
	"PreviewRestoreHostsItems",
	"PreviewSourcesCache",
	"ReprobeIP",
	"RestoreAllHosts",
	"RestoreItems",
	"RuntimeInfo",
	"SaveSettings",
	"SelectStrategy",
	"SetLastDetectProfile",
	"SettingsStore",
}

func TestServiceExposureMatchesDoc(t *testing.T) {
	typ := reflect.TypeOf(NewService("x", "0", nil, nil))
	actual := make([]string, 0, typ.NumMethod())
	for i := 0; i < typ.NumMethod(); i++ {
		actual = append(actual, typ.Method(i).Name)
	}
	sort.Strings(actual)
	expected := append([]string(nil), exposureList...)
	sort.Strings(expected)
	if len(actual) != len(expected) {
		t.Fatalf("导出方法数 %d != 清单 %d；多出的：%v。更新 docs/绑定暴露面清单.md 和本名单（类别/校验策略/前端登记三列都要核）",
			len(actual), len(expected), diff(expected, actual))
	}
	for i := range actual {
		if actual[i] != expected[i] {
			t.Fatalf("导出方法名单对不上（位置 %d）：实际 %s，清单 %s；diff：%v / %v",
				i, actual[i], expected[i], actual, expected)
		}
	}
}

func diff(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	var out []string
	for _, s := range b {
		if !set[s] {
			out = append(out, s)
		}
	}
	return out
}
