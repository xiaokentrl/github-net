package config

import (
	"strconv"
	"testing"
)

// 侧栏可拖范围同时写在后端（本包的 SidebarWidthMin/Max）和前端（useSidebarResize 的两个常量，
// 界面上的上下限也从这两个常量取）。前端抄错一位的后果是：拖到 601 后端判配置非法、
// 界面回读旧值，用户看到的是「拖不动」，而实际是保存被拒。所以这里钉成相等。

const sidebarBoundsFile = "frontend/src/components/layout/useSidebarResize.ts"

func frontendConst(t *testing.T, src, name string) int {
	t.Helper()
	got := matchLines(t, src, `^export const `+name+` = (\d+)$`, "前端常量 "+name)
	if len(got) != 1 {
		t.Fatalf("前端常量 %s 应当只声明一次，实际 %d 次", name, len(got))
	}
	value, err := strconv.Atoi(got[0])
	if err != nil {
		t.Fatalf("前端常量 %s 不是整数：%q", name, got[0])
	}
	return value
}

func TestFrontendSidebarBoundsEqualBackendRange(t *testing.T) {
	src := readSource(t, repoRoot(t), sidebarBoundsFile)
	if got := frontendConst(t, src, "SIDEBAR_MIN_WIDTH"); got != SidebarWidthMin {
		t.Errorf("前端侧栏下限 %d 与后端 %d 不等", got, SidebarWidthMin)
	}
	if got := frontendConst(t, src, "SIDEBAR_MAX_WIDTH"); got != SidebarWidthMax {
		t.Errorf("前端侧栏上限 %d 与后端 %d 不等", got, SidebarWidthMax)
	}
}

// 布局页的三档预设必须落在后端范围内，并且和界面用的是同一对上下限；
// 后端哪天收窄范围，这里先红，而不是等用户在界面上点出一个「保存失败」。
func TestSidebarPresetsStayInsideRange(t *testing.T) {
	src := readSource(t, repoRoot(t), "frontend/src/features/settings/LayoutPane.tsx")
	min := frontendConst(t, readSource(t, repoRoot(t), sidebarBoundsFile), "SIDEBAR_MIN_WIDTH")
	max := frontendConst(t, readSource(t, repoRoot(t), sidebarBoundsFile), "SIDEBAR_MAX_WIDTH")
	presets := matchLines(t, src, `^\s*\{ value: (\d+), label: t\('settings\.layout\.(?:narrow|mid|wide)'\) \},?$`, "侧栏宽度预设")
	if len(presets) != 3 {
		t.Fatalf("侧栏宽度预设应当有 3 档，实际 %d 档：%v", len(presets), presets)
	}
	for _, p := range presets {
		value, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("预设 %q 不是整数", p)
		}
		if value < min || value > max {
			t.Errorf("预设 %d 不在前端范围 %d–%d 内", value, min, max)
		}
		if value < SidebarWidthMin || value > SidebarWidthMax {
			t.Errorf("预设 %d 不在后端范围 %d–%d 内，点了会被拒", value, SidebarWidthMin, SidebarWidthMax)
		}
	}
}
