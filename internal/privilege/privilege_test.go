package privilege

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/hosts"
)

// startTestHelper 在临时 socket 上起一个真实的协议服务，Hosts 和数据目录都指向临时文件。
// 返回连它要用的客户端和清理函数；测试里的一切都不碰真实系统。
func startTestHelper(t *testing.T) (*Client, string, string, func()) {
	t.Helper()
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n::1 localhost\n"), 0o644); err != nil {
		t.Fatalf("写临时 Hosts: %v", err)
	}
	dataDir := filepath.Join(dir, "data")

	ln, err := net.Listen("unix", filepath.Join(dir, "test.sock"))
	if err != nil {
		t.Fatalf("监听临时端点: %v", err)
	}
	srv := NewServer(NewHostsApplier(hostsPath, dataDir))
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ln)
		close(done)
	}()
	cleanup := func() {
		_ = ln.Close()
		<-done
	}
	return NewClientAt(ln.Addr().String()), hostsPath, dataDir, cleanup
}

func TestPingAndWhitelistRejections(t *testing.T) {
	c, _, _, cleanup := startTestHelper(t)
	defer cleanup()

	if err := c.Ping(); err != nil {
		t.Fatalf("ping 该通: %v", err)
	}
	// 白名单外的操作被服务拒，错误里带原因、类型是「服务拒绝」。
	_, err := c.call(Request{Op: "shutdown"})
	if !errors.Is(err, ErrHelperRefused) || !strings.Contains(err.Error(), "不认识的操作") {
		t.Fatalf("白名单外的操作该被拒（ErrHelperRefused + bad_op），实际 %v", err)
	}
	// apply 缺项目 ID、缺映射：同一条白名单门挡下。
	err = c.Apply("", []hosts.Mapping{{IP: "203.0.113.10", Hostnames: []string{"api.github.com"}}})
	if !errors.Is(err, ErrHelperRefused) {
		t.Fatalf("缺项目 ID 的 apply 该被拒，实际 %v", err)
	}
	err = c.Apply("item-1", nil)
	if !errors.Is(err, ErrHelperRefused) {
		t.Fatalf("缺映射的 apply 该被拒，实际 %v", err)
	}
}

func TestUnavailableAndProtocolErrors(t *testing.T) {
	// 端点上没有服务：归为「服务不可用」，不是协议错误。
	c := NewClientAt(filepath.Join(t.TempDir(), "missing.sock"))
	if err := c.Ping(); !errors.Is(err, ErrHelperUnavailable) {
		t.Fatalf("连不上的服务该归为不可用，实际 %v", err)
	}
}

func TestApplyThenRestoreItemRoundTrip(t *testing.T) {
	c, hostsPath, dataDir, cleanup := startTestHelper(t)
	defer cleanup()

	original, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("读原始 Hosts: %v", err)
	}

	// 应用一个项目：用户内容那一行必须原样保留，新映射进管理区段。
	if err := c.Apply("item-1", []hosts.Mapping{{IP: "203.0.113.10", Hostnames: []string{"api.github.com", "codeload.github.com"}}}); err != nil {
		t.Fatalf("apply 不该失败: %v", err)
	}
	content := read(t, hostsPath)
	if !strings.Contains(content, "127.0.0.1 localhost") {
		t.Fatalf("用户原来的行被弄丢了: %q", content)
	}
	for _, want := range []string{"api.github.com", "codeload.github.com", "203.0.113.10"} {
		if !strings.Contains(content, want) {
			t.Fatalf("映射 %s 没写进 Hosts: %q", want, content)
		}
	}
	if _, err := os.Stat(filepath.Join(dataDir, metaFileName)); err != nil {
		t.Fatalf("恢复元数据该落盘: %v", err)
	}
	backup := read(t, filepath.Join(dataDir, backupDirName, backupFileName))
	if backup != string(original) {
		t.Fatalf("备份应是写入前的原文: %q", backup)
	}

	// 同一项目换 IP 再应用：更新而不是留两行。
	if err := c.Apply("item-1", []hosts.Mapping{{IP: "203.0.113.99", Hostnames: []string{"api.github.com"}}}); err != nil {
		t.Fatalf("再次 apply 不该失败: %v", err)
	}
	content = read(t, hostsPath)
	if strings.Contains(content, "203.0.113.10") {
		t.Fatalf("旧 IP 该被换掉: %q", content)
	}
	if !strings.Contains(content, "203.0.113.99") {
		t.Fatalf("新 IP 该生效: %q", content)
	}

	// 单项还原：回到应用前状态；账本清空后元数据和备份一起删。
	if err := c.RestoreItem("item-1"); err != nil {
		t.Fatalf("restoreItem 不该失败: %v", err)
	}
	if got := read(t, hostsPath); got != string(original) {
		t.Fatalf("还原后应回到原文，实际 %q", got)
	}
	if _, err := os.Stat(filepath.Join(dataDir, metaFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("全部退光后元数据该删掉，实际 stat 错误 %v", err)
	}
}

func TestRestoreAllReturnsCount(t *testing.T) {
	c, hostsPath, _, cleanup := startTestHelper(t)
	defer cleanup()

	// 空账本上全量还原：0 个，不是错误。
	n, err := c.RestoreAll()
	if err != nil || n != 0 {
		t.Fatalf("空账本的 restoreAll 该是 (0, nil)，实际 (%d, %v)", n, err)
	}
	if err := c.Apply("item-a", []hosts.Mapping{{IP: "203.0.113.10", Hostnames: []string{"a.github.com"}}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := c.Apply("item-b", []hosts.Mapping{{IP: "203.0.113.11", Hostnames: []string{"b.github.com"}}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	n, err = c.RestoreAll()
	if err != nil || n != 2 {
		t.Fatalf("restoreAll 该还 2 个项目，实际 (%d, %v)", n, err)
	}
	if got := read(t, hostsPath); strings.Contains(got, "a.github.com") || strings.Contains(got, "b.github.com") {
		t.Fatalf("还原后不该再有工具映射: %q", got)
	}
}

func TestCorruptMetadataIsRejected(t *testing.T) {
	c, _, dataDir, cleanup := startTestHelper(t)
	defer cleanup()

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("建数据目录: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, metaFileName), []byte("{不是JSON"), 0o600); err != nil {
		t.Fatalf("写坏元数据: %v", err)
	}
	err := c.Apply("item-1", []hosts.Mapping{{IP: "203.0.113.10", Hostnames: []string{"api.github.com"}}})
	if !errors.Is(err, ErrHelperRefused) || !strings.Contains(err.Error(), "元数据") {
		t.Fatalf("元数据损坏该被明确拒绝（拒绝 + 提到元数据），实际 %v", err)
	}
}

func TestFrameRoundTripAndLimits(t *testing.T) {
	var got []byte
	req := Request{Op: OpApply, ItemID: "item-1", Mappings: []hosts.Mapping{{IP: "203.0.113.10", Hostnames: []string{"a.test"}}}}
	if err := writeFrame(func(b []byte) (int, error) { got = append(got, b...); return len(b), nil }, req); err != nil {
		t.Fatalf("写帧: %v", err)
	}
	body, err := readFrame(func(b []byte) (int, error) {
		if len(got) == 0 {
			return 0, errors.New("empty")
		}
		n := copy(b, got)
		got = got[n:]
		return n, nil
	})
	if err != nil {
		t.Fatalf("读帧: %v", err)
	}
	var back Request
	if err := unmarshalInto(body, &back); err != nil || back.Op != OpApply || back.ItemID != "item-1" {
		t.Fatalf("帧来回不该变形: %+v (err=%v)", back, err)
	}

	// 长度超上限的帧按协议错误拒掉。
	big := make([]byte, MaxFrameSize+1)
	if _, err := readFrame(func(b []byte) (int, error) { return copy(b, big), nil }); !errors.Is(err, ErrProtocol) {
		t.Fatalf("超长帧该是协议错误，实际 %v", err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s: %v", path, err)
	}
	return string(raw)
}
