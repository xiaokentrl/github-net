package hosts

import (
	"fmt"
	"sort"
)

// RestorePreviewRow 是「清除已写入的 Hosts」预览里的一行对比：
// 某个当前由本工具管理的域名，在全量还原之后会变成什么。
// Before 是当前生效的 IP（还原前的样子）；After 是还原后这个域名指向的 IP，
// 空串表示还原后这条映射被整条拿掉（应用前本来就没有）。
type RestorePreviewRow struct {
	Hostname string `json:"hostname"`
	Before   string `json:"before"`
	After    string `json:"after"`
}

// RestorePreview 是「清除已写入的 Hosts」预览的全部内容。
// Count 是受管理域名条数（Rows 的行数），Items 是涉及的项目数——
// 执行那边按项目逐个还原，回执报的是项目数，两个口径都给出来界面才对得上。
type RestorePreview struct {
	Rows  []RestorePreviewRow `json:"rows"`
	Count int                 `json:"count"`
	Items int                 `json:"items"`
}

// PreviewRestore 纯算「全量还原之后」每个受管理域名的前后对比，一个字节都不写盘。
// 复用 RestoreAll 的事务逻辑算出还原后的 Hosts 全文，再和当前区段逐域名比对：
// 当前区段里有、还原后还在的，IP 可能变回应用前的值（或不变——还被别的项目认领着）；
// 当前有、还原后没了的，就是被整条移除。
// 没有任何由本工具管理的映射时返回空预览（没有要还原的东西）。
func PreviewRestore(meta Recovery, content string) (RestorePreview, error) {
	if status, reason := Check(content, meta); status == CheckConflict {
		return RestorePreview{}, fmt.Errorf("%w：%s", ErrConflict, reason)
	}
	_, newContent, restored, err := RestoreAll(meta, content)
	if err != nil {
		return RestorePreview{}, err
	}
	before := hostIPMap(Parse(content))
	after := hostIPMap(Parse(newContent))
	rows := make([]RestorePreviewRow, 0, len(before))
	for host, beforeIP := range before {
		rows = append(rows, RestorePreviewRow{
			Hostname: host,
			Before:   beforeIP,
			After:    after[host],
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Hostname < rows[j].Hostname })
	return RestorePreview{Rows: rows, Count: len(rows), Items: len(restored)}, nil
}

// hostIPMap 把管理区段压成「域名 → IP」的映射（一条记录可能带多个域名，共享同一个 IP）。
// 区段结构不可用时返回空映射，调用方据此把「看不懂的区段」当没有可预览的映射。
func hostIPMap(block Block) map[string]string {
	out := make(map[string]string)
	if !block.Usable() {
		return out
	}
	for _, rec := range block.Records {
		for _, host := range rec.Hostnames {
			out[host] = rec.IP
		}
	}
	return out
}
