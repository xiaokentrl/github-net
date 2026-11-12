package app

import (
	"github.com/xiaokentrl/github-net/internal/task"
)

// 这一条命令只管「检测出来的 IP 那一摊缓存」：SQLite 轮底账、每轮候选、
// 按 IP 去重的地址池，以及本进程内存里每个项目最近一轮的结论。
// 不碰配置、不碰系统 Hosts 里已生效的区段、不碰还原依据、也不碰任务队列——
// 那些各有各的主人，混进来就说不清「清掉的到底是什么」。
//
// 和启动时的 PruneCaches 不是一回事：那边只删超过 3 天保留期的文件，
// 这一条是用户主动点的「全清」，不看时间，一行不留。

// IPCacheClearResult 是「清理全部 IP 缓存」的回执，只报真删掉的行数。
// LedgerAttached 说明这次落盘底账接没接上：没接上时只清掉了内存结论，
// 回执不能说成「清掉 0 轮 0 条」那样像本来就空。
type IPCacheClearResult struct {
	Rounds         int  `json:"rounds"`
	Addresses      int  `json:"addresses"`
	LedgerAttached bool `json:"ledgerAttached"`
}

// IPCachePreview 是「清理全部 IP 缓存」的预览：把真正会被清掉的四处都报出来，
// 界面据此让用户看清要清的是什么，不漏报、也不用别的数顶替域名数。
type IPCachePreview struct {
	Rounds        int             `json:"rounds"`
	Addresses     int             `json:"addresses"`
	Domains       int             `json:"domains"`
	CandidateRows int             `json:"candidateRows"`
	MemoryResults int             `json:"memoryResults"`
	Entries       []PoolEntryView `json:"entries"`
}

// ClearAllIPCache 清掉全部检测缓存。底账没打开（库没接上）时仍要清内存这一份：
// 界面手上那份旧结论一样是缓存，留着就是「清完了却还看得到」。
func (s *Service) ClearAllIPCache() (IPCacheClearResult, error) {
	// 在途检测任务收尾会把结论重新写回内存与底账：跑着清缓存，回执说「清了 N 条」
	// 却马上长回来（2026-11-13 按钮审计低危）。工具栏按钮被 queueBusy 挡住常规路径，
	// 这里再如实拒绝一道。
	for _, t := range s.queueTasks() {
		if t.Kind == task.KindDetect && task.IsActive(t.Status) {
			return IPCacheClearResult{}, NewError(CodeQueueBusy, "还有检测任务在跑，等它完成再清缓存——不然刚清完就会被检测结论填回来", nil)
		}
	}
	if s.candidateStore != nil {
		rounds, addresses, err := s.candidateStore.ClearAll()
		if err != nil {
			// 事务整笔回滚了，一个字都没删：失败要如实报错，不回一个「清掉 0 条」糊过去。
			return IPCacheClearResult{}, NewError(CodeInternal, "IP 缓存没能清干净，这次一个字都没删", err)
		}
		if s.detectResults != nil {
			s.detectResults.clear()
		}
		return IPCacheClearResult{Rounds: int(rounds), Addresses: int(addresses), LedgerAttached: true}, nil
	}
	if s.detectResults != nil {
		s.detectResults.clear()
	}
	return IPCacheClearResult{}, nil
}

// PreviewIPCache 只读回「全部 IP 缓存」现在有什么，不改任何东西。
// Domains 是去重后的域名数（一条域名可能有多条地址），不是地址行数；
// CandidateRows 是逐轮候选记录数，MemoryResults 是内存里最近一轮结论的项目数——
// 这两处执行时也会一起清掉，预览必须说全。
func (s *Service) PreviewIPCache() (IPCachePreview, error) {
	out := IPCachePreview{}
	if s.candidateStore != nil {
		rounds, addresses, err := s.candidateStore.CountAll()
		if err != nil {
			return out, ToPublicError(err)
		}
		candidates, err := s.candidateStore.CountCandidateRows()
		if err != nil {
			return out, ToPublicError(err)
		}
		out.Rounds = int(rounds)
		out.Addresses = int(addresses)
		out.CandidateRows = int(candidates)
	}
	if s.detectResults != nil {
		out.MemoryResults = s.detectResults.count()
	}
	pool, err := s.GetCandidatePool()
	if err != nil {
		return out, err
	}
	out.Entries = pool
	seen := make(map[string]bool, len(pool))
	for _, e := range pool {
		seen[e.Hostname] = true
	}
	out.Domains = len(seen)
	return out, nil
}
