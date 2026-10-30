package config

import (
	"strconv"
	"testing"
)

// 侧栏可拖范围同时写在后端（本包的 SidebarWidthMin/Max）和前端（useSidebarResize 的两个常量，
// 界面上的上下限也从这两个常量取）。前端抄错一位的后果是：拖到 601 后端判配置非法、
// 界面回读旧值，用户看到的是「拖不动」，而实际是保存被拒。所以这里钉成相等。

const sidebarBoundsFile = "frontend/src/components/layout/useSidebarResize.ts"
const queueListBoundsFile = "frontend/src/features/tasks/queueListWidth.ts"
const queueHeightBoundsFile = "frontend/src/features/tasks/queueHeight.ts"

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

// 队列里「任务列表」那一栏的宽度规则在前端（frontend/src/features/tasks/queueListWidth.ts）。
// 界面上的天花板还会再按队列区宽度收一道（45%），后端不知道窗口多大，所以后端的 1200 只是存储兜底。
// 这三处死数不等的话：下限抄大了会拖不出窄栏，上限抄大了会被后端判非法并回读旧值（看着像「拖不动」），
// 默认值抄错了则在配置读回来之前跳一下。
func TestFrontendQueueListBoundsEqualBackendRange(t *testing.T) {
	src := readSource(t, repoRoot(t), queueListBoundsFile)
	if got := frontendConst(t, src, "QUEUE_LIST_MIN_WIDTH"); got != QueueListWidthMin {
		t.Errorf("前端任务列表下限 %d 与后端 %d 不等", got, QueueListWidthMin)
	}
	if got := frontendConst(t, src, "QUEUE_LIST_STATIC_MAX_WIDTH"); got != QueueListWidthMax {
		t.Errorf("前端任务列表上限 %d 与后端 %d 不等", got, QueueListWidthMax)
	}
	if got := frontendConst(t, src, "QUEUE_LIST_DEFAULT_WIDTH"); got != Default().Layout.QueueListWidth {
		t.Errorf("前端默认任务列表宽度 %d 与后端默认 %d 不等", got, Default().Layout.QueueListWidth)
	}
}

// 布局页那三档任务列表预设必须落在后端范围内：后端收窄范围时这里先红，
// 而不是等用户点出一个「保存失败」。键名用 list* 而不是 narrow/mid/wide，避免和侧栏那三档混扫。
func TestQueueListPresetsStayInsideRange(t *testing.T) {
	src := readSource(t, repoRoot(t), "frontend/src/features/settings/LayoutPane.tsx")
	presets := matchLines(t, src, `^\s*\{ value: (\d+), label: t\('settings\.layout\.list(?:Narrow|Mid|Wide)'\) \},?$`, "任务列表宽度预设")
	if len(presets) != 3 {
		t.Fatalf("任务列表宽度预设应当有 3 档，实际 %d 档：%v", len(presets), presets)
	}
	for _, p := range presets {
		value, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("预设 %q 不是整数", p)
		}
		if value < QueueListWidthMin || value > QueueListWidthMax {
			t.Errorf("预设 %d 不在后端范围 %d–%d 内，点了会被拒", value, QueueListWidthMin, QueueListWidthMax)
		}
	}
}

// 布局页的三档侧栏预设必须落在后端范围内，并且和界面用的是同一对上下限；
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

// 队列面板的高度规则在前端（frontend/src/features/tasks/queueHeight.ts）。
// 界面上还会再按窗口高度收一道天花板（最多占窗口 70%、上面那一整栏至少留 200px），
// 后端不知道窗口多大，所以这里那对 120/2000 是存储兜底、不是体验上限。
// 三个死数抄错的后果：下限抄大了会拖不出矮面板，上限抄大了会被后端判非法并回读旧值（看着像「拖不动」），
// 默认值抄错了则在配置读回来之前跳一下。
func TestFrontendQueueHeightBoundsEqualBackendRange(t *testing.T) {
	src := readSource(t, repoRoot(t), queueHeightBoundsFile)
	if got := frontendConst(t, src, "QUEUE_HEIGHT_MIN"); got != QueueHeightMin {
		t.Errorf("前端队列高度下限 %d 与后端 %d 不等", got, QueueHeightMin)
	}
	if got := frontendConst(t, src, "QUEUE_HEIGHT_STATIC_MAX"); got != QueueHeightMax {
		t.Errorf("前端队列高度静态上限 %d 与后端 %d 不等", got, QueueHeightMax)
	}
	if got := frontendConst(t, src, "QUEUE_HEIGHT_DEFAULT"); got != Default().Layout.QueueHeight {
		t.Errorf("前端默认队列高度 %d 与后端默认 %d 不等", got, Default().Layout.QueueHeight)
	}
}

// 布局页那三档队列高度预设必须落在后端范围内：后端收窄范围时这里先红，
// 而不是等用户点出一个「保存失败」。键名用 small/middle/large，和侧栏的 narrow/mid/wide、
// 任务列表的 list* 三批各扫各的，混用会让门数错档数。
func TestQueueHeightPresetsStayInsideRange(t *testing.T) {
	src := readSource(t, repoRoot(t), "frontend/src/features/settings/LayoutPane.tsx")
	presets := matchLines(t, src, `^\s*\{ value: (\d+), label: t\('settings\.layout\.(?:small|middle|large)'\) \},?$`, "队列高度预设")
	if len(presets) != 3 {
		t.Fatalf("队列高度预设应当有 3 档，实际 %d 档：%v", len(presets), presets)
	}
	for _, p := range presets {
		value, err := strconv.Atoi(p)
		if err != nil {
			t.Fatalf("预设 %q 不是整数", p)
		}
		if value < QueueHeightMin || value > QueueHeightMax {
			t.Errorf("预设 %d 不在后端范围 %d–%d 内，点了会被拒", value, QueueHeightMin, QueueHeightMax)
		}
	}
}
