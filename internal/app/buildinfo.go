package app

import (
	"runtime/debug"
	"time"
)

// buildStamp 是「关于我们」那一屏要显示的构建事实。
//
// 三个值全部来自二进制自带的 VCS 标记，不是运行时间，也不是编出来的：
// go 构建时会把源码提交的时间与哈希打进可执行文件，读不到就整套留空，
// 界面上显示「未提供」而不是拿今天充数。
type buildStamp struct {
	// Time 是构建所用源码的提交时间，UTC RFC3339；没有标记时为空。
	Time string
	// Revision 是提交哈希的前 8 位；没有标记时为空。
	Revision string
	// Modified 为真表示这份构建带着工作区里未提交的改动。
	// 它必须显示出来：否则「源码是 0b64053」和「源码是 0b64053 加一堆没提交的改动」看着一样。
	Modified bool
}

// currentBuildStamp 读当前进程自带的构建信息。
// 测试二进制也带这套标记，所以这里读到的东西依赖构建方式，不能拿来当断言对象；
// 断言只对着 buildStampFrom 的确定输入做。
func currentBuildStamp() buildStamp {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return buildStamp{}
	}
	return buildStampFrom(info.Settings)
}

// buildStampFrom 把 -buildvcs 打进来的那串 setting 收成三个值。
// 时间读不出来就整套作废：留下一个没有时间来历的哈希，界面上没法解释它是什么时间的构建。
func buildStampFrom(settings []debug.BuildSetting) buildStamp {
	var committed time.Time
	revision := ""
	modified := false
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.time":
			if parsed, err := time.Parse(time.RFC3339, setting.Value); err == nil {
				committed = parsed
			}
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if committed.IsZero() {
		return buildStamp{}
	}
	return buildStamp{
		Time:     committed.UTC().Format(time.RFC3339),
		Revision: shortRevision(revision),
		Modified: modified,
	}
}

// shortRevision 取哈希前 8 位。git 的短哈希本来就该能唯一，界面上要的是「一眼认得出是哪一笔」，
// 40 位全贴出来没人读；空哈希仍然是空，不补零。
func shortRevision(revision string) string {
	if len(revision) <= 8 {
		return revision
	}
	return revision[:8]
}
