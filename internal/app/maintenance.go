package app

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/detector"
	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/hosts"
)

// 维护循环（2026-11-15 拍板：巡检＋自动补位）：
//
//	每 maintenanceInterval 探一轮「已写入 Hosts 的 IP」——IP 是消耗品（对抗性封锁
//	有保质期，真机实测 57.155.x 检测时通、几小时后被掐），写完不管必然失效。
//	连续 maintenanceFailThreshold 轮探不通判失效：提醒用户；开了自动补位就从
//	候选池挑活 IP 改写 Hosts（走既有特权链＋复验＋双闸，不绕任何安全机制）。
//
// 关停：StopMaintenance（main.go OnShutdown 调）。
const (
	maintenanceInterval      = 15 * time.Minute
	maintenanceProbeTimeout  = 5 * time.Second
	maintenanceFailThreshold = 2
)

// startMaintenance 启动维护循环；maintStop 为 nil（测试直接构造的 Service）就不启动。
func startMaintenance(s *Service) {
	if s.maintStop == nil {
		return
	}
	done := make(chan struct{})
	s.maintDone = done
	go func() {
		defer close(done)
		// 间隔每轮现读配置（2026-11-15 拍板：巡检间隔可在设置里填，默认 8 小时）——
		// 用 time.After 而非固定 Ticker，改间隔下一轮立即生效，无需重启。
		for {
			interval := s.maintInterval
			if s.settings != nil {
				if cfg, err := s.settings.Snapshot(); err == nil && cfg.Maintenance.ProbeIntervalHours > 0 {
					interval = time.Duration(cfg.Maintenance.ProbeIntervalHours) * time.Hour
				}
			}
			select {
			case <-s.maintStop:
				return
			case <-time.After(interval):
				s.maintenanceCycle()
			}
		}
	}()
}

// StopMaintenance 停掉维护循环并等 goroutine 收尾（退出时调用）。
// 放包级不放方法：导出方法会被 Wails 当成绑定命令暴露给前端（「停维护」不该是
// 界面能点的），与 ShutdownTaskQueue 同一惯例。等待是为确保收尾期间不再有
// 补位写盘（对照复核 2026-11-16 低危：不等就无法确定已停）。
func StopMaintenance(s *Service) {
	if s.maintStop == nil {
		return
	}
	close(s.maintStop)
	if s.maintDone != nil {
		<-s.maintDone
	}
}

// maintenanceCycle 跑一轮巡检：探已写入的 IP → 连续失败判失效 → 提醒／自动补位。
func (s *Service) maintenanceCycle() {
	if s.priv == nil || s.settings == nil {
		return
	}
	cfg, err := s.settings.Snapshot()
	if err != nil || !cfg.Maintenance.AutoProbe {
		return
	}
	state, err := s.priv.State()
	if err != nil {
		log.Printf("[maintenance] 读 Hosts 状态失败，本轮跳过：%v", err)
		return
	}
	if !state.InUse {
		s.maintReset()
		return
	}

	// 逐条探测（同 IP 多主机名只探一次，结论共享——证书按主机名验，共享会有误差，
	// 但巡检只关心「这个 IP 还连不连得上」，连通层共享是安全的）。
	type deadEntry = hosts.Entry
	probed := make(map[string]bool)
	var dead []hosts.Entry
	for _, e := range state.Entries {
		if probed[e.IP] {
			continue
		}
		probed[e.IP] = true
		alive, counted := s.maintProbeIP(e.IP, e.Hostname)
		if !counted {
			// 等闸超时：本轮跳过该条，不计失败（复核 2026-11-16 低危）。
			continue
		}
		if alive {
			// 活了：清掉失败计数与提醒标记（IP 恢复是常态——封锁窗口会过去）。
			s.maintMu.Lock()
			for k := range s.maintFails {
				if strings.HasPrefix(k, e.IP+"|") {
					delete(s.maintFails, k)
				}
			}
			for k := range s.maintNotified {
				if strings.HasPrefix(k, e.IP+"|") {
					delete(s.maintNotified, k)
				}
			}
			s.maintMu.Unlock()
			continue
		}
		for _, e2 := range state.Entries {
			if e2.IP == e.IP {
				dead = append(dead, e2)
			}
		}
	}
	if len(dead) == 0 {
		return
	}

	// 失效计数：连续两轮才行动（防单次抖动误报）。判失效与「只提醒一次」的标记
	// 都在计数处完成，键统一为 ip|hostname——提醒写、恢复清理、判重三处同键
	//（2026-11-16 复核：曾出现写入键 hostname|ips 与读取键错位，去重完全失效）。
	var actionable []deadEntry
	s.maintMu.Lock()
	for _, e := range dead {
		key := e.IP + "|" + e.Hostname
		s.maintFails[key]++
		if s.maintFails[key] >= maintenanceFailThreshold && !s.maintNotified[key] {
			s.maintNotified[key] = true
			actionable = append(actionable, e)
		}
	}
	s.maintMu.Unlock()
	if len(actionable) == 0 {
		return
	}

	// 按主机名去重出「失效的主机名集合」，找归属项目并尝试补位。
	deadHosts := map[string][]string{} // hostname -> dead ips
	for _, e := range actionable {
		deadHosts[e.Hostname] = append(deadHosts[e.Hostname], e.IP)
	}
	for hostname, ips := range deadHosts {
		text := fmt.Sprintf("%s 的 IP（%s）已失效", hostname, strings.Join(ips, "、"))
		s.emitNotice("warning", text+"（已写入的映射不再可用）")

		if !cfg.Maintenance.AutoReplace {
			continue
		}
		itemID, ok, shared := s.maintFindItem(hostname)
		if shared {
			s.emitNotice("warning", fmt.Sprintf("%s 被多个项目共同写入，自动补位无法单方面完成——请手动重新检测后补充", hostname))
			continue
		}
		if ok {
			s.maintReplace(itemID, hostname, ips)
		}
	}
}

// maintProbeIP 探一个「主机名+IP」现在是否可用。返回 (活着, 是否计入失败)：
// 等网络闸超时＝本轮被大流量挤掉，不算 IP 死（不计失败，下一轮再看）；
// 探测本身超时/失败＝真死，计失败。
func (s *Service) maintProbeIP(ip, hostname string) (bool, bool) {
	gateCtx, gateCancel := context.WithTimeout(context.Background(), maintenanceProbeTimeout)
	defer gateCancel()
	if s.netGate != nil {
		select {
		case s.netGate <- struct{}{}:
			defer func() { <-s.netGate }()
		case <-gateCtx.Done():
			return false, false
		}
	}
	probeCtx, probeCancel := context.WithTimeout(context.Background(), maintenanceProbeTimeout)
	defer probeCancel()
	probe := s.maintProbe
	if probe == nil {
		probe = detector.ProbeHTTPS
	}
	res, err := probe(probeCtx, detector.Target{IP: ip, Hostname: hostname})
	if err != nil {
		return false, true
	}
	status, _ := accelerator.Verdict(res)
	return status == accelerator.StatusAvailable, true
}

// maintFindItem 按主机名找归属项目 ID。优先认「Hosts 区段里真正认领着这条映射」的
// 项目（对照复核 2026-11-16：目录序取第一个会撞 ErrSharedOwner——项目 B 写的映射
// 却拿目录序靠前的 A 去补位，永远失败）；共享认领（多个项目同写一个主机名）时
// 自动补位在本版语义下不可能单方面成功，跳过并让上层发「请手动处理」。
// 目录里有该主机名但 Hosts 里无认领（从未应用/已还原）→ 取目录序第一个。
func (s *Service) maintFindItem(hostname string) (string, bool, bool) {
	if s.productCatalog == nil {
		return "", false, false
	}
	n := hosts.NormalizeHostname(hostname)

	// Hosts 里真正认领着这条主机名的项目集合（按 State 现读）。
	claiming := map[string]bool{}
	if s.priv != nil {
		if state, err := s.priv.State(); err == nil {
			for _, e := range state.Entries {
				if hosts.NormalizeHostname(e.Hostname) == n && len(e.Owners) > 0 {
					for _, owner := range e.Owners {
						claiming[owner] = true
					}
				}
			}
		}
	}

	var catalogMatches []string
	for _, cat := range s.productCatalog.Categories {
		for _, it := range cat.Items {
			for _, hn := range it.Hostnames {
				if hosts.NormalizeHostname(hn) == n {
					catalogMatches = append(catalogMatches, it.ID)
					break
				}
			}
		}
	}
	if len(catalogMatches) == 0 {
		return "", false, false
	}
	sort.Strings(catalogMatches)
	// 目录序第一个且真正认领着的 → 唯一认领者，补位交给它。
	if len(claiming) == 1 && claiming[catalogMatches[0]] {
		return catalogMatches[0], true, false
	}
	// 多个项目共同认领（同一主机名被多个项目写入）→ 任何一方换 IP 都撞 ErrSharedOwner，
	// 跳过补位并明示需要手动处理（shared=true）。
	if len(claiming) > 1 {
		return catalogMatches[0], true, true
	}
	// 没有认领者（Hosts 区段里这条主机名已不在或无主）→ 目录序第一个可补位。
	return catalogMatches[0], true, false
}

// maintReplace 给一个项目做自动补位：从候选池挑活 IP（排除刚失效的），复验后重写
// 该项目的整段映射，成功后发通知。失败如实发警告。
func (s *Service) maintReplace(itemID, hostname string, deadIPs []string) {
	deadSet := make(map[string]bool, len(deadIPs))
	for _, ip := range deadIPs {
		deadSet[ip] = true
	}
	// 先把失效 IP 在内存与池两处都标成不可用（2026-11-15 真机教训）：
	// 只标池子不够——内存结果也覆盖着该域名，usablePicksAt 的「内存优先」会把死 IP
	// 当最优继续选，池里的活 IP 被排除规则挡住，补位拿到空集。两处都标之后，
	// usablePicksFor 自然落到池里的活 IP。
	for _, ip := range deadIPs {
		at := time.Now()
		_ = s.candidateStore.MarkPoolProbe(hostname, ip, string(accelerator.StatusUnavailable), 0, at)
		s.detectResults.markProbe(itemID, hostname, ip, accelerator.StatusUnavailable,
			0, "巡检：IP 已失效", at)
	}
	// 补位取「每主机名最优」单 IP（usablePicksFor）：同主机名多 IP 的全部冗余写入
	// 被 hosts 包「一主机名一规范 IP」语义拦住（canonicalMappings 防线），
	// 需要元数据 schema 升级专项——在此之前补位换主 IP 即可达成失效恢复目标。
	picks := s.usablePicksFor(itemID)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	writable := s.verifyCandidates(ctx, itemID, picks)
	mappings := mappingsOf(writable)
	if len(mappings) == 0 {
		s.emitNotice("warning", fmt.Sprintf("%s 的 IP 已失效，候选池暂无可用地址可补位——建议重新检测", hostname))
		return
	}
	if err := s.priv.Apply(itemID, mappings); err != nil {
		log.Printf("[maintenance] %s 自动补位写入失败：%v", hostname, err)
		s.emitNotice("warning", fmt.Sprintf("%s 自动补位写入失败：%v", hostname, privilegeErrorText(err)))
		return
	}
	// 按 Hosts 现状重标「当前生效」：失效的旧 IP 从标记里消失，新 IP 标上。
	state, err := s.priv.State()
	if err == nil {
		applied := map[string][]string{}
		for _, e := range state.Entries {
			applied[e.Hostname] = append(applied[e.Hostname], e.IP)
		}
		s.detectResults.markApplied(itemID, applied)
	}
	// 写成功的候选给用户可见的一句话：哪个主机名换成了什么。
	newIPs := map[string][]string{}
	for _, p := range writable {
		newIPs[p.hostname] = append(newIPs[p.hostname], p.ip)
	}
	parts := make([]string, 0, len(newIPs))
	for h, ips := range newIPs {
		parts = append(parts, fmt.Sprintf("%s→%s", h, strings.Join(ips, "、")))
	}
	sort.Strings(parts)
	s.emitNotice("success", fmt.Sprintf("%s 已自动补位：%s", hostname, strings.Join(parts, "；")))
}

// emitNotice 经通知事件把一句面向用户的话推给界面（前端弹右下角提示）。
func (s *Service) emitNotice(kind, text string) {
	if s.emit == nil {
		return
	}
	s.emit(event.NoticePushName, event.NoticePush{Kind: kind, Text: text})
}

// maintReset 全清维护记账（比如全部还原后没有可巡检的东西）。
func (s *Service) maintReset() {
	s.maintMu.Lock()
	s.maintFails = map[string]int{}
	s.maintNotified = map[string]bool{}
	s.maintMu.Unlock()
}
