package strategies

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeResolver 是模拟解析器：测试靠它造超时、取消、各类 DNS 错误，全程不碰真实网络。
type fakeResolver struct {
	mu    sync.Mutex
	query string
	got   []net.IPAddr
	err   error
	// wait 为真时不返回结果，只等上下文结束，用来模拟「解析器不回话」。
	wait bool
	// seen 记录每次收到的查询名，用来验证传出去的是规范化后的名字。
	seen []string
}

func (f *fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	f.mu.Lock()
	f.query = host
	f.seen = append(f.seen, host)
	f.mu.Unlock()

	if f.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return f.got, f.err
}

func (f *fakeResolver) recorded() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.query
}

func (f *fakeResolver) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen)
}

func addrs(names ...string) []net.IPAddr {
	out := make([]net.IPAddr, 0, len(names))
	for _, n := range names {
		out = append(out, net.IPAddr{IP: net.ParseIP(n)})
	}
	return out
}

func lookupErr(t *testing.T, err error) *LookupError {
	t.Helper()
	var le *LookupError
	if !errors.As(err, &le) {
		t.Fatalf("不是 *LookupError：%v（%T）", err, err)
	}
	return le
}

func TestDNSTrimsDedupesAndSorts(t *testing.T) {
	res := &fakeResolver{got: addrs(
		"203.0.113.10", "2400:cb00::1", "203.0.113.2", "203.0.113.10",
	)}
	out, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: "github.com", Max: 8})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range out.Candidates {
		got = append(got, c.IP)
	}
	// IPv4 在前、同族按字节升序、重复只留一条。
	want := []string{"203.0.113.2", "203.0.113.10", "2400:cb00::1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("候选列表不对：%v，想要 %v", got, want)
	}
	if out.Truncated {
		t.Fatal("没超上限却报截断")
	}
	if out.Candidates[2].IPv6 != true || out.Candidates[0].IPv6 {
		t.Fatal("IPv6 标记不对")
	}
}

// 发出去的查询名必须是规范化后的名字，界面传进来的大小写和尾点不带进 DNS 报文。
func TestDNSQueriesNormalizedName(t *testing.T) {
	res := &fakeResolver{got: addrs("1.2.3.4")}
	_, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: " GitHub.COM. ", Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.recorded(); got != "github.com" {
		t.Fatalf("传给解析器的名字是 %q，想要 github.com", got)
	}
}

func TestDNSExactMaxIsNotTruncated(t *testing.T) {
	res := &fakeResolver{got: addrs("1.1.1.1", "2.2.2.2")}
	out, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: "a.test", Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	if out.Truncated || len(out.Candidates) != 2 {
		t.Fatalf("刚好等于上限不该算截断：%+v", out)
	}
}

func TestDNSTruncatesAboveMax(t *testing.T) {
	res := &fakeResolver{got: addrs("1.1.1.1", "2.2.2.2", "3.3.3.3")}
	out, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: "a.test", Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || len(out.Candidates) != 2 {
		t.Fatalf("截断标记或条数不对：%+v", out)
	}
}

// 超时：解析器不回话，自己的预算闹钟到点。
func TestDNSTimeoutFromOwnBudget(t *testing.T) {
	res := &fakeResolver{wait: true}
	start := time.Now()
	_, err := newDNSLookup(res, 30*time.Millisecond).run(context.Background(), Input{Hostname: "slow.test", Max: 2})
	if err == nil {
		t.Fatal("没有报错")
	}
	le := lookupErr(t, err)
	if le.Kind != KindTimeout {
		t.Fatalf("档位应为 timeout，实得 %s（%s）", le.Kind, le.Detail)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("预算没生效，等了 %v", elapsed)
	}
}

// 上层给的期限比解析预算短时，用上层那个，不能反而拖长。
func TestDNSTimeoutHonorsShorterParentDeadline(t *testing.T) {
	res := &fakeResolver{wait: true}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := newDNSLookup(res, time.Hour).run(ctx, Input{Hostname: "slow.test", Max: 2})
	le := lookupErr(t, err)
	if le.Kind != KindTimeout {
		t.Fatalf("档位应为 timeout，实得 %s", le.Kind)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("没按上层的短期限收，等了 %v", elapsed)
	}
}

// 取消：调用方主动取消，不能报成超时——上层要据此决定「不算失败、不重试」。
func TestDNSCancelled(t *testing.T) {
	res := &fakeResolver{wait: true}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := newDNSLookup(res, time.Hour).run(ctx, Input{Hostname: "a.test", Max: 2})
	le := lookupErr(t, err)
	if le.Kind != KindCancelled {
		t.Fatalf("档位应为 cancelled，实得 %s（%s）", le.Kind, le.Detail)
	}
}

func TestDNSClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want LookupErrorKind
	}{
		{"域名不存在", &net.DNSError{Name: "a.test", Err: "no such host", IsNotFound: true}, KindNotFound},
		{"解析器出错", &net.DNSError{Name: "a.test", Err: "server misbehaving"}, KindServerFailure},
		{"解析器超时", &net.DNSError{Name: "a.test", Err: "read udp 127.0.0.1:53: i/o timeout"}, KindTimeout},
		{"别的错误", errors.New("boom"), KindOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &fakeResolver{err: tc.err}
			_, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: "a.test", Max: 2})
			le := lookupErr(t, err)
			if le.Kind != tc.want {
				t.Fatalf("档位实得 %s，想要 %s（%s）", le.Kind, tc.want, le.Detail)
			}
		})
	}
}

// 有回话但一条地址都没有，和「域名不存在」是两回事，界面要分开显示。
func TestDNSNoRecords(t *testing.T) {
	res := &fakeResolver{got: nil}
	_, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: "a.test", Max: 2})
	le := lookupErr(t, err)
	if le.Kind != KindNoRecords {
		t.Fatalf("档位应为 noRecords，实得 %s", le.Kind)
	}
}

// 不泄漏查询域名：Go 的 *net.DNSError 打印出来会带域名，
// 所以对外只给分类和一句说明，错误文本里不许出现被查的名字。
func TestDNSErrorTextLeaksNoHostname(t *testing.T) {
	errs := []error{
		&net.DNSError{Name: "secret-host.test", Err: "no such host", IsNotFound: true},
		&net.DNSError{Name: "secret-host.test", Err: "server misbehaving"},
		&net.DNSError{Name: "secret-host.test", Err: "temporary failure", IsTemporary: true},
		errors.New("dial secret-host.test: connection refused"),
	}
	for _, e := range errs {
		res := &fakeResolver{err: e}
		_, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: "secret-host.test", Max: 2})
		got := err.Error()
		if strings.Contains(strings.ToLower(got), "secret-host") {
			t.Fatalf("错误文本里出现了被查域名：%q", got)
		}
	}
	// 对照组：Go 原生的 DNSError 打印出来确实带域名，证明上面那条断言不是空跑——
	// 「不保存原始 error」是唯一让这两句同时成立的原因。
	raw := &net.DNSError{Name: "secret-host.test", Err: "no such host", IsNotFound: true}
	if !strings.Contains(raw.Error(), "secret-host") {
		t.Fatalf("对照组失效，原生错误文本不带域名：%v", raw)
	}
	// 成功路径的候选里也只有地址，没有域名。
	res := &fakeResolver{got: addrs("1.2.3.4")}
	out, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: "secret-host.test", Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(out), "secret-host") {
		t.Fatalf("结果里出现了域名：%v", out)
	}
}

func TestDNSBadInput(t *testing.T) {
	cases := []struct {
		name string
		in   Input
	}{
		{"空主机名", Input{Hostname: "   ", Max: 2}},
		{"只有尾点", Input{Hostname: ".", Max: 2}},
		{"上限为 0", Input{Hostname: "a.test", Max: 0}},
		{"上限为负", Input{Hostname: "a.test", Max: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &fakeResolver{got: addrs("1.2.3.4")}
			_, err := newDNSLookup(res, time.Second).run(context.Background(), tc.in)
			if !errors.Is(err, ErrBadInput) {
				t.Fatalf("应归进 ErrBadInput，实得 %v", err)
			}
			if res.calls() != 0 {
				t.Fatalf("输入不合法却发了查询：%v", res.seen)
			}
		})
	}
}

// 非法域名形状在发查询之前就拦掉：用户自填的「域名」经常是 URL 或带端口的写法。
func TestDNSInvalidNameShape(t *testing.T) {
	cases := []struct {
		name     string
		hostname string
	}{
		{"带端口", "a.test:443"},
		{"带空格", "a b.test"},
		{"带斜杠", "https://a.test"},
		{"连续点", "a..test"},
		{"标签以短横线开头", "-a.test"},
		{"标签过长", strings.Repeat("a", maxLabelLength+1) + ".test"},
		{"总长超限", strings.Repeat("ab.", 90) + "test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := &fakeResolver{got: addrs("1.2.3.4")}
			_, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: tc.hostname, Max: 2})
			le := lookupErr(t, err)
			if le.Kind != KindInvalidName {
				t.Fatalf("档位应为 invalidName，实得 %s（%s）", le.Kind, le.Detail)
			}
			if res.calls() != 0 {
				t.Fatalf("非法域名不该发查询：%v", res.seen)
			}
		})
	}
}

// IPv4 映射的 IPv6 写法算同一条 IPv4；带 zone 的地址去掉 zone，
// 否则这条地址进不了 Hosts 管理区段。
func TestDNSAddressShapes(t *testing.T) {
	res := &fakeResolver{got: []net.IPAddr{
		{IP: net.ParseIP("::ffff:1.2.3.4")},
		{IP: net.ParseIP("fe80::1"), Zone: "eth0"},
		{IP: net.ParseIP("1.2.3.4")},
	}}
	out, err := newDNSLookup(res, time.Second).run(context.Background(), Input{Hostname: "a.test", Max: 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Candidates) != 2 {
		t.Fatalf("映射地址该和它的 IPv4 合成一条：%+v", out.Candidates)
	}
	if out.Candidates[0].IP != "1.2.3.4" || out.Candidates[0].IPv6 {
		t.Fatalf("第一条应是纯 IPv4：%+v", out.Candidates[0])
	}
	if out.Candidates[1].IP != "fe80::1" || !out.Candidates[1].IPv6 {
		t.Fatalf("第二条应是不带 zone 的 IPv6：%+v", out.Candidates[1])
	}
	if strings.Contains(out.Candidates[1].IP, "%") {
		t.Fatal("地址里还带着 zone")
	}
}

// 注册进来的方法必须自带实现，否则界面上有它可选、点检测却是空的。
func TestRegisteredStrategyHasImplementation(t *testing.T) {
	s, ok := Get(StrategyIDDNSHosts)
	if !ok {
		t.Fatal("init 没注册 dns-hosts")
	}
	if s.Lookup == nil {
		t.Fatal("注册进来的方法没有实现")
	}
	if !s.Caps.has(CapabilityCandidateLookup) {
		t.Fatal("能力位缺候选取得")
	}
	// 这里不调 s.Lookup：它会打真实网络。什么时候发起检测由装配层决定，属下一档。
}
