package app

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// 假件：把信号、等待、拉起全收进记录，产品代码路径一行不改。
type fakeBrowserEnv struct {
	signals   []int
	launched  [][2]interface{} // exe + args
	gonePids  map[int]bool     // 这些 pid 在等待时假装已经退场
	waitCalls int
}

func (f *fakeBrowserEnv) install(t *testing.T) {
	t.Helper()
	origSignal, origLaunch, origWait := browserSignal, browserLaunch, browserWaitGone
	t.Cleanup(func() { browserSignal, browserLaunch, browserWaitGone = origSignal, origLaunch, origWait })
	browserSignal = func(pid int) error { f.signals = append(f.signals, pid); return nil }
	browserLaunch = func(exe string, args []string) error {
		f.launched = append(f.launched, [2]interface{}{exe, args})
		return nil
	}
	browserWaitGone = func(pid int, timeout time.Duration) bool {
		f.waitCalls++
		return f.gonePids[pid]
	}
}

func TestRestartBrowsersChromeRestoresWithFlag(t *testing.T) {
	f := &fakeBrowserEnv{gonePids: map[int]bool{100: true, 101: true, 102: true}}
	f.install(t)
	procs := []browserProc{
		{Pid: 100, Ppid: 1, Exe: "/usr/bin/google-chrome-stable", Args: []string{"/usr/bin/google-chrome-stable", "--profile-directory=Default"}, Family: "Chrome", ChromeLike: true},
		{Pid: 101, Ppid: 100, Exe: "/usr/bin/google-chrome-stable", Args: []string{"/usr/bin/google-chrome-stable"}, Family: "Chrome", ChromeLike: true},
		{Pid: 102, Ppid: 100, Exe: "/usr/bin/google-chrome-stable", Args: []string{"/usr/bin/google-chrome-stable"}, Family: "Chrome", ChromeLike: true},
	}
	restarted, families := restartBrowsers(procs)
	if restarted != 1 || len(families) != 1 || families[0] != "Chrome" {
		t.Fatalf("重启家数 = %d %v，要的是 1 [Chrome]", restarted, families)
	}
	if len(f.launched) != 1 {
		t.Fatalf("拉起 %d 次，要的是 1 次（同家族只拉一份）", len(f.launched))
	}
	args := f.launched[0][1].([]string)
	if !slices.Contains(args, "--restore-last-session") {
		t.Fatalf("Chrome 系重启要补 --restore-last-session，实际参数 %v", args)
	}
	if !slices.Contains(args, "--profile-directory=Default") {
		t.Fatalf("用户原参数丢了：%v", args)
	}
	// 信号要打到全家族三个进程；主进程判定：子进程 101/102 不是拉起对象
	if len(f.signals) != 3 {
		t.Fatalf("要给同家族 3 个进程发信号，实际 %v", f.signals)
	}
}

func TestRestartBrowsersFirefoxNoFlag(t *testing.T) {
	f := &fakeBrowserEnv{gonePids: map[int]bool{200: true}}
	f.install(t)
	procs := []browserProc{
		{Pid: 200, Ppid: 1, Exe: "/usr/lib/firefox/firefox", Args: []string{"/usr/bin/firefox"}, Family: "Firefox", ChromeLike: false},
	}
	restarted, _ := restartBrowsers(procs)
	if restarted != 1 {
		t.Fatalf("Firefox 应重启 1 家，实际 %d", restarted)
	}
	args := f.launched[0][1].([]string)
	if slices.Contains(args, "--restore-last-session") {
		t.Fatalf("Firefox 靠自己的会话恢复设置，不该补 Chrome 的参数：%v", args)
	}
}

func TestRestartBrowsersFlatpakWrapped(t *testing.T) {
	f := &fakeBrowserEnv{gonePids: map[int]bool{300: true}}
	f.install(t)
	// flatpak 的 argv0 是 flatpak 本体，应用 ID 藏在参数里
	procs := []browserProc{
		{Pid: 300, Ppid: 1, Exe: "/usr/bin/flatpak", Args: []string{"/usr/bin/flatpak", "run", "--branch=stable", "com.google.Chrome"}, Family: "Chrome", ChromeLike: true},
	}
	restarted, families := restartBrowsers(procs)
	if restarted != 1 || families[0] != "Chrome" {
		t.Fatalf("flatpak 包装要认出 Chrome 家族，实际 %d %v", restarted, families)
	}
	exe := f.launched[0][0].(string)
	if exe != "/usr/bin/flatpak" {
		t.Fatalf("要按原命令行拉起 flatpak，实际拉起 %s", exe)
	}
}

func TestRestartBrowsersSkipsWhenNotGone(t *testing.T) {
	f := &fakeBrowserEnv{gonePids: map[int]bool{}}
	f.install(t)
	procs := []browserProc{
		{Pid: 400, Ppid: 1, Exe: "/usr/bin/chromium", Args: []string{"/usr/bin/chromium"}, Family: "Chromium", ChromeLike: true},
	}
	restarted, families := restartBrowsers(procs)
	if restarted != 0 || len(families) != 0 {
		t.Fatalf("没退干净的家族不该拉起，实际 %d %v", restarted, families)
	}
	if len(f.launched) != 0 {
		t.Fatalf("没退干净还拉起了：%v", f.launched)
	}
}

func TestRestartBrowsersMultipleFamilies(t *testing.T) {
	f := &fakeBrowserEnv{gonePids: map[int]bool{500: true, 600: true}}
	f.install(t)
	procs := []browserProc{
		{Pid: 500, Ppid: 1, Exe: "/usr/bin/firefox", Args: []string{"/usr/bin/firefox"}, Family: "Firefox", ChromeLike: false},
		{Pid: 600, Ppid: 1, Exe: "/usr/bin/vivaldi-stable", Args: []string{"/usr/bin/vivaldi-stable"}, Family: "Vivaldi", ChromeLike: true},
	}
	restarted, families := restartBrowsers(procs)
	if restarted != 2 || len(families) != 2 {
		t.Fatalf("两家各重启一份，实际 %d %v", restarted, families)
	}
}

func TestFamilyOfByExeAndFlatpakID(t *testing.T) {
	cases := []struct {
		exeBase string
		args    []string
		want    string
	}{
		{"firefox", nil, "Firefox"},
		{"firefox-esr", nil, "Firefox"},
		{"google-chrome-stable", nil, "Chrome"},
		{"microsoft-edge", nil, "Edge"},
		// Linux deb 版 Edge 真正在跑的进程叫 msedge；漏了它界面宣称支持 Edge 却一台都扫不到。
		{"msedge", nil, "Edge"},
		{"msedge-stable", nil, "Edge"},
		{"brave-browser", nil, "Brave"},
		{"flatpak", []string{"flatpak", "run", "org.mozilla.firefox"}, "Firefox"},
		{"flatpak", []string{"flatpak", "run", "com.microsoft.Edge"}, "Edge"},
		{"gedit", nil, ""},                                    // 非浏览器
		{"chrome_crashpad", nil, ""},                          // 崩溃收集器
		{"msedge_crashpad_handler", nil, ""},                  // Edge 的崩溃收集器
		{"chrome-devtools-mcp", nil, ""},                      // 名字撞 chrome 前缀的独立工具，绝不是浏览器
		{"chrome-remote-desktop", nil, ""},                    // 同上：误发 SIGTERM 会杀掉别的程序
		{"msedge", []string{"msedge", "--type=renderer"}, ""}, // 子进程不算主进程候选
	}
	for _, c := range cases {
		got, _ := familyOf(c.exeBase, c.args)
		if got != c.want {
			t.Errorf("familyOf(%q, %v) = %q，要 %q", c.exeBase, c.args, got, c.want)
		}
	}
}

// Edge 的 ChromeLike 不能丢：重启时靠它补 --restore-last-session 还原窗口。
func TestFamilyOfEdgeIsChromeLike(t *testing.T) {
	for _, base := range []string{"msedge", "msedge-stable", "microsoft-edge"} {
		label, chromeLike := familyOf(base, nil)
		if label != "Edge" || !chromeLike {
			t.Errorf("familyOf(%q) = (%q, %v)，要 (\"Edge\", true)", base, label, chromeLike)
		}
	}
}

// 真实 /proc 形状：Edge 主进程 + zygote/renderer + 各路 crashpad，最后只该剩主进程当 launcher。
func TestListBrowserProcsOnlyKeepsEdgeMain(t *testing.T) {
	const main = "/opt/microsoft/msedge/msedge"
	// 直接走 familyOf 逐条过滤，跟 listBrowserProcs 同一套判据。
	cmdlines := [][]string{
		{main},
		{main, "--type=zygote", "--no-zygote-sandbox"},
		{main, "--type=renderer", "--lang=zh-CN"},
		{"/opt/microsoft/msedge/msedge_crashpad_handler"},
		{"/home/kentrl/node_modules/.bin/chrome-devtools-mcp"},
	}
	procs := make([]browserProc, 0, 1)
	for _, args := range cmdlines {
		label, chromeLike := familyOf(strings.ToLower(filepath.Base(args[0])), args)
		if label != "" {
			procs = append(procs, browserProc{Exe: args[0], Args: args, Family: label, ChromeLike: chromeLike})
		}
	}
	if len(procs) != 1 || procs[0].Family != "Edge" {
		t.Fatalf("只该认出 Edge 主进程，实际 %d 条：%+v", len(procs), procs)
	}
}

// 信号发不出去、没退干净、拉起失败，都必须在回执里分得清，不能再压成 restarted=0。
func TestRestartBrowsersReportsFailureStages(t *testing.T) {
	f := &fakeBrowserEnv{gonePids: map[int]bool{700: true}}
	f.install(t)
	browserSignal = func(pid int) error {
		f.signals = append(f.signals, pid)
		if pid == 701 {
			return errors.New("EPERM")
		}
		return nil
	}
	browserLaunch = func(_ string, _ []string) error {
		f.launched = append(f.launched, [2]interface{}{"x", nil})
		return errors.New("拉不起来")
	}
	procs := []browserProc{
		{Pid: 700, Ppid: 1, Exe: "/opt/microsoft/msedge/msedge", Args: []string{"/opt/microsoft/msedge/msedge"}, Family: "Edge", ChromeLike: true},
		{Pid: 701, Ppid: 700, Exe: "/opt/microsoft/msedge/msedge", Args: []string{"/opt/microsoft/msedge/msedge"}, Family: "Edge", ChromeLike: true},
	}
	r := restartBrowsersReport(procs)
	if r.scanned != 2 || r.launchers != 1 {
		t.Errorf("扫到 %d 个、主进程 %d 个，要 2/1", r.scanned, r.launchers)
	}
	if r.signalFailed != 1 {
		t.Errorf("信号失败 %d，要 1", r.signalFailed)
	}
	if r.launchFailed != 1 {
		t.Errorf("拉起失败 %d，要 1", r.launchFailed)
	}
	if r.restarted != 0 {
		t.Errorf("这种情况下没重启成任何一家，实际 %d", r.restarted)
	}
}

// 一台没扫到（或没重启成）不再白占 30 秒窗口：归还后下一轮能立刻占上。
func TestBrowserRestartSlotReleasedOnNoRestart(t *testing.T) {
	s := &Service{}
	stamp, ok := s.claimBrowserRestartSlot()
	if !ok {
		t.Fatal("第一次应该能占上窗口")
	}
	if _, ok2 := s.claimBrowserRestartSlot(); ok2 {
		t.Fatal("窗口占住期间不该让第二轮进来")
	}
	s.releaseBrowserRestartSlot(stamp)
	if _, ok3 := s.claimBrowserRestartSlot(); !ok3 {
		t.Fatal("没重启成一家后窗口必须归还，否则用户 30 秒内重试全是「点了没反应」")
	}
}
