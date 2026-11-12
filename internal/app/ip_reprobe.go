package app

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/detector"
	"github.com/xiaokentrl/github-net/internal/hosts"
)

// 清理预览里那颗「检测」：现场重探一条候选池里的地址，把结论刷回那一行，再回给界面。
// 判据与检测、应用前复验共用 accelerator.Verdict / detector.ProbeHTTPS，不另起一套标准。
// 只改候选池这一行（状态、延迟、测量时间），不碰系统 Hosts、不进任务队列、不清任何缓存。
//
// 探测预算跟单次探测一致（2 秒），外面再留 1 秒余量；界面是同步等这一条，不能挂住不动。
// 并发也走全进程同一道闸，用户连点几十条不会把同时活动的外部请求数翻倍。
func (s *Service) ReprobeIP(hostname, ip string) (PoolEntryView, error) {
	host := hosts.NormalizeHostname(hostname)
	addr := strings.TrimSpace(ip)
	if host == "" || addr == "" {
		return PoolEntryView{}, NewError(CodeInvalidItemID, "域名或地址是空的，没法重新检测", nil)
	}
	if _, err := netip.ParseAddr(addr); err != nil {
		return PoolEntryView{}, NewError(CodeInvalidItemID, fmt.Sprintf("地址 %q 不是合法 IP，没法重新检测", addr), nil)
	}
	// 域名归属校验（安全边界方案：外部输入不可信）：只允许对目录里的域名发起探测，
	// 防止前端对任意域名发 HTTPS 请求。helper 的写路径本来就有目录归属门，这里补齐读路径。
	// 目录加载失败时不进入下面的归属匹配（nil 会整段绕过）——如实报目录错误，不开探测口子。
	if s.productCatalogErr != nil {
		return PoolEntryView{}, ToPublicError(s.productCatalogErr)
	}
	// 同一域名可能归属多个项目（目录里实测有跨分类共用域名）：全部记下，
	// 复测结论要回写到每个项目的内存结论里（2026-11-13 按钮审计中危：之前只写候选池，
	// 详情页 invalidate('results') 重拉 GetDetectResult 拿到的还是复测前的旧值）。
	ownerIDs := []string{}
	if s.productCatalog != nil {
		for _, cat := range s.productCatalog.Categories {
			for _, it := range cat.Items {
				known := false
				for _, hn := range it.Hostnames {
					if hosts.NormalizeHostname(hn) == host {
						known = true
						break
					}
				}
				if known {
					ownerIDs = append(ownerIDs, it.ID)
				}
			}
		}
		if len(ownerIDs) == 0 {
			return PoolEntryView{}, NewError(CodeInvalidItemID, "这个域名不在目录里，不能检测", nil)
		}
	}
	if s.candidateStore == nil {
		return PoolEntryView{}, NewError(CodeInternal, "候选底账没接上，没法重新检测", nil)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if s.netGate != nil {
		select {
		case s.netGate <- struct{}{}:
			defer func() { <-s.netGate }()
		case <-ctx.Done():
			return PoolEntryView{}, NewError(CodeInternal, "网络太忙，这条没排上检测", nil)
		}
	}

	probe := s.applyProbe
	if probe == nil {
		probe = detector.ProbeHTTPS
	}
	res, err := probe(ctx, detector.Target{IP: addr, Hostname: host})
	status := accelerator.StatusUntested
	if err == nil {
		status, _ = accelerator.Verdict(res)
	}

	at := time.Now()
	if err := s.candidateStore.MarkPoolProbe(host, addr, string(status), res.LatencyMS, at); err != nil {
		return PoolEntryView{}, ToPublicError(err)
	}
	// 内存结论同步回写（与应用复验的 recordProbeResult 同一动作）：详情页三格摘要、
	// 「可用 N」计数立刻跟上复测事实，不用等下一轮真检测。找不到对应轮就什么都不动。
	reason := ""
	if err != nil {
		reason = "探测失败"
	}
	if s.detectResults != nil {
		for _, itemID := range ownerIDs {
			s.detectResults.markProbe(itemID, host, addr, status, res.LatencyMS, reason, at)
		}
	}

	// 回读整份池再挑这一行，是为了拿到目录给的菜单归属；界面直接拿一行刷新，不用自己拼。
	pool, err := s.GetCandidatePool()
	if err != nil {
		return PoolEntryView{}, err
	}
	for _, row := range pool {
		if row.Hostname == host && row.IP == addr {
			return row, nil
		}
	}
	return PoolEntryView{}, NewError(CodeInternal, "刚检测完却没在候选池里找到这一行", nil)
}
