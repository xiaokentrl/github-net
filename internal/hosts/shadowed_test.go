package hosts

import "testing"

func TestShadowedOutsideDetectsBlockingLines(t *testing.T) {
	content := "140.82.121.4 github.com\n" +
		"\n" +
		"# 注释行不算\n" +
		"# BEGIN github-net managed hosts v1\n" +
		"203.0.113.10 github.com api.github.com\n" +
		"# END github-net managed hosts v1\n" +
		"199.29.237.204 gist.githubusercontent.com\n"
	block := Parse(content)
	got := shadowedOutside(content, block)

	if !got["github.com"] {
		t.Fatalf("区段前的同名行该被检出，实际 %v", got)
	}
	// 区段之后的行不挡区段（Hosts 首条生效）
	if got["gist.githubusercontent.com"] {
		t.Fatalf("区段之后的行不该算挡住，实际 %v", got)
	}
	// 注释行里的主机名不算
	if got["注释行"] {
		t.Fatalf("注释行不该参与判定")
	}
}

func TestShadowedOutsideNoBlockOrBroken(t *testing.T) {
	// 没有区段：不存在「挡住」这回事
	if got := shadowedOutside("1.2.3.4 github.com\n", Parse("1.2.3.4 github.com\n")); len(got) != 0 {
		t.Fatalf("无区段时该是空集合，实际 %v", got)
	}
	// 区段结构坏掉：不可判读就不给判定（上层按冲突处理）
	broken := Parse("# === 开始 ===\n乱七八糟\n")
	if got := shadowedOutside("# === 开始 ===\n乱七八糟\n", broken); len(got) != 0 {
		t.Fatalf("坏区段该是空集合，实际 %v", got)
	}
}

func TestShadowedNormalizesHostname(t *testing.T) {
	content := "140.82.121.4 API.GitHub.COM.\n" +
		"# BEGIN github-net managed hosts v1\n" +
		"203.0.113.10 api.github.com\n" +
		"# END github-net managed hosts v1\n"
	got := shadowedOutside(content, Parse(content))
	if !got["api.github.com"] {
		t.Fatalf("大小写和尾点应规范化后命中，实际 %v", got)
	}
}
