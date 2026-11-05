package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/hosts"
	"github.com/xiaokentrl/github-net/internal/store"
)

// 逐条地址那一格的「连上要多久、这条通不通」只能来自候选池里同一条地址的最后一次定格。
// 这里钉三件事：只认同名同址那一行、没有就留空、挂了定格不许动 applied/partial/none 的判定。
func TestAttachEntryProbesMatchesOnlyThisAddress(t *testing.T) {
	c := mustCatalog(t)
	written, content := writtenState(t, "127.0.0.1 localhost\n", map[string][]hosts.Mapping{
		// gh-main 三个主机名只写 github.com 一条：另两条没有地址，不该拿到任何定格。
		"gh-main": {{IP: "140.82.113.4", Hostnames: []string{"github.com"}}},
	}, "gh-main")
	probes := map[string]EntryProbe{
		// 同域名同地址：只有这一行才是 github.com 那格的答案。
		probeKey("github.com", "140.82.113.4"):     {Status: "available", LatencyMS: 79, MeasuredAt: "2026-11-06T01:00:00Z"},
		probeKey("github.com", "203.0.113.9"):      {Status: "unavailable", LatencyMS: 0, MeasuredAt: "2026-11-06T01:00:00Z"},
		probeKey("api.github.com", "140.82.113.4"): {Status: "available", LatencyMS: 41, MeasuredAt: "2026-11-06T01:00:00Z"},
	}

	states := AttachEntryProbes(HostsStates(c, readHosts(t, content), AppliedState{Known: true, Written: written}, 5), probes)
	want := []HostEntryState{
		{Hostname: "github.com", IP: "140.82.113.4", Written: true, ProbeStatus: "available", LatencyMS: 79, MeasuredAt: "2026-11-06T01:00:00Z"},
		// api.github.com 没写、区段里也没有它的行：没有地址就没有定格，
		// 不拿同域名的 203.0.113.9（连不通）也不拿别的域名同地址的 41ms 顶上去。
		{Hostname: "api.github.com", IP: "", Written: false},
		{Hostname: "gist.github.com", IP: "", Written: false},
	}
	if got := stateFor(t, states, "gh-main").Entries; !reflect.DeepEqual(got, want) {
		t.Errorf("挂上定格后的逐条事实 = %+v，期望 %+v", got, want)
	}

	// 判定只看认领关系：挂定格前后的状态档和原因必须一字不变。
	bare := HostsStates(c, readHosts(t, content), AppliedState{Known: true, Written: written}, 5)
	before, after := stateFor(t, bare, "gh-main"), stateFor(t, states, "gh-main")
	if before.Status != after.Status || before.Message != after.Message {
		t.Errorf("挂定格改掉了判定：前 %s/%q，后 %s/%q", before.Status, before.Message, after.Status, after.Message)
	}

	// 池没接或读失败（空索引）时原样不动：那一格留空是「不知道」，不是「不通」。
	AttachEntryProbes(bare, nil)
	if !reflect.DeepEqual(bare, HostsStates(c, readHosts(t, content), AppliedState{Known: true, Written: written}, 5)) {
		t.Errorf("没有池却改了逐条事实：%+v", stateFor(t, bare, "gh-main").Entries)
	}
}

// 真链路：一轮检测落盘之后，只读池的界面进程（模拟刚重启、内存里那一轮已经没了）
// 照样要把每条地址的耗时和定格时间说回来；没有地址的那几条一个字都不许多。
func TestGetItemsFillsEntryProbesFromPoolAfterRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "detect-history.db"))
	if err != nil {
		t.Fatalf("候选底账打不开：%v", err)
	}
	defer db.Close()
	finished := time.Now().UTC().Truncate(time.Second)
	round := &accelerator.Result{
		ItemID:     "gh-main",
		StartedAt:  finished.Add(-2 * time.Second),
		FinishedAt: finished,
		Available:  1,
		ProbesUsed: 2,
		Candidates: []accelerator.Candidate{
			{ID: "c1", Hostname: "github.com", IP: "140.82.113.4", Status: accelerator.StatusAvailable, LatencyMS: 79, TLSMS: 120},
			{ID: "c2", Hostname: "github.com", IP: "203.0.113.9", Status: accelerator.StatusUnavailable, LatencyMS: 0, Reason: "证书对不上这个域名"},
		},
	}
	if err := db.SaveRound("round-1", "gh-main", "service", round); err != nil {
		t.Fatalf("一轮检测落盘失败：%v", err)
	}

	written, content := writtenState(t, "127.0.0.1 localhost\n", map[string][]hosts.Mapping{
		"gh-main": {{IP: "140.82.113.4", Hostnames: []string{"github.com"}}},
	}, "gh-main")
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写临时 Hosts 失败：%v", err)
	}
	s := NewService("github-net", "test", nil, nil)
	s.priv = &fakePriv{written: written}
	s.hostsPath = path
	s.candidateStore = db
	defer func() { s.hostsPath = HostsPath() }()

	snapshot, err := s.GetItems()
	if err != nil {
		t.Fatalf("GetItems 不该失败：%v", err)
	}
	entries := stateFor(t, snapshot.Hosts, "gh-main").Entries
	if len(entries) != 3 {
		t.Fatalf("gh-main 的逐条事实数 = %d，期望 3", len(entries))
	}
	main := entries[0]
	if main.Hostname != "github.com" || main.IP != "140.82.113.4" {
		t.Fatalf("第一条该是写上了地址的 github.com，实际 %+v", main)
	}
	if main.ProbeStatus != "available" || main.LatencyMS != 79 {
		t.Errorf("github.com 这一格的定格 = 状态 %q、耗时 %dms，期望 available、79ms", main.ProbeStatus, main.LatencyMS)
	}
	if main.MeasuredAt == "" {
		t.Error("定格时间漏了：界面没法说这句结论是什么时候测的")
	}
	for _, e := range entries[1:] {
		if e.IP != "" || e.ProbeStatus != "" || e.LatencyMS != 0 || e.MeasuredAt != "" {
			t.Errorf("没有地址的 %s 不该带任何定格，实际 %+v", e.Hostname, e)
		}
	}
}
