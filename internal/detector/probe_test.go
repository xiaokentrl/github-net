package detector

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeDialer 替下真拨号：记下每次拿到的 network/addr，并按用例给回指定的错误。
// 「本包绝不解析域名」这条只能靠它证明——地址不合法时必须一次都没拨过。
type fakeDialer struct {
	calls []string
	fail  error
	block bool // true 表示一直等到 ctx 结束再返回 ctx 的错误
}

func (f *fakeDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	f.calls = append(f.calls, network+" "+addr)
	if f.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if f.fail != nil {
		return nil, f.fail
	}
	return nil, errors.New("不该走到这里")
}

func (f *fakeDialer) count() int { return len(f.calls) }

// fastProber 给测试用的短预算，别把一轮测试拖成两秒。
func fastProber(d Dialer) prober {
	return prober{dial: d, budget: 50 * time.Millisecond}
}

const testHostname = "probe.test"

// --- 输入门：只接 IP 字面量 ---

func TestTargetRejectsNonIPLiteralAndNeverDials(t *testing.T) {
	d := &fakeDialer{}
	p := fastProber(d)

	res, err := p.https(context.Background(), Target{IP: "leaky.example", Hostname: testHostname})
	if !errors.Is(err, ErrBadTarget) {
		t.Fatalf("塞域名必须当场拒，实得 err=%v result=%+v", err, res)
	}
	if d.count() != 0 {
		t.Errorf("拒了还拨过号，等于把探测变成解析：calls=%v", d.calls)
	}
	// 报错不回显传进来的名字，否则日志里就留下被查域名了。
	if strings.Contains(err.Error(), "leaky") {
		t.Errorf("错误文本回显了目标名字：%q", err.Error())
	}
}

func TestTargetPortRules(t *testing.T) {
	tests := []struct {
		name    string
		target  Target
		want    string // 期望拨号地址；空串表示期望被拒
		wantErr bool
	}{
		{name: "0 端口按 443", target: Target{IP: "1.2.3.4"}, want: "tcp 1.2.3.4:443"},
		{name: "显式端口照用", target: Target{IP: "1.2.3.4", Port: 8443}, want: "tcp 1.2.3.4:8443"},
		{name: "IPv6 加方括号", target: Target{IP: "::1"}, want: "tcp [::1]:443"},
		{name: "端口 0 以上越界", target: Target{IP: "1.2.3.4", Port: 70000}, wantErr: true},
		{name: "负端口", target: Target{IP: "1.2.3.4", Port: -1}, wantErr: true},
		{name: "空地址", target: Target{IP: ""}, wantErr: true},
		{name: "带端口的字符串不是 IP", target: Target{IP: "1.2.3.4:443"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDialer{}
			p := fastProber(d)
			_, err := p.tcp(context.Background(), tc.target)
			gotBad := errors.Is(err, ErrBadTarget)
			if gotBad != tc.wantErr {
				t.Fatalf("ErrBadTarget=%v，期望 %v（err=%v）", gotBad, tc.wantErr, err)
			}
			if tc.wantErr {
				if d.count() != 0 {
					t.Errorf("非法目标不该拨号，calls=%v", d.calls)
				}
				return
			}
			if d.count() != 1 || d.calls[0] != tc.want {
				t.Fatalf("拨号地址实得 %v，期望 [%s]", d.calls, tc.want)
			}
		})
	}
}

func TestHTTPSRequiresHostname(t *testing.T) {
	d := &fakeDialer{}
	p := fastProber(d)
	if _, err := p.https(context.Background(), Target{IP: "1.2.3.4"}); !errors.Is(err, ErrBadTarget) {
		t.Fatalf("不给主机名就没法验证书，必须当场拒，实得 %v", err)
	}
	if d.count() != 0 {
		t.Errorf("非法目标不该拨号，calls=%v", d.calls)
	}
}

// --- 失败分类：用假拨号器喂各类错误 ---

func TestClassifyDialErrors(t *testing.T) {
	tests := []struct {
		name     string
		fail     error
		wantKind FailureKind
	}{
		{"拒绝", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, FailRefused},
		{"网络不可达", &net.OpError{Op: "dial", Err: syscall.ENETUNREACH}, FailUnreachable},
		{"主机不可达", &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}, FailUnreachable},
		{"取消", context.Canceled, FailCancelled},
		{"其它错误", errors.New("boom"), FailOther},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := fastProber(&fakeDialer{fail: tc.fail})
			res, err := p.tcp(context.Background(), Target{IP: "1.2.3.4"})
			if err != nil {
				t.Fatalf("连不上是结果，不是 error，实得 %v", err)
			}
			if res.OK {
				t.Error("失败了却报 OK")
			}
			if res.Kind != tc.wantKind {
				t.Errorf("分类实得 %q，期望 %q（detail=%q）", res.Kind, tc.wantKind, res.Detail)
			}
			if res.Detail == "" {
				t.Error("失败必须带一句人话")
			}
			if res.TLSMS != 0 {
				t.Errorf("连接段就失败，不该有 TLS 耗时：%d", res.TLSMS)
			}
		})
	}
}

// classify 是纯函数，直接喂给它构造出来的错误：
// 「父证书没权签这种证书」这一档在真握手里要构造两级证书链才碰得到，
// 但它是证书结论的一条分支，不能因为场景难造就不测——
// 而且只有直接测，才能证明这一行不是删了也没人发现的死代码。
// 同样的直接测也用来钉「失败发生在哪一段」的兜底归档。
func TestClassifyCertAndStageFallback(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		st         stage
		wantKind   FailureKind
		wantDetail string
	}{
		{
			name:       "父证书没权签这种证书",
			err:        &tls.CertificateVerificationError{Err: x509.ConstraintViolationError{}},
			st:         stageTLS,
			wantKind:   FailCertUntrusted,
			wantDetail: "证书校验没通过",
		},
		{
			name:       "握手段的其它失败",
			err:        errors.New("remote error: tls: handshake failure"),
			st:         stageTLS,
			wantKind:   FailHandshake,
			wantDetail: "TLS 握手没成功",
		},
		{
			name:       "连接段的其它失败",
			err:        errors.New("some dial failure"),
			st:         stageTCP,
			wantKind:   FailOther,
			wantDetail: "探测失败",
		},
	}
	p := prober{dial: &fakeDialer{}, budget: 2 * time.Second}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, detail := p.classify(tc.err, false, tc.st)
			if kind != tc.wantKind {
				t.Errorf("分类实得 %q，期望 %q", kind, tc.wantKind)
			}
			if detail != tc.wantDetail {
				t.Errorf("文案实得 %q，期望 %q", detail, tc.wantDetail)
			}
		})
	}
}

// 预算到点必须自己收：上层没给期限、或给的期限比预算长，都按本包的预算截。
func TestOwnBudgetFiresReportsBudget(t *testing.T) {
	parents := []struct {
		name  string
		delay time.Duration // 上层期限；0 表示不给期限
	}{
		{"上层没给期限", 0},
		{"上层期限比预算长", 10 * time.Second},
	}
	for _, parent := range parents {
		t.Run(parent.name, func(t *testing.T) {
			ctx := context.Background()
			if parent.delay > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, parent.delay)
				defer cancel()
			}
			res, err := fastProber(&fakeDialer{block: true}).tcp(ctx, Target{IP: "1.2.3.4"})
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if res.Kind != FailTimeout {
				t.Errorf("自己的预算到点应判 timeout，实得 %q", res.Kind)
			}
			if !strings.Contains(res.Detail, "预算") {
				t.Errorf("自己超时要说清是预算用满，detail=%q", res.Detail)
			}
			// 失败也要留下耗时：界面得知道这 50ms 是真花掉了，不是没开始。
			if res.LatencyMS < 40 {
				t.Errorf("预算 50ms 用满，连接段耗时实得 %d，期望 ≥40", res.LatencyMS)
			}
		})
	}
}

// 上层期限比探测预算短时，超时归上层，不能谎称是自己的预算用满。
func TestUpperDeadlineIsNotMyBudget(t *testing.T) {
	p := prober{dial: &fakeDialer{block: true}, budget: 2 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	res, err := p.tcp(ctx, Target{IP: "1.2.3.4"})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Kind != FailTimeout {
		t.Errorf("实得 %q，期望 timeout", res.Kind)
	}
	if strings.Contains(res.Detail, "预算") {
		t.Errorf("上层期限到了却怪自己预算，detail=%q", res.Detail)
	}
}

// --- 真回环连接：不碰外网，但走真的 TCP 栈 ---

func TestProbeTCPAgainstRealLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("本机开不了回环监听：%v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	res, err := ProbeTCP(context.Background(), Target{IP: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !res.OK || res.Kind != "" || res.Verified {
		t.Errorf("连上了就该只报 OK： %+v", res)
	}

	if err := ln.Close(); err != nil {
		t.Fatalf("关监听失败：%v", err)
	}
	gone, err := ProbeTCP(context.Background(), Target{IP: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if gone.Kind != FailRefused {
		t.Errorf("端口关掉后应判 refused，实得 %q（detail=%q）", gone.Kind, gone.Detail)
	}
}

// --- TLS：自签证书三档 + 明文对端 + 不读正文 ---

type certSpec struct {
	dnsName    string
	expired    bool
	clientOnly bool // true 表示只允许客户端用途，用来造「用途不对」的证书
	serialPad  int64
}

func makeCert(t *testing.T, spec certSpec) (tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败：%v", err)
	}
	notBefore := time.Now().Add(-time.Hour)
	notAfter := time.Now().Add(time.Hour)
	if spec.expired {
		notBefore = time.Now().Add(-2 * time.Hour)
		notAfter = time.Now().Add(-time.Hour)
	}
	serverEKU := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	if spec.clientOnly {
		serverEKU = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1 + spec.serialPad),
		Subject:               pkix.Name{CommonName: spec.dnsName},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           serverEKU,
		DNSNames:              []string{spec.dnsName},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("自签证书失败：%v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("解析证书失败：%v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, leaf
}

// tlsServer 起一个真 TLS 服务，并数出握手完成后从这条连接读到的应用数据字节数。
// 客户端只握手不发请求，所以这个数必须是 0——「不下载正文」就靠它当门。
type tlsServer struct {
	port     int
	cert     tls.Certificate
	plain    bool // true 表示连上就回一句明文，冒充不会说 TLS 的对端
	appBytes chan int
}

func newTLSServer(t *testing.T, cert tls.Certificate, plain bool) *tlsServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("本机开不了回环监听：%v", err)
	}
	s := &tlsServer{port: ln.Addr().(*net.TCPAddr).Port, cert: cert, plain: plain, appBytes: make(chan int, 4)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *tlsServer) serve(conn net.Conn) {
	if s.plain {
		// 先晾一会儿再回明文：这样「握手段失败了」一定留下可观测的耗时。
		time.Sleep(60 * time.Millisecond)
		_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nhello"))
		time.Sleep(200 * time.Millisecond)
		_ = conn.Close()
		return
	}
	tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{s.cert}})
	if err := tlsConn.Handshake(); err != nil {
		_ = conn.Close()
		return
	}
	_ = tlsConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 4096)
	n, _ := tlsConn.Read(buf)
	_ = tlsConn.Close()
	s.appBytes <- n
}

// target 拼出指向本机端口的探测对象；主机名固定，证书签给谁由用例决定。
func (s *tlsServer) target(hostname string) Target {
	return Target{IP: "127.0.0.1", Hostname: hostname, Port: s.port}
}

func TestProbeHTTPSCertificateVerdicts(t *testing.T) {
	good, goodLeaf := makeCert(t, certSpec{dnsName: testHostname, serialPad: 1})
	other, otherLeaf := makeCert(t, certSpec{dnsName: "other.test", serialPad: 2})
	expired, expiredLeaf := makeCert(t, certSpec{dnsName: testHostname, expired: true, serialPad: 3})
	clientOnly, clientOnlyLeaf := makeCert(t, certSpec{dnsName: testHostname, clientOnly: true, serialPad: 4})

	poolOf := func(leaves ...*x509.Certificate) *x509.CertPool {
		pool := x509.NewCertPool()
		for _, l := range leaves {
			pool.AddCert(l)
		}
		return pool
	}

	tests := []struct {
		name       string
		cert       tls.Certificate
		pool       *x509.CertPool
		hostname   string
		wantOK     bool
		wantVerify bool
		wantKind   FailureKind
		wantDetail string
	}{
		{"受信且主机名对", good, poolOf(goodLeaf), testHostname, true, true, "", ""},
		{"不受信", good, poolOf(otherLeaf), testHostname, false, false, FailCertUntrusted, "证书链不受信任"},
		{"主机名不对", other, poolOf(otherLeaf), testHostname, false, false, FailCertMismatch, "证书不是签给这个主机名的"},
		{"已过期", expired, poolOf(expiredLeaf), testHostname, false, false, FailCertExpired, "证书已过期或还没生效"},
		{"用途不对", clientOnly, poolOf(clientOnlyLeaf), testHostname, false, false, FailCertUntrusted, "证书本身有问题"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTLSServer(t, tc.cert, false)
			p := prober{dial: systemDialer, pool: tc.pool, budget: 2 * time.Second}
			res, err := p.https(context.Background(), srv.target(tc.hostname))
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if res.OK != tc.wantOK || res.Verified != tc.wantVerify || res.Kind != tc.wantKind {
				t.Fatalf("结论实得 %+v，期望 ok=%v verified=%v kind=%q", res, tc.wantOK, tc.wantVerify, tc.wantKind)
			}
			if res.Detail != tc.wantDetail {
				t.Errorf("文案实得 %q，期望 %q", res.Detail, tc.wantDetail)
			}
			// 任何一档结论都不许把主机名带出去，日志里不能留下被查对象。
			if strings.Contains(res.Detail, tc.hostname) || strings.Contains(res.Detail, "other.test") {
				t.Errorf("说明里混进了主机名：%q", res.Detail)
			}
			if res.OK {
				waitAppBytes(t, srv, 0)
			}
		})
	}
}

// 对端不说 TLS：连上了但握手没成，不能报成证书问题也不能报成连不上。
// 这里走对外的 ProbeHTTPS，顺手钉住「ProbeHTTPS 真的做了 TLS 这一段」。
func TestProbeHTTPSAgainstPlainEndpoint(t *testing.T) {
	srv := newTLSServer(t, tls.Certificate{}, true)
	res, err := ProbeHTTPS(context.Background(), srv.target(testHostname))
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Kind != FailHandshake {
		t.Fatalf("实得 %q（detail=%q），期望 handshake", res.Kind, res.Detail)
	}
	if res.OK || res.Verified {
		t.Errorf("握手没成却报通了： %+v", res)
	}
	if res.TLSMS < 50 {
		t.Errorf("握手失败也要记下握手段耗时（对端晾了 60ms），实得 %d", res.TLSMS)
	}
	if res.LatencyMS < 0 {
		t.Errorf("连接段耗时不该为负：%d", res.LatencyMS)
	}
}

// waitAppBytes 验服务端在这条连接上读到的应用数据字节数。
func waitAppBytes(t *testing.T, srv *tlsServer, want int) {
	t.Helper()
	select {
	case got := <-srv.appBytes:
		if got != want {
			t.Errorf("服务端读到应用数据 %d 字节，期望 %d——探测只该握手，不该发请求", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Error("没等到服务端的读数结果")
	}
}

// 结论文案里不许出现地址：真回环跑一遍，逐条字段查。
func TestResultNeverLeaksAddress(t *testing.T) {
	p := prober{dial: systemDialer, budget: 2 * time.Second}
	res, err := p.tcp(context.Background(), Target{IP: "127.0.0.1", Port: 1})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if res.Kind != FailRefused && res.Kind != FailUnreachable && res.Kind != FailOther {
		t.Fatalf("端口 1 连不上是必然的，实得 %q", res.Kind)
	}
	if strings.Contains(res.Detail, "127.0.0.1") {
		t.Errorf("说明里混进了地址：%q", res.Detail)
	}
}
