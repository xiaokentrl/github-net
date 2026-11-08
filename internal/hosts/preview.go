package hosts

import (
	"errors"
	"fmt"
	"sort"
)

// RestorePreviewRow 是还原预览里的一行对比：某个当前由本工具管理的域名，还原之后会变成什么。
// Before 是当前生效的 IP（还原前的样子）；After 是还原后这个域名指向的 IP，
// 空串表示还原后这条映射被整条拿掉（应用前本来就没有）。
type RestorePreviewRow struct {
	Hostname string `json:"hostname"`
	Before   string `json:"before"`
	After    string `json:"after"`
}

// RestorePreview 是还原预览的全部内容。
// Count 是受管理域名条数（Rows 的行数），Items 是涉及的项目数——
// 执行那边按项目逐个还原，回执报的是项目数，两个口径都给出来界面才对得上。
type RestorePreview struct {
	Rows  []RestorePreviewRow `json:"rows"`
	Count int                 `json:"count"`
	Items int                 `json:"items"`
}

// previewOf 把「还原前全文」和「纯算出来的还原后全文」摊成逐域名前后对比。
// 只认管理区段里实际写着的映射，区段判读不了就是空预览，不拿猜的地址补。
func previewOf(content, newContent string, items int) RestorePreview {
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
	return RestorePreview{Rows: rows, Count: len(rows), Items: items}
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
	return previewOf(content, newContent, len(restored)), nil
}

// PreviewRestoreItem 纯算「只还原这一个项目之后」的前后对比，一个字节都不写盘。
// 执行侧走 RestoreItem（单项目），预览必须走同一个入口：共享域名在还有其他认领者时
// 只摘掉本项目、IP 原样留着；拿全量还原的结果来报，会把那一行错标成「删掉／变回」。
// 项目当前没有应用前快照（没应用过或已还原）时，还原是无害空操作，这里也给空预览，不报错。
func PreviewRestoreItem(meta Recovery, content, itemID string) (RestorePreview, error) {
	if status, reason := Check(content, meta); status == CheckConflict {
		return RestorePreview{}, fmt.Errorf("%w：%s", ErrConflict, reason)
	}
	_, newContent, err := RestoreItem(meta, content, itemID)
	if errors.Is(err, ErrItemNotActive) {
		return RestorePreview{}, nil
	}
	if err != nil {
		return RestorePreview{}, err
	}
	return previewOf(content, newContent, 1), nil
}

// PreviewRestoreItems 纯算「把这几个项目按给定顺序一起还原」之后的前后对比，一字节都不写盘。
// 顺序必须跟队列执行侧一致：共享域名的最终 IP 由最后一个释放者的应用前快照决定，
// 拿 N 份单项目预览去合并会一律判「不变」，漏掉整条撤掉／变回原 IP 的结果。
// 没应用过（或已还原）的项目按执行侧口径跳过：还原是无害空操作，不算进已释放条数。
func PreviewRestoreItems(meta Recovery, content string, itemIDs []string) (RestorePreview, error) {
	if status, reason := Check(content, meta); status == CheckConflict {
		return RestorePreview{}, fmt.Errorf("%w：%s", ErrConflict, reason)
	}
	running, runningContent := meta, content
	released := 0
	for _, itemID := range itemIDs {
		state, ok := projectState(running, itemID)
		if !ok {
			continue
		}
		next, nextContent, err := restoreInto(running, runningContent, state)
		if err != nil {
			return RestorePreview{}, err
		}
		running, runningContent = next, nextContent
		released++
	}
	return previewOf(content, runningContent, released), nil
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
