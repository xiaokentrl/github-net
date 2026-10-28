package hosts

import (
	"errors"
	"strings"
	"testing"
)

func rec(ip string, hosts ...string) Record {
	return Record{IP: ip, Hostnames: hosts}
}

func block(lines ...string) string {
	all := append([]string{MarkerBegin()}, lines...)
	return strings.Join(append(all, MarkerEnd()), "\n") + "\n"
}

func wantErr(t *testing.T, err error, target error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s：应报错，得到 nil", what)
	}
	if !errors.Is(err, target) {
		t.Errorf("%s：错误类型不对，期望 %v，得到 %v", what, target, err)
	}
}

func TestRenderAppendsWhenNoBlock(t *testing.T) {
	content := "127.0.0.1 localhost\n::1 localhost\n"
	got, err := Render(content, []Record{rec("140.82.113.4", "github.com")})
	if err != nil {
		t.Fatal(err)
	}
	want := content + block("140.82.113.4 github.com")
	if got != want {
		t.Errorf("追加结果不对:\n得到 %q\n期望 %q", got, want)
	}
	// 追加不能动用户行：前缀必须逐字节是原文。
	if !strings.HasPrefix(got, content) {
		t.Error("区段前的用户内容被改了")
	}
}

func TestRenderKeepsOutsideContentVerbatim(t *testing.T) {
	// 大小写、行内多空格、注释、空行、IPv6 混排，全在区段外。
	content := "# 我的 hosts\n\n127.0.0.1   LOCALHOST   # 手写的\n::1\tip6-localhost\n\n"
	got, err := Render(content, []Record{rec("151.101.1.69", "objects.githubusercontent.com")})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.SplitN(got, MarkerBegin(), 2)
	if len(parts) != 2 {
		t.Fatalf("写出结果的区段形状不对: %q", got)
	}
	before, after := parts[0], parts[1]
	if before != strings.TrimSuffix(content, "\n")+"\n" {
		t.Errorf("区段外的内容不是逐字保留:\n得到 %q\n期望 %q", before, content)
	}
	if !strings.HasSuffix(after, MarkerEnd()+"\n") {
		t.Errorf("区段没有以结束标记收尾: %q", after)
	}
	if strings.Contains(after, "LOCALHOST") {
		t.Error("用户行被搬进区段了")
	}
}

func TestRenderReplacesExistingBlockInPlace(t *testing.T) {
	content := "127.0.0.1 localhost\n" + block("1.1.1.1 old.example") + "9.9.9.9 tail.example\n"
	got, err := Render(content, []Record{rec("2.2.2.2", "mid.example")})
	if err != nil {
		t.Fatal(err)
	}
	want := "127.0.0.1 localhost\n" + block("2.2.2.2 mid.example") + "9.9.9.9 tail.example\n"
	if got != want {
		t.Errorf("原地替换不对:\n得到 %q\n期望 %q", got, want)
	}
	// 区段前后的用户行都还在，顺序也没变。
	if strings.Index(got, "localhost") > strings.Index(got, MarkerBegin()) {
		t.Error("前面的用户行跑到区段后面了")
	}
	if !strings.Contains(got, "tail.example") {
		t.Error("后面的用户行被吃掉了")
	}
}

func TestRenderSortsAndDedupes(t *testing.T) {
	in := []Record{
		rec("140.82.114.4", "www.github.com", "github.com"),
		rec("151.101.1.69", "objects.githubusercontent.com"),
		rec("140.82.114.4", "github.com"), // 与第一条重复，必须只出现一次
	}
	first, err := Render("", in)
	if err != nil {
		t.Fatal(err)
	}
	// 打乱输入顺序必须出同一个字节串——区段摘要要能跨进程比较就靠这条。
	shuffled := []Record{in[1], in[2], in[0]}
	second, err := Render("", shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("输入顺序影响输出:\n%s\n---\n%s", first, second)
	}
	want := block("140.82.114.4 github.com www.github.com", "151.101.1.69 objects.githubusercontent.com")
	if first != want {
		t.Errorf("排序/去重结果不对:\n得到 %q\n期望 %q", first, want)
	}
}

func TestRenderRejectsBadRecords(t *testing.T) {
	cases := []struct {
		name string
		recs []Record
	}{
		{"IP 不是地址", []Record{rec("not-an-ip", "github.com")}},
		{"IP 为空", []Record{rec("", "github.com")}},
		{"没有主机名", []Record{rec("140.82.113.4")}},
		{"主机名带端口", []Record{rec("140.82.113.4", "github.com:443")}},
		{"主机名是路径", []Record{rec("140.82.113.4", "github.com/x")}},
		{"主机名本身是 IP", []Record{rec("140.82.113.4", "140.82.113.5")}},
		{"主机名只有一个点", []Record{rec("140.82.113.4", ".")}},
		{"同一主机名两个 IP", []Record{rec("1.1.1.1", "a.example"), rec("2.2.2.2", "a.example")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Render("", tc.recs)
			wantErr(t, err, ErrBadRecord, tc.name)
			if _, err := Render(block("9.9.9.9 keep.example"), tc.recs); !errors.Is(err, ErrBadRecord) {
				t.Errorf("%s：已有区段时也要拒，得到 %v", tc.name, err)
			}
		})
	}
}

func TestRenderTreatsSameIPTwoWaysAsOneRecord(t *testing.T) {
	got, err := Render("", []Record{rec("0:0:0:0:0:0:0:1", "a.example"), rec("::1", "b.example")})
	if err != nil {
		t.Fatalf("同一 IP 的两种写法不该算冲突: %v", err)
	}
	if want := block("::1 a.example b.example"); got != want {
		t.Errorf("得到 %q，期望 %q", got, want)
	}
}

func TestRenderRefusesUnreadableBlock(t *testing.T) {
	cases := []struct {
		name    string
		content string
		needle  string
	}{
		{"有头没尾", MarkerBegin() + "\n1.1.1.1 a.example\n", "没有结束标记"},
		{"有尾没头", "1.1.1.1 a.example\n" + MarkerEnd() + "\n", "没有开始标记"},
		{"标记重复", block("1.1.1.1 a.example") + block("2.2.2.2 b.example"), "重复"},
		{"顺序颠倒", MarkerEnd() + "\n" + MarkerBegin() + "\n", "重复"},
		{"版本未知", "# BEGIN github-net managed hosts v2\n1.1.1.1 a.example\n# END github-net managed hosts v2\n", "v2"},
		{"区段里有注释", block("1.1.1.1 a.example  # 手加的"), "外部改过"},
		{"区段里有裸 IP 行", block("1.1.1.1"), "外部改过"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Render(tc.content, []Record{rec("3.3.3.3", "c.example")})
			wantErr(t, err, ErrConflict, tc.name)
			if !strings.Contains(err.Error(), tc.needle) {
				t.Errorf("错误里没带上原判读原因（含 %q）: %v", tc.needle, err)
			}
			// 清空请求同样要拒：结构判读不了时，「删掉重来」也是猜测式修复。
			if _, err := Render(tc.content, nil); !errors.Is(err, ErrConflict) {
				t.Errorf("%s：清空也必须拒，得到 %v", tc.name, err)
			}
		})
	}
}

func TestRenderRemovesBlockWhenNothingLeft(t *testing.T) {
	content := "127.0.0.1 localhost\n" + block("1.1.1.1 a.example") + "tail line\n"
	got, err := Render(content, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "127.0.0.1 localhost\ntail line\n"
	if got != want {
		t.Errorf("清空后应连标记一起撤走:\n得到 %q\n期望 %q", got, want)
	}
	if strings.Contains(got, "github-net managed hosts") {
		t.Error("标记残留在文件里")
	}
	if Parse(got).Present {
		t.Error("判读结果还说有区段")
	}
}

func TestRenderLeavesFileWithoutBlockUntouched(t *testing.T) {
	for _, content := range []string{"", "127.0.0.1 localhost", "127.0.0.1 localhost\n\n", "# 注释\r\n"} {
		got, err := Render(content, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got != content {
			t.Errorf("没有区段又要清空时不该动文件: 得到 %q，期望 %q", got, content)
		}
	}
}

func TestRenderIsIdempotent(t *testing.T) {
	recs := []Record{rec("140.82.113.4", "github.com"), rec("151.101.1.69", "a.example", "b.example")}
	content := "127.0.0.1 localhost\n"
	once, err := Render(content, recs)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Render(once, recs)
	if err != nil {
		t.Fatal(err)
	}
	if once != twice {
		t.Errorf("同样映射写两次应当不动，得到:\n%q\n%q", once, twice)
	}
	// 写进去的区段必须能被读回来，且读回来的就是这批映射。
	read := Parse(once)
	if !read.Present || !read.Usable() {
		t.Fatalf("写出去的区段判读不了: %+v", read)
	}
	if len(read.Records) != 2 {
		t.Fatalf("读回来的行数 = %d，期望 2: %+v", len(read.Records), read.Records)
	}
	// 排序键是每行最小的主机名，所以 a.example 那行在前，github.com 那行在后。
	if got := strings.Join(read.Records[0].Hostnames, ",") + "/" + read.Records[0].IP; got != "a.example,b.example/151.101.1.69" {
		t.Errorf("第一行 = %q", got)
	}
	if got := strings.Join(read.Records[1].Hostnames, ",") + "/" + read.Records[1].IP; got != "github.com/140.82.113.4" {
		t.Errorf("第二行 = %q", got)
	}
}

func TestRenderRoundTripRestoresOriginalBytes(t *testing.T) {
	// 追加再清空，必须回到原文逐字节一致——这是「不插空行」这条决定的直接收益。
	for _, content := range []string{"", "\n", "127.0.0.1 localhost\n", "# a\r\n# b\r\n", "a\n\n\n"} {
		with, err := Render(content, []Record{rec("1.1.1.1", "x.example")})
		if err != nil {
			t.Fatal(err)
		}
		if with == content {
			t.Errorf("加了一条映射却和原文一样: %q", with)
		}
		cleared, err := Render(with, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cleared != content {
			t.Errorf("清空后没回到原文:\n得到 %q\n期望 %q", cleared, content)
		}
	}
}

func TestRenderKeepsCRLFStyle(t *testing.T) {
	content := "127.0.0.1 localhost\r\n::1 ip6-localhost\r\n"
	got, err := Render(content, []Record{rec("1.1.1.1", "x.example")})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "\n") && strings.Count(got, "\n") != strings.Count(got, "\r\n") {
		t.Errorf("行尾风格混用了（LF 与 CRLF 同时出现）: %q", got)
	}
	if !strings.Contains(got, "\r\n1.1.1.1 x.example\r\n") {
		t.Errorf("新写的区段没跟着用 CRLF: %q", got)
	}
	// 混用行尾的文件（原文就混）不做统一：判读吃得住，写出仍按第一处 CRLF 走。
	if !Parse(got).Usable() {
		t.Errorf("写出去的区段读不回来: %+v", Parse(got))
	}
}

func TestRenderTerminatesUnterminatedLastLine(t *testing.T) {
	// 原文最后一条没有换行：往末尾追加时必须先把它补成整行，否则区段标记会和用户行粘一起。
	got, err := Render("127.0.0.1 localhost", []Record{rec("1.1.1.1", "x.example")})
	if err != nil {
		t.Fatal(err)
	}
	want := "127.0.0.1 localhost\n" + block("1.1.1.1 x.example")
	if got != want {
		t.Errorf("得到 %q，期望 %q", got, want)
	}
}

func TestRenderDoesNotWriteWhenItRefuses(t *testing.T) {
	// 拒的时候返回空串，调用方不可能拿着它落盘。
	for _, content := range []string{MarkerBegin() + "\n", block("bad line #"), "# BEGIN github-net managed hosts v9\n# END github-net managed hosts v9\n"} {
		got, err := Render(content, []Record{rec("1.1.1.1", "x.example")})
		if err == nil {
			t.Fatalf("应拒绝: %q", content)
		}
		if got != "" {
			t.Errorf("拒绝时还返回了内容: %q", got)
		}
	}
}
