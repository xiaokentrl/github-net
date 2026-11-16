package app

import (
	"context"
	"github.com/xiaokentrl/github-net/internal/event"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/detector"
	"github.com/xiaokentrl/github-net/internal/hosts"
	"github.com/xiaokentrl/github-net/internal/privilege"
	"github.com/xiaokentrl/github-net/internal/store"
)

// 维护循环（巡检＋自动补位）的行为测试：假探针控制 IP 死活（巡检与复验共用同一个
// 死活表），真 privilege 客户端＋服务端（临时 socket/Hosts）验证整条写链。

type fakeProber struct {
	mu    sync.Mutex
	table map[string]bool // "hostname|ip" → 活着？
	calls int
}

func (f *fakeProber) probe(_ context.Context, tgt detector.Target) (detector.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.table[tgt.Hostname+"|"+tgt.IP] {
		return detector.Result{OK: true, Verified: true}, nil
	}
	return detector.Result{OK: false, Kind: detector.FailTimeout}, nil
}

func (f *fakeProber) alive(host, ip string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.table[host+"|"+ip]
}

// newMaintenanceService 在 newDetectProfileService 之上接齐维护循环需要的四件套：
// 真 privilege 链（临时 socket/Hosts）、假探针、临时配置仓、短节拍。
func newMaintenanceService(t *testing.T, table map[string]bool) (*Service, *fakeProber) {
	t.Helper()
	svc := newDetectProfileService(t)
	prober := &fakeProber{table: table}
	probe := func(_ context.Context, tgt detector.Target) (detector.Result, error) {
		return prober.probe(context.Background(), tgt)
	}
	svc.applyProbe = probe
	svc.maintProbe = probe

	// 真 privilege 链（与 internal/privilege 测试同一形状：临时目录＋unix 端点）。
	dir := t.TempDir()
	hostsPath := filepath.Join(dir, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatalf("写临时 Hosts: %v", err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "test.sock"))
	if err != nil {
		t.Fatalf("监听临时端点: %v", err)
	}
	srv := privilege.NewServer(privilege.NewHostsApplier(hostsPath, filepath.Join(dir, "data")))
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ln)
		close(done)
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	svc.priv = privilege.NewClientAt(ln.Addr().String())
	svc.hostsPath = hostsPath

	cfgStore := config.NewStore(filepath.Join(t.TempDir(), "config", "config.json"),
		map[string]bool{"github": true}, map[string]bool{"live.one": true}, map[string]bool{"code": true})
	svc.settings = cfgStore
	// 候选底账（补位货源从池里来）：临时库，用完关。
	db, derr := store.Open(filepath.Join(t.TempDir(), "detect.db"))
	if derr != nil {
		t.Fatalf("开临时底账: %v", derr)
	}
	t.Cleanup(func() { db.Close() })
	svc.candidateStore = db
	svc.maintInterval = 10 * time.Millisecond
	svc.maintFails = map[string]int{}
	svc.maintNotified = map[string]bool{}
	return svc, prober
}

func seedApplied(t *testing.T, svc *Service, ip string) {
	t.Helper()
	// 检测结果：该 IP 可用。
	svc.detectResults.put("github", "live.one", &accelerator.Result{
		ItemID:     "api",
		Hostnames:  []accelerator.HostOutcome{{Hostname: "api.github.com", Resolved: true}},
		Candidates: []accelerator.Candidate{{ID: "c1", Hostname: "api.github.com", IP: ip, Status: accelerator.StatusAvailable, LatencyMS: 100}},
		Available:  1, ProbesUsed: 1,
	})
	// 候选池：给一个「活 IP」当补位货源（30.30.30.30）。
	// MarkPoolProbe 是 UPDATE-only：池里没这行就更新零行——必须走 Save 插入池行。
	now := time.Now()
	if err := svc.candidateStore.SaveRound("", "github", "live.one", &accelerator.Result{
		ItemID:     "api",
		Hostnames:  []accelerator.HostOutcome{{Hostname: "api.github.com", Resolved: true}},
		Candidates: []accelerator.Candidate{{ID: "pool1", Hostname: "api.github.com", IP: "30.30.30.30", Status: accelerator.StatusAvailable, LatencyMS: 60}},
		Available:  1, ProbesUsed: 1,
		StartedAt: now, FinishedAt: now,
	}); err != nil {
		t.Fatalf("种池行失败: %v", err)
	}
	// 关键前提：先把死 IP **真写进 Hosts**（模拟用户已点过「补充可用IP」）——
	// 巡检的对象是「已写入的 IP」，Hosts 里没有区段时 State() 不是 InUse，循环直接复位。
	// 单 IP 语义：hosts 包「一主机名一规范 IP」防线，全部冗余写入待 schema 专项。
	if err := svc.priv.Apply("api", []hosts.Mapping{{IP: ip, Hostnames: []string{"api.github.com"}}}); err != nil {
		t.Fatalf("预写 Hosts 失败: %v", err)
	}
}

// 两轮巡检判失效 → 自动补位：死 IP 从 Hosts 消失，候选池的活 IP 写入并标「当前生效」。
func TestMaintenanceDetectsDeadAndReplaces(t *testing.T) {
	table := map[string]bool{
		"api.github.com|20.27.177.113":  false, // 已写入的：死了
		"api.github.com|57.155.223.240": false, // 已写入的：死了
		"api.github.com|30.30.30.30":    true,  // 候选池里的补位货源：活着
	}
	svc, prober := newMaintenanceService(t, table)
	seedApplied(t, svc, "20.27.177.113")

	svc.maintenanceCycle() // 第一轮：失败计数
	state1, err1 := svc.priv.State()
	if err1 != nil {
		t.Fatalf("第一轮读状态失败: %v", err1)
	}
	t.Logf("第一轮后 State: %+v 探针=%d 失败计数=%+v", state1, prober.calls, svc.maintFails)
	svc.maintenanceCycle() // 第二轮：判失效＋自动补位

	state, err := svc.priv.State()
	if err != nil {
		t.Fatalf("读状态失败: %v", err)
	}
	t.Logf("第二轮后 State: %+v 探针=%d", state, prober.calls)
	ips := map[string]bool{}
	for _, e := range state.Entries {
		if e.Hostname == "api.github.com" {
			ips[e.IP] = true
		}
	}
	if ips["20.27.177.113"] || ips["57.155.223.240"] {
		t.Errorf("死 IP 应已被换出，实际 %v", ips)
	}
	if !ips["30.30.30.30"] {
		t.Errorf("补位活 IP 应已写入，实际 %v", ips)
	}
	if prober.alive("api.github.com", "30.30.30.30") == false {
		t.Error("补位前探活应确认 30.30.30.30 活着（测试自检）")
	}
}

// 巡检关闭：探针一次都不该被调。
func TestMaintenanceSkipsWhenDisabled(t *testing.T) {
	svc, prober := newMaintenanceService(t, map[string]bool{"github.com|1.2.3.4": true})
	seedApplied(t, svc, "1.2.3.4")
	if _, err := svc.settings.Save(config.Patch{AutoProbe: boolPtr(false)}, 0, "m1"); err != nil {
		t.Fatal(err)
	}
	callsBefore := prober.calls
	svc.maintenanceCycle()
	if prober.calls != callsBefore {
		t.Errorf("巡检关闭时不应探任何 IP，实际多探了 %d 次", prober.calls-callsBefore)
	}
}

// 无已写入内容：State() 不是 InUse，循环复位返回，不 panic 不探针。
func TestMaintenanceNoopWithoutApplied(t *testing.T) {
	svc, prober := newMaintenanceService(t, map[string]bool{})
	svc.maintenanceCycle()
	if prober.calls != 0 {
		t.Errorf("无已写入内容时不应探针，实际 %d 次", prober.calls)
	}
}

// 连续两轮才判失效：第一轮失败只计数不动手。
func TestMaintenanceNeedsTwoConsecutiveFails(t *testing.T) {
	table := map[string]bool{"github.com|3.3.3.3": false}
	svc, _ := newMaintenanceService(t, table)
	seedApplied(t, svc, "3.3.3.3")
	// 候选池放个活货源：若误判失效并补位，Hosts 里会出现 30.30.30.30。
	_ = svc.candidateStore.MarkPoolProbe("api.github.com", "30.30.30.30", "available", 60, time.Now())

	svc.maintenanceCycle()
	state, err := svc.priv.State()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range state.Entries {
		if e.IP == "30.30.30.30" {
			t.Error("第一轮失败就该动手补位＝单次抖动误报，写明连续两轮的语义没落地")
		}
	}
}

func boolPtr(v bool) *bool { return &v }

// 提醒去重（2026-11-16 复核中危修复的钉子）：补位失败/无货源时，同一个死 IP
// 多轮只发**一条**「已失效」提醒——去重键读写错位曾导致每 15 分钟刷屏一条。
func TestMaintenanceNotifiesOncePerDeadIP(t *testing.T) {
	svc, _ := newMaintenanceService(t, map[string]bool{"api.github.com|3.3.3.3": false})
	seedApplied(t, svc, "3.3.3.3")

	var warnings []string
	svc.emit = func(name string, payload any) {
		if name == "notice:push" {
			if p, ok := payload.(event.NoticePush); ok && p.Kind == "warning" {
				warnings = append(warnings, p.Text)
			}
		}
	}
	svc.maintenanceCycle()
	svc.maintenanceCycle()
	svc.maintenanceCycle()

	count := 0
	for _, w := range warnings {
		if strings.Contains(w, "已失效") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("同一死 IP 三轮只该发一条「已失效」提醒，实际 %d 条：%v", count, warnings)
	}
}

// 共享认领（多项目同写一个主机名）时跳过补位并明示——自动补位单方面改写必然撞 ErrSharedOwner。
func TestMaintenanceSkipsSharedClaim(t *testing.T) {
	svc, _ := newMaintenanceService(t, map[string]bool{"api.github.com|3.3.3.3": false})
	seedApplied(t, svc, "3.3.3.3")
	// 制造共享认领：给区段条目再加一个项目认领者（item-b），Hosts 与元数据同步。
	state, err := svc.priv.State()
	if err != nil {
		t.Fatal(err)
	}
	_ = state
	// 直接往元数据里塞第二个项目的认领（走 Apply 语义：item-b 应用同 IP＝并进认领者）。
	if err := svc.priv.Apply("item-b", []hosts.Mapping{{IP: "3.3.3.3", Hostnames: []string{"api.github.com"}}}); err != nil {
		t.Fatalf("制造共享认领失败: %v", err)
	}

	var shared bool
	svc.emit = func(name string, payload any) {
		if name == "notice:push" {
			if p, ok := payload.(event.NoticePush); ok && strings.Contains(p.Text, "共同写入") {
				shared = true
			}
		}
	}
	svc.maintenanceCycle()
	svc.maintenanceCycle()
	if !shared {
		t.Error("共享认领的主机名该跳过补位并明示「请手动处理」")
	}
	// Hosts 不该被单方面改写（3.3.3.3 仍在）。
	state2, err := svc.priv.State()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range state2.Entries {
		if e.Hostname == "api.github.com" && e.IP == "3.3.3.3" {
			found = true
		}
	}
	if !found {
		t.Error("共享认领的映射不该被自动补位改写")
	}
}
