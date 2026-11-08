package app

import (
	"slices"
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
		{"brave-browser", nil, "Brave"},
		{"flatpak", []string{"flatpak", "run", "org.mozilla.firefox"}, "Firefox"},
		{"flatpak", []string{"flatpak", "run", "com.microsoft.Edge"}, "Edge"},
		{"gedit", nil, ""},           // 非浏览器
		{"chrome_crashpad", nil, ""}, // 崩溃收集器不是家族前缀命中（带下划线后缀不匹配 chrome 前缀？会匹配——见下）
	}
	for _, c := range cases {
		got, _ := familyOf(c.exeBase, c.args)
		if got != c.want {
			t.Errorf("familyOf(%q) = %q，要 %q", c.exeBase, got, c.want)
		}
	}
}
