package hosts

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// 这些用例只用内存字符串和临时目录里的假 Hosts 文件，绝不碰真实系统 Hosts。

func blockWith(lines ...string) string {
	parts := append([]string{MarkerBegin()}, lines...)
	parts = append(parts, MarkerEnd())
	return strings.Join(parts, "\n")
}

func TestParseNoMarkerIsAbsentAndUsable(t *testing.T) {
	content := "127.0.0.1\tlocalhost\n127.0.1.1 kentrl\n::1 ip6-localhost\n"
	got := Parse(content)
	if got.Present {
		t.Errorf("没有标记却报 Present=true：%+v", got)
	}
	if !got.Usable() {
		t.Errorf("没有区段应当算可用（就是零条记录），实际 %q：%s", got.Anomaly, got.Reason)
	}
	if len(got.Records) != 0 {
		t.Errorf("没有区段却读出 %d 条记录", len(got.Records))
	}
	// 区段外的用户手写行不算本工具记录，哪怕主机名一模一样。
	if got.HasRecord("localhost") {
		t.Error("区段外的用户行被当成了本工具记录")
	}
}

func TestParseReadsRecordsAndNormalizes(t *testing.T) {
	// 第 1 行是区段前的用户内容，第 5 行是区段后的用户内容。
	content := strings.Join([]string{
		"127.0.0.1 localhost",
		MarkerBegin(),
		"140.82.113.4 GitHub.COM github.com.",
		"",
		MarkerEnd(),
		"127.0.0.1 demo.test",
	}, "\n")
	got := Parse(content)
	if !got.Present || !got.Usable() {
		t.Fatalf("区段应当可用，实际 Present=%v anomaly=%q reason=%s", got.Present, got.Anomaly, got.Reason)
	}
	if got.Version != ManagedVersion {
		t.Errorf("区段版本 = %d，期望 %d", got.Version, ManagedVersion)
	}
	if len(got.Records) != 1 {
		t.Fatalf("记录条数 = %d，期望 1：%+v", len(got.Records), got.Records)
	}
	rec := got.Records[0]
	if rec.IP != "140.82.113.4" {
		t.Errorf("IP = %q", rec.IP)
	}
	if strings.Join(rec.Hostnames, ",") != "github.com,github.com" {
		t.Errorf("主机名规范化结果 = %v，期望都收成 github.com", rec.Hostnames)
	}
	if rec.Line != 3 {
		t.Errorf("行号 = %d，期望 3（行号只用来给用户指问题出在哪一行）", rec.Line)
	}
	if !got.HasRecord("github.com") || !got.HasRecord("GitHub.com.") {
		t.Error("HasRecord 对大小写和尾随点不生效")
	}
	if got.HasRecord("demo.test") {
		t.Error("区段外的主机名被算成区段内记录")
	}
}

func TestParseStructureAnomalies(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    Anomaly
	}{
		{"只有开始标记", MarkerBegin() + "\n1.2.3.4 a.com\n", AnomalyMissingEnd},
		{"只有结束标记", "1.2.3.4 a.com\n" + MarkerEnd() + "\n", AnomalyMissingBegin},
		{"结束在开始前面", MarkerEnd() + "\n1.2.3.4 a.com\n" + MarkerBegin() + "\n", AnomalyDuplicated},
		{"两个开始标记", MarkerBegin() + "\n1.2.3.4 a.com\n" + MarkerBegin() + "\n1.2.3.5 b.com\n" + MarkerEnd() + "\n", AnomalyDuplicated},
		{"两个结束标记", MarkerBegin() + "\n1.2.3.4 a.com\n" + MarkerEnd() + "\n" + MarkerEnd() + "\n", AnomalyDuplicated},
		{"区段套区段", MarkerBegin() + "\n" + MarkerBegin() + "\n1.2.3.4 a.com\n" + MarkerEnd() + "\n", AnomalyDuplicated},
		{"版本不认识", "# BEGIN github-net managed hosts v2\n1.2.3.4 a.com\n# END github-net managed hosts v2\n", AnomalyUnknownVersion},
		{"首尾版本不一致", "# BEGIN github-net managed hosts v1\n1.2.3.4 a.com\n# END github-net managed hosts v2\n", AnomalyUnknownVersion},
		{"区段里整行注释", blockWith("# 手工加的"), AnomalyBadRecord},
		{"区段里行尾注释", blockWith("1.2.3.4 a.com # 手工加的"), AnomalyBadRecord},
		{"区段里只有 IP", blockWith("1.2.3.4"), AnomalyBadRecord},
		{"区段里 IP 不合格", blockWith("not-an-ip a.com"), AnomalyBadRecord},
		{"区段里主机名带端口", blockWith("1.2.3.4 a.com:443"), AnomalyBadRecord},
		{"区段里主机名本身是 IP", blockWith("1.2.3.4 5.6.7.8"), AnomalyBadRecord},
		{"标记行前后有空格也算标记", "  " + MarkerBegin() + "\n1.2.3.4 a.com\n" + MarkerEnd() + "\n", AnomalyMissingBegin},
		{"标记行后面有字就不算标记", MarkerBegin() + " 备注\n1.2.3.4 a.com\n" + MarkerEnd() + "\n", AnomalyMissingBegin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.content)
			if got.Anomaly != tc.want {
				t.Errorf("异常档 = %q，期望 %q（reason=%s）", got.Anomaly, tc.want, got.Reason)
			}
			if got.Anomaly != AnomalyNone && got.Reason == "" {
				t.Error("异常档必须带一句能直接显示的原因")
			}
			if got.Anomaly != AnomalyNone && len(got.Records) != 0 {
				t.Errorf("区段判读不了却不该给记录：%+v", got.Records)
			}
		})
	}
}

func TestParseHasRecordMatchesAnyHostnameInBlock(t *testing.T) {
	got := Parse(blockWith("1.2.3.4 a.com b.com", "1.2.3.5 c.com"))
	for _, host := range []string{"a.com", "b.com", "c.com"} {
		if !got.HasRecord(host) {
			t.Errorf("%s 在区段里却没匹配上", host)
		}
	}
	if got.HasRecord("d.com") {
		t.Error("区段里没有的 d.com 被匹配上了")
	}
	if got.HasRecord("") || got.HasRecord("   ") {
		t.Error("空主机名不该匹配任何东西")
	}
}

func TestParseAcceptsCRLFAndTrailingBlankLines(t *testing.T) {
	content := strings.ReplaceAll(blockWith("1.2.3.4 a.com"), "\n", "\r\n") + "\r\n\r\n"
	got := Parse(content)
	if !got.Usable() || len(got.Records) != 1 {
		t.Errorf("CRLF 文件应当能读，实际 anomaly=%q 记录数=%d", got.Anomaly, len(got.Records))
	}
}

// 下面这几条走 Read：文件读写全在 t.TempDir() 里，不会碰到 /etc/hosts。

func TestReadOnTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	content := "127.0.0.1 localhost\n" + blockWith("140.82.113.4 github.com") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写临时 Hosts 失败：%v", err)
	}
	snap := Read(path)
	if !snap.OK() {
		t.Fatalf("读临时文件应当成功，实际 %q", snap.ReadErr)
	}
	if snap.Path != path {
		t.Errorf("路径回显 = %q，期望 %q", snap.Path, path)
	}
	if snap.ReadAt.IsZero() {
		t.Error("读取时间没填，前端就没法说明这次是什么时候读的")
	}
	if !snap.Block.HasRecord("github.com") {
		t.Error("刚写进去的映射没被读出来")
	}
	if snap.Block.HasRecord("localhost") {
		t.Error("区段外的 localhost 不该算本工具记录")
	}
}

func TestReadFailuresAreStatesNotPanics(t *testing.T) {
	snap := Read("")
	if snap.OK() || snap.ReadErr == "" {
		t.Errorf("空路径要报读不到，实际 OK=%v ReadErr=%q", snap.OK(), snap.ReadErr)
	}

	missing := Read(filepath.Join(t.TempDir(), "没有这个文件"))
	if missing.OK() || !strings.Contains(missing.ReadErr, "不存在") {
		t.Errorf("文件不存在的说明里要有「不存在」，实际 %q", missing.ReadErr)
	}

	dir := t.TempDir()
	asDir := Read(dir)
	if asDir.OK() || asDir.ReadErr == "" {
		t.Errorf("路径指向目录也要报读不到，实际 OK=%v", asDir.OK())
	}

	if os.Geteuid() == 0 {
		t.Skip("以 root 跑测试时读权限门永远打开，这条只在普通用户下有意义")
	}
	path := filepath.Join(dir, "no-read")
	if err := os.WriteFile(path, []byte("1.2.3.4 a.com\n"), 0o000); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	noPerm := Read(path)
	if noPerm.OK() || !strings.Contains(noPerm.ReadErr, "没有读权限") {
		t.Errorf("没权限的说明里要有「没有读权限」，实际 %q", noPerm.ReadErr)
	}
}

// 本机真实 /etc/hosts 的形状（无标记）也要能被这条链路判成「没有区段」。
func TestParseRealHostsShapeOnThisMachine(t *testing.T) {
	content := "127.0.0.1\tlocalhost\n127.0.1.1 kentrl\n\n# comment\n::1     ip6-localhost\n127.0.0.1\tdemo.test\n"
	got := Parse(content)
	if got.Present || !got.Usable() {
		t.Errorf("这份形状应当是「没有区段」，实际 Present=%v anomaly=%q", got.Present, got.Anomaly)
	}
	if got.HasRecord("demo.test") {
		t.Error("用户手写行不该被当成本工具已替换")
	}
}

func TestMarkerTextIsTheFixedPair(t *testing.T) {
	wantBegin := "# BEGIN github-net managed hosts v" + strconv.Itoa(ManagedVersion)
	wantEnd := "# END github-net managed hosts v" + strconv.Itoa(ManagedVersion)
	if MarkerBegin() != wantBegin || MarkerEnd() != wantEnd {
		t.Errorf("标记原文和规划定下来的不一致：%q / %q", MarkerBegin(), MarkerEnd())
	}
}
