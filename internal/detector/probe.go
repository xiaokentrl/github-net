// Package detector 是候选地址的共享探测层：给定一个已经解析出来的 IP，
// 量出「连得上吗、握手上吗、证书对这个域名有效吗」。
//
// 分工要划清：解析域名取候选是加速方法自己的事（在 internal/accelerator/strategies），
// 本包只接地址，绝不反过来解析——Target.IP 不是合法 IP 字面量就当场拒，
// 这样「探测」这条路不可能偷偷变成一次 DNS 查询。
// 本包也不写 Hosts、不改系统任何状态，只做连接、握手和计时。
//
// 并发闸也不在这里：全局同时最多 8 个网络请求是编排层的账，
// 本包一次调用只探一个地址，谁调、调几次都不影响这里的计时口径。
package detector

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

// probeBudget 是总纲定的探测预算：一个候选地址的 TCP 连接加 TLS 握手合计最多 2 秒。
// 上层给的期限更短就跟上层那个，本包不把自己的预算套到上层头上。
const probeBudget = 2 * time.Second

// defaultPort 是候选探测的默认端口：首期方法都是给 HTTPS 站点找地址。
const defaultPort = 443

// Dialer 是拨号动作的接缝：线上用 *net.Dialer，测试换成假的，
// 才能在不碰外网的前提下跑超时、取消和各种连接错误分支。
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

var systemDialer Dialer = &net.Dialer{}

// ErrBadTarget 表示调用方给的探测对象本身不合法（地址不是 IP 字面量、端口越界、
// 要验证书却没给主机名）。这跟「连不上」是两回事：连不上是探测结果，
// 输入不合法是编程错误，不能混在一种返回里。
var ErrBadTarget = errors.New("探测目标不合法")

// Target 是一次探测的输入。
type Target struct {
	// IP 是候选地址，必须是 IP 字面量（IPv6 不带 zone）。本包只拨这个地址，不解析名字。
	IP string
	// Hostname 是证书要匹配的目标主机名；只探 TCP 时可以留空。
	Hostname string
	// Port 为 0 时按 443 处理。
	Port int
}

// Result 是一次探测的结论。
//
// 只有耗时和结论，没有原始错误：Go 的拨错和证书错误文本里会带地址、域名和证书内容，
// 而规划要求不把主机名写进日志。所以每档失败都给一句固定文案，原始错误一律不外传。
type Result struct {
	// OK 表示这一档探测走通了：TCP 探测连上即算，HTTPS 探测还要握手成功。
	OK bool
	// LatencyMS 是 TCP 连接耗时；连接失败时是从开始到失败的用时。
	LatencyMS int64
	// TLSMS 是 TLS 握手耗时，没做 TLS（或连接阶段就没过）时为 0。
	TLSMS int64
	// Verified 表示证书链可信、且对 Target.Hostname 有效。只有 HTTPS 探测会给真。
	Verified bool
	// Kind 是失败分类，成功时为空串。界面按它取文案，不解析 Detail。
	Kind FailureKind
	// Detail 是给看一眼的人话，不含主机名和地址。
	Detail string
}

// FailureKind 是给上层映射稳定错误码用的分类。
type FailureKind string

const (
	FailTimeout       FailureKind = "timeout"       // 到自己那份预算或上层期限
	FailCancelled     FailureKind = "cancelled"     // 调用方取消
	FailRefused       FailureKind = "refused"       // 对端明确拒绝（端口没在听）
	FailUnreachable   FailureKind = "unreachable"   // 路由不通、网络不可达
	FailHandshake     FailureKind = "handshake"     // 连上了但 TLS 握手没成（对端不说 TLS）
	FailCertUntrusted FailureKind = "certUntrusted" // 证书链不受信
	FailCertMismatch  FailureKind = "certMismatch"  // 证书不是签给这个主机名的
	FailCertExpired   FailureKind = "certExpired"   // 证书过期或还没生效
	FailOther         FailureKind = "other"
)

// stage 记录失败发生在哪一段：连接段和握手段的兜底归档不一样。
type stage int

const (
	stageTCP stage = iota
	stageTLS
)

// prober 把接缝（拨号器、信任链、预算）收到一处，
// 这样测试能同时跑「真回环连接」和「假拨号器给的各类错误」两条路。
type prober struct {
	dial   Dialer
	pool   *x509.CertPool // nil 表示用系统信任链
	budget time.Duration
}

var defaultProber = prober{dial: systemDialer, budget: probeBudget}

// ProbeTCP 只量「这个地址的这个端口连不连得上」。
func ProbeTCP(ctx context.Context, t Target) (Result, error) {
	return defaultProber.tcp(ctx, t)
}

// ProbeHTTPS 量完整的可用链路：TCP 连接 → TLS 握手 → 证书校验。
// 只握手，不发任何 HTTP 请求，也不读正文——候选指标要的是连接与握手耗时。
func ProbeHTTPS(ctx context.Context, t Target) (Result, error) {
	return defaultProber.https(ctx, t)
}

func (p prober) tcp(ctx context.Context, t Target) (Result, error) {
	addr, err := t.addr()
	if err != nil {
		return Result{}, err
	}
	runCtx, cancel, budgetFired := p.withBudget(ctx)
	defer cancel()

	start := time.Now()
	conn, err := p.dial.DialContext(runCtx, "tcp", addr)
	elapsed := time.Since(start)
	if err != nil {
		return p.fail(err, budgetFired(), stageTCP, elapsed, 0), nil
	}
	_ = conn.Close()
	return Result{OK: true, LatencyMS: millis(elapsed)}, nil
}

func (p prober) https(ctx context.Context, t Target) (Result, error) {
	addr, err := t.addr()
	if err != nil {
		return Result{}, err
	}
	if t.Hostname == "" {
		return Result{}, fmt.Errorf("%w：校验证书必须给主机名", ErrBadTarget)
	}
	// 一份期限管两步：规划说的是「TCP + TLS 合计最多 2 秒」，不是各给 2 秒。
	runCtx, cancel, budgetFired := p.withBudget(ctx)
	defer cancel()

	start := time.Now()
	conn, err := p.dial.DialContext(runCtx, "tcp", addr)
	tcpCost := time.Since(start)
	if err != nil {
		return p.fail(err, budgetFired(), stageTCP, tcpCost, 0), nil
	}
	defer func() { _ = conn.Close() }()

	cfg := &tls.Config{ServerName: t.Hostname, RootCAs: p.pool, MinVersion: tls.VersionTLS12}
	tlsConn := tls.Client(conn, cfg)
	handshakeStart := time.Now()
	if err := tlsConn.HandshakeContext(runCtx); err != nil {
		return p.fail(err, budgetFired(), stageTLS, tcpCost, time.Since(handshakeStart)), nil
	}
	return Result{
		OK:        true,
		LatencyMS: millis(tcpCost),
		TLSMS:     millis(time.Since(handshakeStart)),
		Verified:  true,
	}, nil
}

// addr 把目标拼成拨号用的地址，顺手把「IP 字段里塞了域名」这条路堵死：
// 不合法就直接拒，绝不交给拨号器去解析。报错只说形状问题，不回显传进来的字符串。
func (t Target) addr() (string, error) {
	if net.ParseIP(t.IP) == nil {
		return "", fmt.Errorf("%w：地址不是 IP 字面量，本包不做域名解析", ErrBadTarget)
	}
	port := t.Port
	if port == 0 {
		port = defaultPort
	}
	if port < 1 || port > 65535 {
		return "", fmt.Errorf("%w：端口 %d 不在 1 到 65535", ErrBadTarget, port)
	}
	return net.JoinHostPort(t.IP, fmt.Sprint(port)), nil
}

// withBudget 只在「上层没给期限」或「给的期限比探测预算长」时套一层自己的闹钟。
// budgetFired 用来分清超时是谁造成的：自己的闹钟响了算探测超时，
// 上层的期限到了就按上层期限处理，不重复计时。
func (p prober) withBudget(ctx context.Context) (context.Context, context.CancelFunc, func() bool) {
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= p.budget {
		return ctx, func() {}, func() bool { return false }
	}
	runCtx, cancel := context.WithTimeout(ctx, p.budget)
	return runCtx, cancel, func() bool { return errors.Is(runCtx.Err(), context.DeadlineExceeded) }
}

// fail 把失败组装成结论：分类、固定文案、两段耗时。原始 error 不出这个函数。
// 连不上不是 Go 意义上的 error——那是探测结果，所以这里出结论、error 一律给 nil。
func (p prober) fail(err error, budgetFired bool, st stage, tcpCost, tlsCost time.Duration) Result {
	kind, detail := p.classify(err, budgetFired, st)
	res := Result{Kind: kind, Detail: detail, LatencyMS: millis(tcpCost)}
	if tlsCost > 0 {
		res.TLSMS = millis(tlsCost)
	}
	return res
}

// classify 把 Go 的拨号与证书错误收成有限几档。
// 顺序有意为之：证书是「这个地址对这个域名到底行不行」的确凿结论，先认；
// 再认计时与取消归谁，最后才是连接层的错和兜底。
func (p prober) classify(err error, budgetFired bool, st stage) (FailureKind, string) {
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return FailCertMismatch, "证书不是签给这个主机名的"
	}
	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		return FailCertUntrusted, "证书链不受信任"
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		if invalid.Reason == x509.Expired {
			return FailCertExpired, "证书已过期或还没生效"
		}
		return FailCertUntrusted, "证书本身有问题"
	}
	var verifyErr *tls.CertificateVerificationError
	if errors.As(err, &verifyErr) {
		return FailCertUntrusted, "证书校验没通过"
	}

	switch {
	case budgetFired:
		return FailTimeout, fmt.Sprintf("连接与握手超过 %s 预算", p.budget)
	case errors.Is(err, context.Canceled):
		return FailCancelled, "探测已取消"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return FailTimeout, "对端没有按时响应"
	case errors.Is(err, syscall.ECONNREFUSED):
		return FailRefused, "端口拒绝连接"
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH):
		return FailUnreachable, "到该地址的路走不通"
	}

	// 走到这里说明是握手段的其它失败（对端不说 TLS、半路断开、alert）。
	if st == stageTLS {
		return FailHandshake, "TLS 握手没成功"
	}
	return FailOther, "探测失败"
}

func millis(d time.Duration) int64 {
	return int64(d / time.Millisecond)
}
