package app

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/detector"
	"github.com/xiaokentrl/github-net/internal/hosts"
	"github.com/xiaokentrl/github-net/internal/privilege"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 应用与还原的入口层。分工和检测那条一样：入口验形状（项目在目录、有检测结果、
// 服务可达与否不在这验——队列任务里会验并给出可见失败），执行器验内容并干活。
// 这一批命令都不自己写 Hosts：写系统的只有特权辅助服务（internal/privilege）。

// ApplyItems 把指定项目排进应用队列：把该项目当前挑中的候选地址写进系统 Hosts。
// 写盘前有一道复验：结论超过 applyFreshWindow 的那些先现场探一遍，验通的才写，
// 一个都没验通就一个字不写、任务落失败（拍板 2026-11-06：永远不把不能正常加速的地址写进 Hosts）。
// 勾了几项就排几行——做不了的那几项也进同一轮，以「跳过」落定并写明原因，
// 不从这一轮里消失（用户勾 6 项、队列只剩 1 行、剩下 5 行去哪了，比报错更难查）。
// 整批校验已经换成逐项判定，但项目在不在目录里、清单空不空仍然整批拒。
func (s *Service) ApplyItems(itemIDs []string) ([]task.Task, error) {
	ids, err := s.uniqueKnownItems(itemIDs)
	if err != nil {
		return nil, err
	}
	roundID := s.tasks.BeginRound(task.KindApply, s.batchScope(ids), applyRoundName(len(ids)))
	tasks := make([]task.Task, 0, len(ids))
	for _, id := range ids {
		name, scope, _ := s.findItem(id)
		req := task.NewRequest{Kind: task.KindApply, ItemID: id, Name: name, ScopeID: scope, RoundID: roundID}
		if reason := s.applyBlocker(id); reason != "" {
			t, err := s.tasks.EnqueueSkipped(req, reason)
			if err != nil {
				return nil, ToPublicError(err)
			}
			tasks = append(tasks, t)
			continue
		}
		t, err := s.tasks.Enqueue(req)
		if err != nil {
			return nil, ToPublicError(err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// applyBlocker 说清这个项目这会儿为什么还不能应用；能应用返回空串。
// 返回的句子直接进任务并显示到队列明细，所以写的是用户照着能做的下一步，不是内部原因。
func (s *Service) applyBlocker(itemID string) string {
	// 内存和候选池里有一样候选的地址就放行（拍板 2026-11-03）：重启过后内存是空的，
	// 池里缓存的可用地址照样能进入候选——以前只看内存，重启一次就逼用户重新检测。
	// 注意这里放行的只是「有候选」：结论放久了的那些要等任务开始执行时现场复验通过才真写盘，
	// 复验不过就一个地址都不写（拍板 2026-11-06），所以点这颗按钮本身不联网。
	if len(s.usablePicksFor(itemID)) > 0 {
		return ""
	}
	// 拦下了也要分清两种说法，下一步动作不一样：有检测结果但全不可用 → 重新检测；
	// 连检测结果都没有（含重启后内存已空、缓存也没货）→ 先检测。
	if _, ok := s.detectResults.view(itemID); ok {
		return "最近一轮检测没有可用的候选地址，先重新检测"
	}
	return "这个项目还没有检测结果，先检测再应用"
}

// applyFreshWindow 是「这份可用结论还算新鲜、点应用不用现场重测」的期限（拍板 2026-11-06）。
// 定 10 分钟：正常用法是检测完顺手点应用，那一份必然是新鲜的、一次网络都不发；
// 再放久就只是在替一个可能已经被换掉的地址作保。
// 这条和缓存保留期是两件事（那个只有 retention.MaxAge 一个数字），别互相引用。
const applyFreshWindow = 10 * time.Minute

// usablePick 是一个域名当前挑中的那个地址，外加这条结论的两件事实。
type usablePick struct {
	hostname string
	ip       string
	latency  int64
	ipv6     bool
	// fresh 为真：这份 available 结论是 applyFreshWindow 内测出来的，可以直接写。
	// 为假：必须让应用执行器现场复验通过才准写——旧结论只是候选，不是通行证。
	fresh bool
	// fromPool 为真：这条来自落盘的候选池，复验结论要刷回池里那一行；
	// 为假：来自内存那一轮，只刷内存。
	fromPool bool
}

// usablePicksFor 给一个项目列出当前候选的地址（时效按现在算）。
func (s *Service) usablePicksFor(itemID string) []usablePick {
	return s.usablePicksAt(itemID, time.Now())
}

// usablePicksAt 给一个项目挑「现在候选能写」的域名→地址（拍板 2026-11-03 定来源，2026-11-06 补时效）：
// 每个域名取它当前已知最优的可用地址——内存里最近一轮优先（最新鲜），
// 内存没覆盖的域名用候选池里跨源累积的缓存补齐。切源随便切，应用合并全部已知。
// 选址规则和 bestMappings 同一条：延迟最低、并列 IPv4 优先。一个域名都没有给空。
// 每条再按「结论距今是否超过 applyFreshWindow」标 fresh；时间读不懂一律当不新鲜，
// 宁可多复验一次，也不拿一个说不清多旧的地址直接写盘。
func (s *Service) usablePicksAt(itemID string, now time.Time) []usablePick {
	best := make(map[string]usablePick)
	consider := func(p usablePick) {
		current, ok := best[p.hostname]
		better := !ok || p.latency < current.latency || (p.latency == current.latency && current.ipv6 && !p.ipv6)
		if better {
			best[p.hostname] = p
		}
	}
	// 内存里最近一轮：最新鲜的一份，整轮共用它的定格时间。
	roundFinished := s.detectResults.roundFinishedAt(itemID)
	roundFresh := !roundFinished.IsZero() && now.Sub(roundFinished.UTC()) <= applyFreshWindow
	if view, ok := s.detectResults.view(itemID); ok {
		for _, h := range view.Hosts {
			for _, c := range h.Candidates {
				if c.Status == "available" {
					consider(usablePick{hostname: c.Hostname, ip: c.IP, latency: c.LatencyMS, ipv6: c.IPv6, fresh: roundFresh})
				}
			}
		}
	}
	// 池补齐：只补内存没覆盖的域名（覆盖了的以内存为准，它更新鲜）。
	_, _, item, ok := s.lookupItem(itemID)
	if !ok || len(item.Hostnames) == 0 || s.candidateStore == nil {
		return picksOf(best)
	}
	want := make(map[string]bool, len(item.Hostnames))
	for _, h := range item.Hostnames {
		if n := hosts.NormalizeHostname(h); n != "" {
			want[n] = true
		}
	}
	rows, err := s.candidateStore.Pool("")
	if err != nil {
		return picksOf(best)
	}
	for _, r := range rows {
		if r.Status != "available" || !want[r.Hostname] {
			continue
		}
		if _, covered := best[r.Hostname]; covered {
			continue
		}
		consider(usablePick{
			hostname: r.Hostname, ip: r.IP, latency: r.LatencyMS, ipv6: r.IPv6,
			fresh: withinFreshWindow(r.UpdatedAt, now), fromPool: true,
		})
	}
	return picksOf(best)
}

// withinFreshWindow 判一条落盘时间戳是否还在复验豁免窗口内。读不懂（空串、格式不对、
// 时间在未来得离谱）一律按不新鲜：这种行只能现场复验，不能免检写盘。
func withinFreshWindow(stamp string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(stamp))
	if err != nil {
		return false
	}
	return now.Sub(t.UTC()) <= applyFreshWindow
}

// picksOf 把「每个域名一个候选地址」摊成稳定顺序的切片（按域名排），
// 执行器按它逐条复验、再把验通的收成写盘映射。
func picksOf(best map[string]usablePick) []usablePick {
	if len(best) == 0 {
		return nil
	}
	names := make([]string, 0, len(best))
	for hostname := range best {
		names = append(names, hostname)
	}
	sort.Strings(names)
	out := make([]usablePick, 0, len(names))
	for _, hostname := range names {
		out = append(out, best[hostname])
	}
	return out
}

// mappingsOf 把选中的域名→地址按 IP 分组收成写盘映射：同一个地址顶着多个域名时合成一行。
// 域名和 IP 各自按字典序排，写出去的内容对同一批输入是稳定的。
func mappingsOf(picks []usablePick) []hosts.Mapping {
	if len(picks) == 0 {
		return nil
	}
	byIP := make(map[string][]string)
	for _, p := range picks {
		byIP[p.ip] = append(byIP[p.ip], p.hostname)
	}
	ips := make([]string, 0, len(byIP))
	for ip := range byIP {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	out := make([]hosts.Mapping, 0, len(ips))
	for _, ip := range ips {
		names := byIP[ip]
		sort.Strings(names)
		out = append(out, hosts.Mapping{IP: ip, Hostnames: names})
	}
	return out
}

// recordProbeResult 把一次现场复验的结论回写到它来的地方（拍板 2026-11-06：通与不通都回写）。
// 内存那一轮里那条候选的状态、延迟、定格时间一起刷；来自池的那行同步刷状态和测量时间，
// 来源与归属不动。这样界面说的「可用」和下一次应用挑的东西，都是刚验过的事实，
// 不会反复拿一个已经废掉的地址去试。
// 刷盘失败不回报：少刷一行缓存不影响这次写没写对，把错误抛回队列只会盖掉真正的结论。
func (s *Service) recordProbeResult(itemID string, pick usablePick, status string, reason string, latencyMS int64, at time.Time) {
	if s.detectResults != nil {
		s.detectResults.markProbe(itemID, pick.hostname, pick.ip, accelerator.CandidateStatus(status), latencyMS, reason, at)
	}
	if !pick.fromPool || s.candidateStore == nil {
		return
	}
	_ = s.candidateStore.MarkPoolProbe(hosts.NormalizeHostname(pick.hostname), pick.ip, status, latencyMS, at)
}

// verifyCandidates 是应用写盘前那道复验（拍板 2026-11-06）：
// 窗口内测出来的结论直接放过，一次网络都不发；更早的每条现场探一遍，验通的才准写。
// 通与不通都当场回写来源，所以同一件事不会为同一条废地址复验第二次。
// 各条并行探，共用检测那一道全局闸——应用这条路不会把同时活动的外部请求数翻倍。
func (s *Service) verifyCandidates(ctx context.Context, itemID string, picks []usablePick) []usablePick {
	passed := make([]bool, len(picks))
	var wg sync.WaitGroup
	for i, p := range picks {
		if p.fresh {
			passed[i] = true
			continue
		}
		wg.Add(1)
		go func(i int, p usablePick) {
			defer wg.Done()
			passed[i] = s.verifyOne(ctx, itemID, p)
		}(i, p)
	}
	wg.Wait()
	out := make([]usablePick, 0, len(picks))
	for i, p := range picks {
		if passed[i] {
			out = append(out, p)
		}
	}
	return out
}

// verifyOne 现场探一条候选地址，把结论回写，返回这条现在能不能写进 Hosts。
// 判据和检测那轮是同一条（accelerator.Verdict：连上并且证书对这个域名有效），
// 这里不重写第二套标准。探测目标不合法（地址不是 IP 字面量）按不通处理：
// 那种行本来就不该出现在候选里，宁可少写也不猜。
func (s *Service) verifyOne(ctx context.Context, itemID string, p usablePick) bool {
	if s.netGate != nil {
		select {
		case s.netGate <- struct{}{}:
			defer func() { <-s.netGate }()
		case <-ctx.Done():
			return false
		}
	}
	probe := s.applyProbe
	if probe == nil {
		probe = detector.ProbeHTTPS
	}
	res, err := probe(ctx, detector.Target{IP: p.ip, Hostname: p.hostname})
	status, reason := accelerator.StatusUntested, "探测没有给出结论，这一条没有复验"
	if err == nil {
		status, reason = accelerator.Verdict(res)
	}
	s.recordProbeResult(itemID, p, string(status), reason, res.LatencyMS, time.Now())
	return status == accelerator.StatusAvailable
}

// RestoreItems 把指定项目排进还原队列：退回它应用前的映射。
// 项目在不在目录里要验；它有没有被应用过不在这验——还原没应用过的项目在服务侧是无害的空操作，
// 让队列里那条以「还原完成」收场即可，不给用户造一个必须先理解的错误。
func (s *Service) RestoreItems(itemIDs []string) ([]task.Task, error) {
	ids, err := s.uniqueKnownItems(itemIDs)
	if err != nil {
		return nil, err
	}
	singleName := ""
	if len(ids) == 1 {
		singleName, _, _ = s.findItem(ids[0])
	}
	return s.enqueueAll(task.KindRestore, ids, restoreRoundName(singleName, len(ids)))
}

// RestoreAllHosts 把全部还由工具管理的项目一次退回应用前状态，返回实际还原的项目数。
// 这是服务侧一笔原子操作、没有进度可言，所以不进队列：单独一条命令直接完成，
// 失败原样上抛给界面（错误码区分「服务没在跑」和「服务拒了」）。
func (s *Service) RestoreAllHosts() (int, error) {
	if s.priv == nil {
		return 0, NewError(CodeHelperUnavailable, "特权辅助服务没在运行，无法还原", nil)
	}
	n, err := s.priv.RestoreAll()
	if err != nil {
		return 0, privilegePublicError(err)
	}
	// 全部还原不进队列，没有执行器来重标「当前生效」。命令成功就说明系统 Hosts 上
	// 已经没有本工具写的映射，这里把内存里所有项目的标记清掉；界面随后重拉结果才不会
	// 继续把已经还原掉的 IP 显示成「当前写入」。失败时上面已经返回，现场一个字不动。
	if s.detectResults != nil {
		s.detectResults.clearApplied()
	}
	return n, nil
}

// HostsRestorePreview 是「清除已写入的 Hosts」预览里的前后对比，给前端展示用。
// 行里 Before 是当前生效的 IP，After 是还原后的 IP；After 为空表示这条映射将被移除（应用前本来没有）。
type HostsRestorePreview struct {
	Rows  []HostsRestoreRow `json:"rows"`
	Count int               `json:"count"`
	Items int               `json:"items"`
}

// HostsRestoreRow 是预览里的一行：某个当前由工具管理的域名，还原前后分别指向哪个 IP。
type HostsRestoreRow struct {
	Hostname string `json:"hostname"`
	Before   string `json:"before"`
	After    string `json:"after"`
}

// PreviewRestoreHosts 只读算「清除已写入的 Hosts」之后每个受管理域名的前后对比。
// 不写盘、不进队列；界面在用户勾选那一项时调用，让他先看清楚恢复前和恢复后的差别。
// 真正的还原仍是 RestoreAllHosts 那一份；这里只把结果翻译成前端要的形状。
func (s *Service) PreviewRestoreHosts() (HostsRestorePreview, error) {
	if s.priv == nil {
		return HostsRestorePreview{}, NewError(CodeHelperUnavailable, "特权辅助服务没在运行，无法预览还原", nil)
	}
	preview, err := s.priv.Preview()
	if err != nil {
		return HostsRestorePreview{}, privilegePublicError(err)
	}
	rows := make([]HostsRestoreRow, 0, len(preview.Rows))
	for _, r := range preview.Rows {
		rows = append(rows, HostsRestoreRow{Hostname: r.Hostname, Before: r.Before, After: r.After})
	}
	return HostsRestorePreview{Rows: rows, Count: preview.Count, Items: preview.Items}, nil
}

// OpenHosts 请系统用默认编辑器打开 Hosts 文件。打开动作由后端执行，
// 前端不碰进程；拿不到可展示失败，不静默。
func (s *Service) OpenHosts() error {
	path := s.hostsPath
	if path == "" {
		return NewError(CodeHostsFailed, "这个平台拿不到 Hosts 文件路径", nil)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		// start 是 cmd 的内建命令；路径是固定常量不是用户输入，没有注入面。
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		// Linux 桌面环境里 xdg-open 走默认关联；服务态没有会话，这命令只从界面进程发。
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		// 内部原因交给 Error() 追加一次，这里不再拼进消息：拼了界面上就是同一句两遍。
		return NewError(CodeHostsFailed, "没能打开 Hosts 文件", err)
	}
	// 不等编辑器退出：打开成功就算完成，进程交给系统。
	go func() { _ = cmd.Wait() }()
	return nil
}

// uniqueKnownItems 是 Apply/Restore 两个入口共用的第一道门：
// 去掉空白与重复、整批非空、每个 ID 都在目录里。任何一项不合格整批拒绝。
func (s *Service) uniqueKnownItems(itemIDs []string) ([]string, error) {
	if s.productCatalogErr != nil {
		return nil, ToPublicError(s.productCatalogErr)
	}
	ids := make([]string, 0, len(itemIDs))
	seen := make(map[string]bool, len(itemIDs))
	for _, raw := range itemIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, NewError(CodeInvalidItemID, "没有要操作的项目", nil)
	}
	for _, id := range ids {
		if _, _, ok := s.findItem(id); !ok {
			return nil, NewError(CodeInvalidItemID, "产品目录里没有这个项目，刷新界面后再试", nil)
		}
	}
	return ids, nil
}

// hasAvailable 报告一轮结果里有没有「可用」的候选。
func hasAvailable(view DetectResultView) bool {
	for _, h := range view.Hosts {
		for _, c := range h.Candidates {
			if c.Status == "available" {
				return true
			}
		}
	}
	return false
}

// enqueueAll 把一批已通过校验的项目按给定类型一起排进队列，归进同一个新的轮次，
// 返回任务快照。轮名由调用方按原型规则给（界面根节点显示它），范围只在整批同属一个范围时记下。
// 排到一半失败只剩「应用正在退出」，前面已排进去的有效任务会随队列关闭被取消。
func (s *Service) enqueueAll(kind task.Kind, ids []string, roundName string) ([]task.Task, error) {
	roundID := s.tasks.BeginRound(kind, s.batchScope(ids), roundName)
	tasks := make([]task.Task, 0, len(ids))
	for _, id := range ids {
		name, itemScope, _ := s.findItem(id)
		t, err := s.tasks.Enqueue(task.NewRequest{Kind: kind, ItemID: id, Name: name, ScopeID: itemScope, RoundID: roundID})
		if err != nil {
			return nil, ToPublicError(err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// batchScope 只在整批同属一个范围时记下那个范围；混了范围或项目不在目录里就给空串。
func (s *Service) batchScope(ids []string) string {
	scopeID := ""
	for _, id := range ids {
		_, sc, ok := s.findItem(id)
		if !ok {
			continue
		}
		if scopeID == "" {
			scopeID = sc
		} else if sc != scopeID {
			return ""
		}
	}
	return scopeID
}

// applyRoundName / restoreRoundName 是原型定下的两条轮名规则，写在一处免得界面自己拼第二套：
// 应用一批是「应用队列 · N 项」，恢复单个是「恢复 · 项目名」，恢复多个和恢复同类成一批「恢复队列 · N 项」。
func applyRoundName(count int) string {
	return "应用队列 · " + strconv.Itoa(count) + " 项"
}

func restoreRoundName(itemName string, count int) string {
	if count == 1 {
		return "恢复 · " + itemName
	}
	return "恢复队列 · " + strconv.Itoa(count) + " 项"
}

// privilegePublicError 把特权层失败翻成带稳定错误码的公开错误（只用于不走队列的直接命令）。
func privilegePublicError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case isHelperUnavailable(err):
		return NewError(CodeHelperUnavailable, "特权辅助服务没在运行，无法操作系统 Hosts；重新安装本应用可以把它带起来", err)
	case isHelperRefused(err):
		return NewError(CodeHostsFailed, "特权辅助服务拒绝了这次操作："+unwrapMessage(err), err)
	default:
		return NewError(CodeHostsFailed, "修改系统 Hosts 失败："+unwrapMessage(err), err)
	}
}

func isHelperUnavailable(err error) bool { return errors.Is(err, privilege.ErrHelperUnavailable) }
func isHelperRefused(err error) bool     { return errors.Is(err, privilege.ErrHelperRefused) }
