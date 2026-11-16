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
	go func() {
		ticker := time.NewTicker(s.maintInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.maintStop:
				return
			case <-ticker.C:
				s.maintenanceCycle()
			}
		}
	}()
}

// StopMaintenance 停掉维护循环（退出时调用）。放包级不放方法：导出方法会被 Wails
// 当成绑定命令暴露给前端（「停维护」不该是界面能点的），与 ShutdownTaskQueue 同一惯例。
func StopMaintenance(s *Service) {
	if s.maintStop != nil {
		close(s.maintStop)
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
		if s.maintProbeIP(e.IP, e.Hostname) {
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

	// 失效计数：连续两轮才行动（防单次抖动误报）。
	var actionable []deadEntry
	s.maintMu.Lock()
	for _, e := range dead {
		key := e.IP + "|" + e.Hostname
		s.maintFails[key]++
		if s.maintFails[key] >= maintenanceFailThreshold && !s.maintNotified[key] {
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
		s.maintMu.Lock()
		s.maintNotified[hostname+"|"+strings.Join(ips, ",")] = true
		s.maintMu.Unlock()
		s.emitNotice("warning", text+"（已写入的映射不再可用）")

		if !cfg.Maintenance.AutoReplace {
			continue
		}
		if itemID, ok := s.maintFindItem(hostname); ok {
			s.maintReplace(itemID, hostname, ips)
		}
	}
}

// maintProbeIP 探一个「主机名+IP」现在是否可用（走全进程网络闸）。
func (s *Service) maintProbeIP(ip, hostname string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), maintenanceProbeTimeout)
	defer cancel()
	if s.netGate != nil {
		select {
		case s.netGate <- struct{}{}:
			defer func() { <-s.netGate }()
		case <-ctx.Done():
			return false
		}
	}
	probe := s.maintProbe
	if probe == nil {
		probe = detector.ProbeHTTPS
	}
	res, err := probe(ctx, detector.Target{IP: ip, Hostname: hostname})
	if err != nil {
		return false
	}
	status, _ := accelerator.Verdict(res)
	return status == accelerator.StatusAvailable
}

// maintFindItem 按主机名找归属项目 ID（目录里同主机名归多个项目时取第一个）。
func (s *Service) maintFindItem(hostname string) (string, bool) {
	if s.productCatalog == nil {
		return "", false
	}
	n := hosts.NormalizeHostname(hostname)
	for _, cat := range s.productCatalog.Categories {
		for _, it := range cat.Items {
			for _, hn := range it.Hostnames {
				if hosts.NormalizeHostname(hn) == n {
					return it.ID, true
				}
			}
		}
	}
	return "", false
}

// maintReplace 给一个项目做自动补位：从候选池挑活 IP（排除刚失效的），复验后重写
// 该项目的整段映射，成功后发通知。失败如实发警告。
func (s *Service) maintReplace(itemID, hostname string, deadIPs []string) {
	deadSet := make(map[string]bool, len(deadIPs))
	for _, ip := range deadIPs {
		deadSet[ip] = true
	}
	// 池里先把失效 IP 标成不可用：usableAllAt 只收 available，排除即生效。
	for _, ip := range deadIPs {
		_ = s.candidateStore.MarkPoolProbe(hostname, ip, string(accelerator.StatusUnavailable), 0, time.Now())
	}
	picks := s.usableAllFor(itemID)
	picks = pickExcluding(picks, deadSet)

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

// pickExcluding 排除指定 IP（失效的那几个）。
func pickExcluding(picks []usablePick, dead map[string]bool) []usablePick {
	out := make([]usablePick, 0, len(picks))
	for _, p := range picks {
		if !dead[p.ip] {
			out = append(out, p)
		}
	}
	return out
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
