package app

// 这一条命令只管「检测出来的 IP 那一摊缓存」：SQLite 轮底账、每轮候选、
// 按 IP 去重的地址池，以及本进程内存里每个项目最近一轮的结论。
// 不碰配置、不碰系统 Hosts 里已生效的区段、不碰还原依据、也不碰任务队列——
// 那些各有各的主人，混进来就说不清「清掉的到底是什么」。
//
// 和启动时的 PruneCaches 不是一回事：那边只删超过 3 天保留期的文件，
// 这一条是用户主动点的「全清」，不看时间，一行不留。

// IPCacheClearResult 是「清理全部 IP 缓存」的回执，只报真删掉的行数。
type IPCacheClearResult struct {
	Rounds    int `json:"rounds"`
	Addresses int `json:"addresses"`
}

// IPCachePreview 是「清理全部 IP 缓存」的预览：报数量、也给明细，界面据此让用户看清要清的是什么。
type IPCachePreview struct {
	Rounds    int             `json:"rounds"`
	Addresses int             `json:"addresses"`
	Domains   int             `json:"domains"`
	Entries   []PoolEntryView `json:"entries"`
}

// ClearAllIPCache 清掉全部检测缓存。底账没打开（库没接上）时仍要清内存这一份：
// 界面手上那份旧结论一样是缓存，留着就是「清完了却还看得到」。
func (s *Service) ClearAllIPCache() (IPCacheClearResult, error) {
	if s.candidateStore != nil {
		rounds, addresses, err := s.candidateStore.ClearAll()
		if err != nil {
			// 事务整笔回滚了，一个字都没删：失败要如实报错，不回一个「清掉 0 条」糊过去。
			return IPCacheClearResult{}, NewError(CodeInternal, "IP 缓存没能清干净，这次一个字都没删", err)
		}
		if s.detectResults != nil {
			s.detectResults.clear()
		}
		return IPCacheClearResult{Rounds: int(rounds), Addresses: int(addresses)}, nil
	}
	if s.detectResults != nil {
		s.detectResults.clear()
	}
	return IPCacheClearResult{}, nil
}

// PreviewIPCache 只读回「全部 IP 缓存」现在有什么：轮底账数、地址池行数、候选池明细。
// 不改任何东西，界面在用户勾选「清理全部 IP 缓存」时调用，让他先看清楚再决定。
func (s *Service) PreviewIPCache() (IPCachePreview, error) {
	out := IPCachePreview{}
	if s.candidateStore != nil {
		rounds, addresses, err := s.candidateStore.CountAll()
		if err != nil {
			return out, ToPublicError(err)
		}
		out.Rounds = int(rounds)
		out.Addresses = int(addresses)
	}
	pool, err := s.GetCandidatePool()
	if err != nil {
		return out, err
	}
	out.Entries = pool
	out.Domains = len(pool)
	return out, nil
}
