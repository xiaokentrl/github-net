package app

import (
	"context"
	"fmt"
	"log"
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
//
// 同一个浏览器在不同发行方式下进程名不一样，别只登记包装脚本名：
// Microsoft Edge 的 deb 包装叫 microsoft-edge，但真正在跑的进程叫 msedge（Linux 实测），
// 漏了后者就是「开关开着、Hosts 写成功了、浏览器却一台都没重启」。
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
	{"msedge", "Edge", true},
	{"opera", "Opera", true},
}

// flatpak 里进程 argv0 不是浏览器名，认的是「flatpak run <应用ID>」里的应用 ID 后缀。
var browserFlatpakIDs = []string{
	"org.mozilla.firefox", "org.chromium.chromium", "com.google.chrome",
	"com.brave.browser", "com.vivaldi.vivaldi", "com.microsoft.edge", "com.opera.opera",
}

// 名字里带这些词的一定不是浏览器主进程。前半是浏览器自带的辅助进程（崩溃收集、
// 沙箱占位、渲染子进程），后半是名字恰好撞上浏览器前缀的独立工具——
// 比如 chrome-devtools-mcp 以 chrome 开头，不挡住会对它发 SIGTERM 再按原命令行拉起。
var browserHelperRejects = []string{
	"crashpad", "sandbox", "helper", "renderer", "chromedriver", "chrome-headless",
	"devtools", "mcp", "remote-desktop",
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

// browserRestartReport 是「这一轮到底做了什么」的如实记录。以前只回一个 restarted 数，
// 0 家、信号发不出去、浏览器没退干净、拉起失败全都被压成 0，界面一律说「没发现浏览器」——
// 用户看到的就是「功能没效果」却查不出断在哪。每一步都记下来，回执才可能说实话。
type browserRestartReport struct {
	scanned      int // /proc 里认出的浏览器进程数
	launchers    int // 判成主进程的个数（要等退场、要拉起的那几个）
	signalFailed int // SIGTERM 发不出去的进程数
	notGone      int // 等不到温和退场的主进程数
	launchFailed int // 退干净了但按原命令行拉起失败的主进程数
	restarted    int
	families     []string
}

// 回执原因码：前端按它选文案，别再用一句「没发现正在运行的浏览器」盖住所有早退。
const (
	restartReasonOK              = "ok"
	restartReasonPartial         = "partial"
	restartReasonRoundNotSettled = "roundNotSettled"
	restartReasonNoWritten       = "noWritten"
	restartReasonDebounced       = "debounced"
	restartReasonNoneFound       = "noneFound"
	restartReasonNotGone         = "notGone"
	restartReasonLaunchFailed    = "launchFailed"
	restartReasonSignalFailed    = "signalFailed"
)

// RestartBrowsers 温和退出所有正在运行的浏览器并按原样拉起，返回这一轮的完整记录。
// 没在跑的浏览器绝不拉起；某家等不到退场就跳过这一家，绝不强杀。
func RestartBrowsers() browserRestartReport {
	return restartBrowsersReport(browserList())
}

// restartBrowsers 是原有口径的薄封装：只回「重启了几家、各叫什么」，测试与旧调用方沿用。
func restartBrowsers(procs []browserProc) (int, []string) {
	r := restartBrowsersReport(procs)
	return r.restarted, r.families
}

// restartBrowsersReport 是核心：同家族全体 SIGTERM → 逐个主进程等退场 → 按原命令行各拉一份。
// 主进程判定：父进程不属于同家族。同家族可以有多个主进程（不同 profile 的多实例），
// 每个实例各按各的命令行拉起；回执按家族去重，用户看到的还是「重启了哪几个浏览器」。
func restartBrowsersReport(procs []browserProc) browserRestartReport {
	r := browserRestartReport{scanned: len(procs)}
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
	r.launchers = len(launchers)
	log.Printf("[browser-restart] 扫到 %d 个浏览器进程、%d 个主进程", r.scanned, r.launchers)

	// 整家族一起收 SIGTERM：主进程们各自落盘退场，信号只发这一轮扫到的 pid。
	for _, ps := range byFamily {
		for _, p := range ps {
			if err := browserSignal(p.Pid); err != nil {
				r.signalFailed++
				log.Printf("[browser-restart] 给 pid %d (%s) 发 SIGTERM 失败：%v", p.Pid, p.Family, err)
			}
		}
	}

	familiesSeen := map[string]bool{}
	for _, l := range launchers {
		if !browserWaitGone(l.Pid, browserWaitTimeout) {
			r.notGone++
			log.Printf("[browser-restart] %s 主进程 pid %d 在 %s 内没退干净，不强杀、不拉起", l.Family, l.Pid, browserWaitTimeout)
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
			r.launchFailed++
			// 只记家族与可执行文件名，不记完整路径（含用户名的路径属于敏感清单）
			log.Printf("[browser-restart] %s 按 %s 拉起失败：%v", l.Family, filepath.Base(l.Exe), err)
			continue
		}
		r.restarted++
		if !familiesSeen[l.Family] {
			familiesSeen[l.Family] = true
			r.families = append(r.families, l.Family)
		}
	}
	log.Printf("[browser-restart] 结果：重启 %d 家 %v，信号失败 %d、没退干净 %d、拉起失败 %d",
		r.restarted, r.families, r.signalFailed, r.notGone, r.launchFailed)
	return r
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

// claimBrowserRestartSlot 占住去抖窗口，返回本次时间戳；窗口已被占用返回 false。
// 抽成方法是为了能单独钉死「一台没扫到也白占 30 秒」这个老问题。
func (s *Service) claimBrowserRestartSlot() (time.Time, bool) {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if time.Since(s.browserLastRestart) < browserRestartAfterApplyDebounce {
		return time.Time{}, false
	}
	stamp := time.Now()
	s.browserLastRestart = stamp
	return stamp, true
}

// releaseBrowserRestartSlot 归还没真重启成的那次占位，只认自己那枚时间戳，
// 别把并发轮次刚写进去的新时间戳一起清掉。
func (s *Service) releaseBrowserRestartSlot(stamp time.Time) {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	if s.browserLastRestart.Equal(stamp) {
		s.browserLastRestart = time.Time{}
	}
}

// browserRestartEnabled 读开关：配置层没接上（目录加载失败）一律当作关闭——
// 动用户正开着的浏览器必须拿到明确的授权，拿不到授权就不动。
func (s *Service) browserRestartEnabled() bool {
	if s.settings == nil {
		log.Printf("[browser-restart] 配置层没接上，按「开关关闭」处理")
		return false
	}
	cfg, err := s.settings.Snapshot()
	if err != nil {
		log.Printf("[browser-restart] 配置读不出来，按「开关关闭」处理：%v", err)
		return false
	}
	return cfg.Browser.RestartAfterApply
}

// emitBrowserRestart 发回执。以前只有「真跑完 RestartBrowsers」才发，任何一处早退
// 界面都毫无动静，用户只能得出「这功能没效果」。现在每一步都带原因码和计数。
func (s *Service) emitBrowserRestart(reason string, r browserRestartReport) {
	if s.emit == nil {
		return
	}
	s.emit(event.BrowserRestartedName, map[string]any{
		"restarted":    r.restarted,
		"families":     r.families,
		"reason":       reason,
		"scanned":      r.scanned,
		"launchers":    r.launchers,
		"signalFailed": r.signalFailed,
		"notGone":      r.notGone,
		"launchFailed": r.launchFailed,
	})
}

// restartBrowsersAfterApply 等这一轮应用落定：至少一条真写成功（StatusDone）才重启浏览器；
// 全跳过/全失败不动——没写进任何东西，浏览器也没必要重启。结果走事件总线给前端报回执。
func (s *Service) restartBrowsersAfterApply(roundID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := s.tasks.WaitRound(ctx, roundID); err != nil {
		log.Printf("[browser-restart] 轮次 %s 没落定，跳过重启：%v", roundID, err)
		s.emitBrowserRestart(restartReasonRoundNotSettled, browserRestartReport{})
		return
	}
	written := 0
	total := 0
	for _, t := range s.tasks.Snapshot().Tasks {
		if t.RoundID == roundID && t.Kind == task.KindApply {
			total++
			if t.Status == task.StatusDone {
				written++
			}
		}
	}
	if total == 0 {
		// 这轮一条任务都没有：双击第二次调用里所有项都被队列去重复用了已有行，
		// 没有属于本轮的任何任务——第一轮已经报过结论，这里再报 noWritten 就是噪音
		// （2026-11-13 按钮审计中危）。静默退出，不占重启窗口。
		log.Printf("[browser-restart] 轮次 %s 快照里没有本轮任务，静默跳过", roundID)
		return
	}
	if written == 0 {
		// 全跳过、全失败，或用户点了「清空已完成」把本轮终态记录删掉，都会走到这里。
		// 明说出来，别让人对着「功能没效果」猜。
		log.Printf("[browser-restart] 轮次 %s 快照里没有 StatusDone 的应用任务，跳过重启", roundID)
		s.emitBrowserRestart(restartReasonNoWritten, browserRestartReport{})
		return
	}
	// 去重去抖：另一轮 30 秒内刚重启过就让位，免得把刚拉起的又杀一遍。
	// 但只占「真重启成」的窗口——一台没扫到也白占 30 秒，用户重试全是「点了没反应」。
	stamp, ok := s.claimBrowserRestartSlot()
	if !ok {
		log.Printf("[browser-restart] 30 秒内刚重启过，本轮跳过")
		s.emitBrowserRestart(restartReasonDebounced, browserRestartReport{})
		return
	}

	r := RestartBrowsers()
	if r.restarted == 0 {
		s.releaseBrowserRestartSlot(stamp) // 没真重启成一家，窗口让出去，下一轮能立刻再试
	}

	reason := restartReasonOK
	switch {
	case r.restarted > 0 && (r.notGone > 0 || r.launchFailed > 0 || r.signalFailed > 0):
		reason = restartReasonPartial
	case r.scanned == 0 || r.launchers == 0:
		reason = restartReasonNoneFound
	case r.notGone > 0:
		reason = restartReasonNotGone
	case r.launchFailed > 0:
		reason = restartReasonLaunchFailed
	case r.signalFailed > 0:
		reason = restartReasonSignalFailed
	}
	s.emitBrowserRestart(reason, r)
}
