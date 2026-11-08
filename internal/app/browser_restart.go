package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 浏览器家族登记表：可执行名前缀 → 家族。名字只登记「进程叫什么」，不登记装在哪——
// apt/deb、snap 包装脚本、flatpak 各装各的，路径五花八门，但进程名就是这几个词。
// ChromeLike 的家族重启时补 --restore-last-session，让它自己恢复关闭前的窗口；
// Firefox 系靠它自己的「恢复上次会话」设置，程序不代设（设置里已写明这一条）。
type browserFamily struct {
	prefix     string
	label      string
	chromeLike bool
}

var browserFamilies = []browserFamily{
	{"firefox", "Firefox", false},
	{"zen", "Zen", false},
	{"librewolf", "LibreWolf", false},
	{"waterfox", "Waterfox", false},
	{"floorp", "Floorp", false},
	{"chromium", "Chromium", true},
	{"google-chrome", "Chrome", true},
	{"chrome", "Chrome", true},
	{"brave", "Brave", true},
	{"vivaldi", "Vivaldi", true},
	{"microsoft-edge", "Edge", true},
	{"opera", "Opera", true},
}

// flatpak 里进程 argv0 不是浏览器名，认的是「flatpak run <应用ID>」里的应用 ID 后缀。
var browserFlatpakIDs = []string{
	"org.mozilla.firefox", "org.chromium.chromium", "com.google.chrome",
	"com.brave.browser", "com.vivaldi.vivaldi", "com.microsoft.edge", "com.opera.opera",
}

// 浏览器进程扫出来的一行：Exe/Args 原样取自 /proc/<pid>/cmdline（NUL 分隔），
// Family 是匹配到的家族；ChromeLike 原样带出来，重启那一步用。
type browserProc struct {
	Pid        int
	Ppid       int
	Exe        string
	Args       []string
	Family     string
	ChromeLike bool
}

// 低层动作收成包级变量：测试换假件，不改产品代码路径。
var (
	browserList   = listBrowserProcs
	browserSignal = func(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }
	browserLaunch = func(exe string, args []string) error { return launchDetached(exe, args) }
	// 等进程退场：SIGTERM 之后浏览器要时间落盘会话，等不到就放弃这一家，
	// 绝不 SIGKILL——强杀等于放弃「还原关闭前窗口」这个目标本身。
	browserWaitGone = waitPidGone
)

// browserWaitTimeout 是一次「温和退出」的总等待。真在跑的浏览器 8 秒足够落盘退干净。
const browserWaitTimeout = 8 * time.Second

// 浏览器自带的辅助进程（崩溃收集、沙箱占位）会撞上家族前缀，明确排除——
// 它们的父进程正常时是浏览器主进程，但主进程先死时会变孤儿被误判成启动器。
var browserHelperRejects = []string{"crashpad", "sandbox", "helper", "renderer", "chromedriver", "chrome-headless"}

// familyOf 按可执行名前缀认家族；认不出再看 flatpak 的应用 ID。认不出返回空串。
func familyOf(exeBase string, args []string) (string, bool) {
	// Chromium 系子进程（gpu/utility/renderer）主进程从不带 --type=：
	// 主进程先死时它们会变孤儿，靠这一条把它们挡在「主进程」判定之外。
	for _, arg := range args {
		if strings.HasPrefix(arg, "--type=") {
			return "", false
		}
	}
	for _, reject := range browserHelperRejects {
		if strings.Contains(exeBase, reject) {
			return "", false
		}
	}
	for _, f := range browserFamilies {
		if strings.HasPrefix(exeBase, f.prefix) {
			return f.label, f.chromeLike
		}
	}
	// flatpak run 的 argv0 是 flatpak 本体：从参数里找已知应用 ID（大小写不敏感）。
	// 应用 ID 用点分隔（com.microsoft.edge）、家族前缀用连字符（microsoft-edge），
	// 两边都把点/连字符去掉再比对，一份前缀两边通用。
	if exeBase == "flatpak" {
		for _, arg := range args {
			lower := strings.ToLower(arg)
			for _, id := range browserFlatpakIDs {
				if !strings.Contains(lower, id) {
					continue
				}
				normalized := strings.NewReplacer(".", "", "-", "", "_", "").Replace(lower)
				for _, f := range browserFamilies {
					if strings.Contains(normalized, strings.ReplaceAll(f.prefix, "-", "")) {
						return f.label, f.chromeLike
					}
				}
			}
		}
	}
	return "", false
}

// listBrowserProcs 扫 /proc：cmdline 非空、能认出家族的才算。
// 辅助进程（crashpad、renderer）和主进程是同一个可执行名，靠「主进程判定」在下一步分开。
func listBrowserProcs() []browserProc {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	out := make([]browserProc, 0, 8)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue // 不是数字目录的不是进程
		}
		args, err := procCmdline(pid)
		if err != nil || len(args) == 0 {
			continue // 内核线程和刚退出的进程没有 cmdline
		}
		base := strings.ToLower(filepath.Base(args[0]))
		family, chromeLike := familyOf(base, args)
		if family == "" {
			continue
		}
		ppid, err := procPpid(pid)
		if err != nil {
			ppid = 0
		}
		out = append(out, browserProc{Pid: pid, Ppid: ppid, Exe: args[0], Args: args, Family: family, ChromeLike: chromeLike})
	}
	return out
}

// procCmdline 读 /proc/<pid>/cmdline 并按 NUL 切开；文件在进程退出后会读不到。
func procCmdline(pid int) ([]string, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// procPpid 读 /proc/<pid>/status 里的 PPid 行（stat 的第 4 字段会被进程名里的括号搅乱，这里走 status 稳）。
func procPpid(pid int) (int, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "PPid:") {
			return strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
		}
	}
	return 0, fmt.Errorf("/proc/%d/status 里没有 PPid", pid)
}

// RestartBrowsers 温和退出所有正在运行的浏览器并按原样拉起，返回重启了几家、各叫什么。
// 没在跑的浏览器绝不拉起；某家等不到退场就跳过这一家，绝不强杀。
func RestartBrowsers() (int, []string) {
	return restartBrowsers(browserList())
}

// restartBrowsers 是核心：同家族全体 SIGTERM → 逐个主进程等退场 → 按原命令行各拉一份。
// 主进程判定：父进程不属于同家族。同家族可以有多个主进程（不同 profile 的多实例），
// 每个实例各按各的命令行拉起；回执按家族去重，用户看到的还是「重启了哪几个浏览器」。
func restartBrowsers(procs []browserProc) (int, []string) {
	byFamily := map[string][]browserProc{}
	for _, p := range procs {
		byFamily[p.Family] = append(byFamily[p.Family], p)
	}
	// 主进程：父进程不属于同家族的那些。snap/flatpak 的包装也在这一层。
	var launchers []browserProc
	for _, ps := range byFamily {
		famPids := map[int]bool{}
		for _, p := range ps {
			famPids[p.Pid] = true
		}
		for _, p := range ps {
			if famPids[p.Ppid] {
				continue // 父进程也是同家族：自己是子进程
			}
			launchers = append(launchers, p)
		}
	}
	// 按 pid 排序：多实例时先拉早起的那个，顺序稳定可复现。
	sort.Slice(launchers, func(i, j int) bool { return launchers[i].Pid < launchers[j].Pid })

	// 整家族一起收 SIGTERM：主进程们各自落盘退场，信号只发这一轮扫到的 pid。
	for family, ps := range byFamily {
		_ = family
		for _, p := range ps {
			_ = browserSignal(p.Pid)
		}
	}

	restarted := 0
	familiesSeen := map[string]bool{}
	var families []string
	for _, l := range launchers {
		if !browserWaitGone(l.Pid, browserWaitTimeout) {
			continue // 没退干净：不拉起、不强杀，用户自己关了再开
		}
		args := append([]string{}, l.Args[1:]...)
		if l.ChromeLike {
			hasRestore := false
			for _, a := range args {
				if a == "--restore-last-session" {
					hasRestore = true
					break
				}
			}
			if !hasRestore {
				args = append(args, "--restore-last-session")
			}
		}
		if err := browserLaunch(l.Exe, args); err != nil {
			continue
		}
		restarted++
		if !familiesSeen[l.Family] {
			familiesSeen[l.Family] = true
			families = append(families, l.Family)
		}
	}
	return restarted, families
}

// waitPidGone 轮询直到进程退场或超时。zombie（State: Z）不算活着——
// 本应用自己拉起的浏览器若没人收尸，/proc 目录会挂着，按活着等就死等了。
func waitPidGone(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !pidAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// pidAlive 读 /proc/<pid>/status 的 State 行：读不到＝已退；Z（zombie）＝已退等收尸。
func pidAlive(pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "State:") {
			fields := strings.Fields(line)
			return len(fields) >= 2 && fields[1] != "Z"
		}
	}
	return false
}

// launchDetached 按原命令行把浏览器拉起来：新会话脱离本进程，输出全部丢弃——
// 浏览器是自己活自己的，应用退出不能带走它。Start 出来的孩子由 goroutine 收尸，
// 不然 zombie 的 /proc 目录挂着，下一轮 waitPidGone 会被它骗住。
func launchDetached(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Dir = userHomeOrEmpty()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// userHomeOrEmpty 拿用户主目录当浏览器的工作目录；拿不到就用空串（进程继承当前目录）。
func userHomeOrEmpty() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// browserRestartAfterApplyDebounce 是两次浏览器重启之间的最小间隔：
// 并发两轮应用各自落定时，后到的看到刚重启过就不再动，免得把刚拉起的又杀一遍。
const browserRestartAfterApplyDebounce = 30 * time.Second

// browserRestartEnabled 读开关：配置层没接上（目录加载失败）一律当作关闭——
// 动用户正开着的浏览器必须拿到明确的授权，拿不到授权就不动。
func (s *Service) browserRestartEnabled() bool {
	if s.settings == nil {
		return false
	}
	cfg, err := s.settings.Snapshot()
	if err != nil {
		return false
	}
	return cfg.Browser.RestartAfterApply
}

// restartBrowsersAfterApply 等这一轮应用落定：至少一条真写成功（StatusDone）才重启浏览器；
// 全跳过/全失败不动——没写进任何东西，浏览器也没必要重启。结果走事件总线给前端报回执。
func (s *Service) restartBrowsersAfterApply(roundID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := s.tasks.WaitRound(ctx, roundID); err != nil {
		return // 队列停了或超时：这一轮没落定，什么都不动
	}
	written := 0
	for _, t := range s.tasks.Snapshot().Tasks {
		if t.RoundID == roundID && t.Kind == task.KindApply && t.Status == task.StatusDone {
			written++
		}
	}
	if written == 0 {
		return
	}
	// 去重去抖的临界区：另一轮 30 秒内刚重启过（或正重启）就让位，
	// 不然第二轮会把第一轮刚拉起的浏览器再杀一遍再拉一遍。
	s.browserMu.Lock()
	if time.Since(s.browserLastRestart) < browserRestartAfterApplyDebounce {
		s.browserMu.Unlock()
		return
	}
	s.browserLastRestart = time.Now()
	s.browserMu.Unlock()

	restarted, families := RestartBrowsers()
	if s.emit != nil {
		s.emit(event.BrowserRestartedName, map[string]any{"restarted": restarted, "families": families})
	}
}
