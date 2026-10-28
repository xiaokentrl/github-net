package app

import (
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func setting(key, value string) debug.BuildSetting {
	return debug.BuildSetting{Key: key, Value: value}
}

func TestBuildStampFromReadsVCSMarkers(t *testing.T) {
	stamp := buildStampFrom([]debug.BuildSetting{
		setting("vcs.revision", "0b64053507c6c0f0c71a56ee11f6ec6e1758a358"),
		setting("vcs.time", "2026-10-28T17:30:35Z"),
		setting("vcs.modified", "true"),
	})
	if stamp.Time != "2026-10-28T17:30:35Z" {
		t.Errorf("构建时间 = %q，期望原样 UTC RFC3339", stamp.Time)
	}
	if stamp.Revision != "0b640535" {
		t.Errorf("构建哈希 = %q，期望前 8 位 0b640535", stamp.Revision)
	}
	if !stamp.Modified {
		t.Error("工作区有未提交改动却没报 modified，界面上会把脏构建说成干净")
	}
}

func TestBuildStampFromTreatsLocalTimeAsStillReal(t *testing.T) {
	// 有些构建机带时区偏移，必须换算成 UTC 而不是原样贴出去，否则两台机器出的包对不上。
	stamp := buildStampFrom([]debug.BuildSetting{
		setting("vcs.time", "2026-10-29T01:30:35+08:00"),
	})
	if stamp.Time != "2026-10-28T17:30:35Z" {
		t.Errorf("带时区的提交时间没换算成 UTC: %q", stamp.Time)
	}
}

func TestBuildStampFromWithoutMarkersIsAllEmpty(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings []debug.BuildSetting
	}{
		{"完全没有标记", nil},
		{"只有哈希没有时间", []debug.BuildSetting{setting("vcs.revision", "0b64053507c6c0f0")}},
		{"时间格式不对", []debug.BuildSetting{setting("vcs.time", "2026-10-28")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stamp := buildStampFrom(tc.settings)
			if stamp != (buildStamp{}) {
				t.Errorf("标记不全却给出了 %+v，界面会显示一个没有来历的哈希", stamp)
			}
		})
	}
}

func TestBuildStampFromOnlyAcceptsTrueForModified(t *testing.T) {
	// vcs.modified 的值是 "true"/"false" 字符串；别的写法不能算脏。
	stamp := buildStampFrom([]debug.BuildSetting{
		setting("vcs.time", "2026-10-28T17:30:35Z"),
		setting("vcs.modified", "TRUE"),
	})
	if stamp.Modified {
		t.Error(`vcs.modified 写成 "TRUE" 就认成脏构建了，只接受 "true"`)
	}
}

func TestShortRevisionKeepsShortAndEmptyValues(t *testing.T) {
	if got := shortRevision(""); got != "" {
		t.Errorf("空哈希被补成了 %q", got)
	}
	if got := shortRevision("abc"); got != "abc" {
		t.Errorf("不足 8 位的哈希被改成了 %q", got)
	}
	if got := shortRevision(strings.Repeat("0", 40)); len(got) != 8 {
		t.Errorf("40 位哈希截出来是 %d 位，期望 8", len(got))
	}
}

func TestGetAppStatusCarriesStampOrNothing(t *testing.T) {
	withStamp := &Service{appName: "GitHub Net", version: "0.1.0", stamp: buildStamp{
		Time:     "2026-10-28T17:30:35Z",
		Revision: "0b640535",
		Modified: true,
	}}
	status, err := withStamp.GetAppStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.BuildTime != "2026-10-28T17:30:35Z" || status.BuildRevision != "0b640535" || !status.BuildModified {
		t.Errorf("构建标记没原样透传给前端: %+v", status)
	}

	// 没有标记的构建（例如 -buildvcs=false）三个值必须一起是零，
	// 前端才知道该显示「未提供」而不是一个空白格。
	clean := &Service{appName: "GitHub Net", version: "0.1.0"}
	status, err = clean.GetAppStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.BuildTime != "" || status.BuildRevision != "" || status.BuildModified {
		t.Errorf("没有构建标记却给出 %+v", status)
	}
}

func TestCurrentBuildStampTimeIsParsableWhenPresent(t *testing.T) {
	// 这一条只对当前真二进制做形状检查：有值就必须是能解析的 UTC RFC3339，
	// 不假设 go test 一定把 VCS 标记打进来，所以空值也算过。
	stamp := currentBuildStamp()
	if stamp.Time == "" {
		t.Skip("这个测试二进制没带 VCS 标记，形状检查跳过")
	}
	if _, err := time.Parse(time.RFC3339, stamp.Time); err != nil {
		t.Errorf("构建时间 %q 不是 RFC3339: %v", stamp.Time, err)
	}
	if !strings.HasSuffix(stamp.Time, "Z") {
		t.Errorf("构建时间 %q 没换算成 UTC", stamp.Time)
	}
	if len(stamp.Revision) != 8 {
		t.Errorf("构建哈希 %q 不是 8 位", stamp.Revision)
	}
}
