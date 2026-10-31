package app

import (
	"sort"

	"github.com/xiaokentrl/github-net/internal/hosts"
)

// PoolEntryView 是候选池给前端的一行：一个「域名 + IP」的最新已知状态，
// 加上它归属的二级菜单（目录里哪些项目的域名正好是这个主机名）。
// 一个域名可能同时写在多个二级菜单里（比如 github.com 同属主站和镜像项），
// 归属给成数组，筛选时命中任意一个就算。
type PoolEntryView struct {
	Hostname  string   `json:"hostname"`
	IP        string   `json:"ip"`
	IPv6      bool     `json:"ipv6"`
	Status    string   `json:"status"`
	LatencyMS int64    `json:"latencyMs"`
	SourceID  string   `json:"sourceId"`
	UpdatedAt string   `json:"updatedAt"`
	ItemNames []string `json:"itemNames"`
}

// GetCandidatePool 读整份候选池（缓存语义：每个域名只有最近一轮的地址），
// 按延迟升序排（没测出延迟的按域名+地址顺序垫底），归属菜单按目录顺序。
// 池子打不开不是错误：给空列表，界面显示「还没有候选」。
func (s *Service) GetCandidatePool() ([]PoolEntryView, error) {
	if s.candidateStore == nil {
		return []PoolEntryView{}, nil
	}
	rows, err := s.candidateStore.Pool("")
	if err != nil {
		return nil, ToPublicError(err)
	}
	// hostname → 二级菜单名。目录是唯一来源，现场扫一遍：几十个项目，线性足够。
	byHost := map[string][]string{}
	for _, category := range s.productCatalog.Categories {
		for _, item := range category.Items {
			for _, h := range item.Hostnames {
				n := hosts.NormalizeHostname(h)
				if n == "" {
					continue
				}
				if !containsString(byHost[n], item.Name) {
					byHost[n] = append(byHost[n], item.Name)
				}
			}
		}
	}
	out := make([]PoolEntryView, 0, len(rows))
	for _, r := range rows {
		names := byHost[r.Hostname]
		if names == nil {
			names = []string{}
		}
		out = append(out, PoolEntryView{
			Hostname:  r.Hostname,
			IP:        r.IP,
			IPv6:      r.IPv6,
			Status:    r.Status,
			LatencyMS: r.LatencyMS,
			SourceID:  r.SourceID,
			UpdatedAt: r.UpdatedAt,
			ItemNames: names,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, lj := out[i].LatencyMS, out[j].LatencyMS
		if (li > 0) != (lj > 0) {
			return li > 0
		}
		if li > 0 && li != lj {
			return li < lj
		}
		if out[i].Hostname != out[j].Hostname {
			return out[i].Hostname < out[j].Hostname
		}
		return out[i].IP < out[j].IP
	})
	return out, nil
}

func containsString(list []string, s string) bool {
	for _, got := range list {
		if got == s {
			return true
		}
	}
	return false
}
