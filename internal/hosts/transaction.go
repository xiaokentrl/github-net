package hosts

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// 两个新哨兵错误：上层按 errors.Is 分支，不靠句子猜。
var (
	// ErrSharedOwner 是共享主机名的分歧：这个主机名还被别的项目认领着，而它当前指向的 IP
	// 和本次要写的不是同一个。契约要求「其他活动项目仍引用时保留该记录」，也要求
	// 不得静默抹掉别人的改动，所以既不覆盖也不合并，直接停手让上层去问用户。
	ErrSharedOwner = errors.New("主机名被多个项目认领且指向不同地址")
	// ErrItemNotActive 是这个项目当前没有可还原的应用前快照：从没应用过，或者已经还原过了。
	// 契约规定还原成功后不再显示还原入口，所以重复请求不该被当成第二次成功。
	ErrItemNotActive = errors.New("该项目当前没有可还原的应用前快照")
)

// Apply 算出「把 itemID 项目的这批映射写进区段之后」的新元数据与新 Hosts 全文。
// 纯逻辑：一个字节都不写盘，也不管备份与回滚——那属特权服务那半边。
//
// 出任何错都原样返回传进来的 meta 与 content，调用方拿不到半改状态。
//
// 三条固定行为：
//  1. 写之前先比对区段摘要（Check）。对不上就 ErrConflict 停手，不做「先清掉再写」。
//  2. 应用前快照按主机名累计：本次要写的主机名进快照，已经记过的保留最初那个值。记的是「这个项目动手
//     之前区段里的样子」：该主机名要么没有映射，要么是它自己以前写的、要么是无主的旧映射（别的项目
//     还原后留下的工具痕迹）——这三种都记当下那个 IP；还被别的项目认领着就记空，因为那条记录的存亡
//     归对方管，不该由本项目还原时去恢复。
//  3. 共享主机名按所有权合并：同一个 IP 就并进去；不同 IP 且还有其他认领者就拒。
func Apply(meta Recovery, content string, itemID string, mappings []Mapping) (Recovery, string, error) {
	if strings.TrimSpace(itemID) == "" {
		return meta, content, fmt.Errorf("%w：项目 ID 是空的", ErrBadRecord)
	}
	if status, reason := Check(content, meta); status == CheckConflict {
		return meta, content, fmt.Errorf("%w：%s", ErrConflict, reason)
	}
	want, err := canonicalMappings(mappings, itemID)
	if err != nil {
		return meta, content, err
	}

	entries := cloneEntries(meta.Entries)
	current := indexEntries(entries)

	// 快照先攒，再动条目：顺序反了就会把「本次刚写进去的 IP」当成应用前的样子。
	before := make(map[string]string)
	if state, ok := projectState(meta, itemID); ok {
		for _, o := range state.Before {
			before[o.Hostname] = o.IP
		}
	}
	record := func(host string) {
		if _, seen := before[host]; seen {
			return
		}
		prev := ""
		if entry, ok := current[host]; ok && len(withoutOwner(entry.Owners, itemID)) == 0 {
			prev = entry.IP
		}
		before[host] = prev
	}
	for _, host := range sortedIPs(want) {
		if entry, ok := current[host]; ok && entry.IP != want[host] && len(withoutOwner(entry.Owners, itemID)) > 0 {
			return meta, content, fmt.Errorf("%w：%s 当前指向 %s，仍被 %s 认领，项目 %s 不能把它改成 %s",
				ErrSharedOwner, host, entry.IP, strings.Join(entry.Owners, "、"), itemID, want[host])
		}
		record(host)
	}

	// 释放：该项目原来认领、这次不再写的主机名。还有其他认领者就只摘自己。
	for _, host := range sortedHosts(current) {
		if _, keep := want[host]; keep {
			continue
		}
		idx, ok := indexOfEntry(entries, host)
		if !ok || !hasOwner(entries[idx].Owners, itemID) {
			continue
		}
		entries[idx].Owners = withoutOwner(entries[idx].Owners, itemID)
		if len(entries[idx].Owners) == 0 {
			entries = removeEntry(entries, host)
		}
	}
	// 写入：新映射落进条目集，认领者排序去重。
	for _, host := range sortedIPs(want) {
		if idx, ok := indexOfEntry(entries, host); ok {
			entries[idx].IP = want[host]
			entries[idx].Owners = withOwner(entries[idx].Owners, itemID)
			continue
		}
		entries = append(entries, Entry{Hostname: host, IP: want[host], Owners: []string{itemID}})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Hostname < entries[j].Hostname })
	snapshot := make([]Ownership, 0, len(before))
	for host, ip := range before {
		snapshot = append(snapshot, Ownership{Hostname: host, IP: ip})
	}
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].Hostname < snapshot[j].Hostname })

	next, nextContent, err := finish(meta, content, entries, itemID, snapshot)
	if err != nil {
		return meta, content, err
	}
	return next, nextContent, nil
}

// RestoreItem 释放 itemID 项目的所有认领，并把它应用前的映射放回去。
// 还原的是这一个项目：不恢复整份 Hosts，也不清空整个区段；区段外的用户行由 Render 原样保留。
//
// 共享主机名只在「最后一个所有者释放」时才动那条记录，还有其他认领者就原样留着。
func RestoreItem(meta Recovery, content string, itemID string) (Recovery, string, error) {
	if status, reason := Check(content, meta); status == CheckConflict {
		return meta, content, fmt.Errorf("%w：%s", ErrConflict, reason)
	}
	state, ok := projectState(meta, itemID)
	if !ok {
		return meta, content, fmt.Errorf("%w：%s", ErrItemNotActive, itemID)
	}
	next, nextContent, err := restoreInto(meta, content, state)
	if err != nil {
		return meta, content, err
	}
	return next, nextContent, nil
}

// RestoreAll 按项目 ID 的固定顺序逐个还原，返回还原了哪些项目供上层写清单。
//
// 任何一步出错就整体不返回新状态：这里只算字符串，写盘只有一次机会。
// 契约里那份「冲突项目单独失败、其他无冲突项目继续」的清单属服务层——
// 逐项失败（权限不足、文件被占用、写后校验不过）只有真正落盘那一层才会发生，
// 所以它循环 RestoreItem 自己收清单，本层不编一个永远不会走到的假失败分支。
func RestoreAll(meta Recovery, content string) (Recovery, string, []string, error) {
	if status, reason := Check(content, meta); status == CheckConflict {
		return meta, content, nil, fmt.Errorf("%w：%s", ErrConflict, reason)
	}
	states := make([]ProjectState, len(meta.Projects))
	copy(states, meta.Projects)
	sort.Slice(states, func(i, j int) bool { return states[i].ItemID < states[j].ItemID })

	running, runningContent := meta, content
	restored := make([]string, 0, len(states))
	for _, state := range states {
		next, nextContent, err := restoreInto(running, runningContent, state)
		if err != nil {
			return meta, content, nil, fmt.Errorf("还原项目 %s 时：%w", state.ItemID, err)
		}
		running, runningContent = next, nextContent
		restored = append(restored, state.ItemID)
	}
	return running, runningContent, restored, nil
}

// restoreInto 把单个项目的快照落回条目集，并渲染出新的元数据与全文。
func restoreInto(meta Recovery, content string, state ProjectState) (Recovery, string, error) {
	entries := cloneEntries(meta.Entries)
	current := indexEntries(entries)
	before := make(map[string]string, len(state.Before))
	for _, o := range state.Before {
		before[o.Hostname] = o.IP
	}

	// 要处置的主机名＝我现在还认领的 ∪ 快照里记过的。后者不可少：上次应用被我摘掉的主机名
	// 已经不在我名下了，但应用前它可能有映射，还原就要把它放回去。
	var hosts []string
	seen := map[string]bool{}
	for _, entry := range entries {
		if hasOwner(entry.Owners, state.ItemID) && !seen[entry.Hostname] {
			hosts = append(hosts, entry.Hostname)
			seen[entry.Hostname] = true
		}
	}
	for host := range before {
		if !seen[host] {
			hosts = append(hosts, host)
			seen[host] = true
		}
	}
	sort.Strings(hosts)

	for _, host := range hosts {
		prevIP := before[host]
		entry, exists := current[host]
		switch {
		case !exists:
			// 区段里已经没有这条了：应用前它有映射就放回去，没有就什么都不做。
			if prevIP != "" {
				entries = append(entries, Entry{Hostname: host, IP: prevIP})
			}
		case hasOwner(entry.Owners, state.ItemID):
			owners := withoutOwner(entry.Owners, state.ItemID)
			if len(owners) > 0 {
				// 还有别的项目认领：只摘掉自己，IP 与记录都原样留着。
				entry.Owners = owners
				entries = replaceEntry(entries, entry)
				continue
			}
			// 我是最后一个所有者：退回应用前的样子——有旧映射就带着空所有者放回去（它仍是本工具写的，
			// 但已经没有活动项目认领，单项还原删不掉它），没有就整条撤掉。
			if prevIP != "" {
				entry.IP = prevIP
				entry.Owners = owners
				entries = replaceEntry(entries, entry)
			} else {
				entries = removeEntry(entries, host)
			}
		default:
			// 这条映射还挂着别的认领者而我不认领它，或者与本项目的快照无关：不碰别人的映射。
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Hostname < entries[j].Hostname })

	projects := make([]ProjectState, 0, len(meta.Projects))
	for _, p := range meta.Projects {
		if p.ItemID != state.ItemID {
			projects = append(projects, p)
		}
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ItemID < projects[j].ItemID })
	return finishParts(meta, content, entries, projects)
}

// finish 组装「应用之后」的条目与项目快照。
func finish(meta Recovery, content string, entries []Entry, itemID string, before []Ownership) (Recovery, string, error) {
	projects := make([]ProjectState, 0, len(meta.Projects)+1)
	replaced := false
	for _, p := range meta.Projects {
		if p.ItemID == itemID {
			projects = append(projects, ProjectState{ItemID: itemID, Before: before})
			replaced = true
			continue
		}
		projects = append(projects, p)
	}
	if !replaced {
		projects = append(projects, ProjectState{ItemID: itemID, Before: before})
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ItemID < projects[j].ItemID })
	return finishParts(meta, content, entries, projects)
}

// finishParts 渲染新全文、重算摘要，并保住「首次写入前」那份原始快照不被覆盖。
func finishParts(meta Recovery, content string, entries []Entry, projects []ProjectState) (Recovery, string, error) {
	next, err := Render(content, recordSet(entries))
	if err != nil {
		return Recovery{}, "", err
	}
	out := Recovery{
		SchemaVersion: MetadataSchemaVersion,
		Entries:       entries,
		Projects:      projects,
	}
	if meta.Exists() {
		// 原始事务快照只在首次写入前记一次，之后任何更新都不许覆盖它。
		out.Original = meta.Original
	} else {
		out.Original = content
	}
	withDigest, err := out.withDigest(next)
	if err != nil {
		return Recovery{}, "", err
	}
	return withDigest, next, nil
}

// withDigest 用「已经渲染出来的新全文」重算摘要，而不是拿条目自己算——
// 这样存进元数据的摘要永远等于下次读到的区段形状，两边不会漂开。
//
// 这里不重复做形状校验：条目的规范化写法、排序去重由上面的 Apply/restoreInto 保证，
// 而元数据唯一的外出去处是 Marshal、唯一入口是 UnmarshalMetadata，形状门在那两端各把一次关。
func (r Recovery) withDigest(content string) (Recovery, error) {
	digest, err := Digest(Parse(content))
	if err != nil {
		return Recovery{}, err
	}
	r.Digest = digest
	return r, nil
}

// recordSet 把条目换成渲染用的记录：一条主机名一条输入，分组、排序、去重交给 Render
// 那一份规范化，这里不留第二套排序口径。
func recordSet(entries []Entry) []Record {
	records := make([]Record, 0, len(entries))
	for _, e := range entries {
		records = append(records, Record{IP: e.IP, Hostnames: []string{e.Hostname}})
	}
	return records
}

// canonicalMappings 把应用请求规范化成「主机名 → 规范 IP」。同一个主机名在一份请求里
// 给了两个 IP 就地拒掉，不是后者覆盖前者——那等于让调用方的遍历顺序决定写什么。
func canonicalMappings(mappings []Mapping, itemID string) (map[string]string, error) {
	if len(mappings) == 0 {
		return nil, fmt.Errorf("%w：项目 %s 没有给出任何主机名", ErrBadRecord, itemID)
	}
	want := make(map[string]string)
	for _, m := range mappings {
		ip := net.ParseIP(strings.TrimSpace(m.IP))
		if ip == nil {
			return nil, fmt.Errorf("%w：项目 %s 的 IP %q 不是合法地址", ErrBadRecord, itemID, m.IP)
		}
		if len(m.Hostnames) == 0 {
			return nil, fmt.Errorf("%w：项目 %s 的 %s 后面没有主机名", ErrBadRecord, itemID, ip.String())
		}
		for _, raw := range m.Hostnames {
			host := NormalizeHostname(raw)
			if !validHostname(host) {
				return nil, fmt.Errorf("%w：项目 %s 的主机名 %q 不能写进区段", ErrBadRecord, itemID, raw)
			}
			if known, seen := want[host]; seen {
				if known != ip.String() {
					return nil, fmt.Errorf("%w：项目 %s 把 %s 同时指到 %s 和 %s", ErrBadRecord, itemID, host, known, ip.String())
				}
				continue
			}
			want[host] = ip.String()
		}
	}
	return want, nil
}

func projectState(r Recovery, itemID string) (ProjectState, bool) {
	for _, p := range r.Projects {
		if p.ItemID == itemID {
			return p, true
		}
	}
	return ProjectState{}, false
}

func cloneEntries(in []Entry) []Entry {
	out := make([]Entry, 0, len(in))
	for _, e := range in {
		out = append(out, Entry{Hostname: e.Hostname, IP: e.IP, Owners: append([]string(nil), e.Owners...)})
	}
	return out
}

func indexEntries(entries []Entry) map[string]Entry {
	byHost := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byHost[e.Hostname] = e
	}
	return byHost
}

func sortedHosts(byHost map[string]Entry) []string {
	hosts := make([]string, 0, len(byHost))
	for host := range byHost {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func sortedIPs(byHost map[string]string) []string {
	hosts := make([]string, 0, len(byHost))
	for host := range byHost {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func indexOfEntry(entries []Entry, host string) (int, bool) {
	for i, e := range entries {
		if e.Hostname == host {
			return i, true
		}
	}
	return 0, false
}

func removeEntry(entries []Entry, host string) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Hostname != host {
			out = append(out, e)
		}
	}
	return out
}

func replaceEntry(entries []Entry, want Entry) []Entry {
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.Hostname == want.Hostname {
			out = append(out, want)
			continue
		}
		out = append(out, e)
	}
	return out
}

func hasOwner(owners []string, itemID string) bool {
	for _, owner := range owners {
		if owner == itemID {
			return true
		}
	}
	return false
}

func withoutOwner(owners []string, itemID string) []string {
	out := make([]string, 0, len(owners))
	for _, owner := range owners {
		if owner != itemID {
			out = append(out, owner)
		}
	}
	return out
}

// withOwner 加进认领者并保持排序去重：owners 的排序既是元数据形状校验的一项，
// 也是「同一批数据只有一种字节表示」的前提。
func withOwner(owners []string, itemID string) []string {
	out := append(withoutOwner(owners, itemID), itemID)
	sort.Strings(out)
	return out
}
