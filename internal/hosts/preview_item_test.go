package hosts

import "testing"

func previewRow(t *testing.T, p RestorePreview, hostname string) RestorePreviewRow {
	t.Helper()
	for _, r := range p.Rows {
		if r.Hostname == hostname {
			return r
		}
	}
	t.Fatalf("预览里没有 %s：%+v", hostname, p.Rows)
	return RestorePreviewRow{}
}

// 单项目还原和全量还原对共享域名给出不同结论，这条口径必须钉死：
// 界面若拿全量预览去描述「还原可用IP」，会把点下去 IP 纹丝不动的一行错标成「删掉／变回」。
func TestPreviewRestoreItemKeepsSharedHostname(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "shared.example"))
	content, meta = mustApply(t, meta, content, "item-b", mapping("1.1.1.1", "shared.example", "b-only.example"))

	one, err := PreviewRestoreItem(meta, content, "item-a")
	if err != nil {
		t.Fatalf("单项预览失败：%v", err)
	}
	if got := previewRow(t, one, "shared.example"); got.Before != "1.1.1.1" || got.After != "1.1.1.1" {
		t.Errorf("item-b 还认领着，单项还原不该动这条 IP：%+v", got)
	}
	all, err := PreviewRestore(meta, content)
	if err != nil {
		t.Fatalf("全量预览失败：%v", err)
	}
	if got := previewRow(t, all, "shared.example"); got.After != "" {
		t.Errorf("全量还原把最后一个所有者也释放了，这条应整条撤掉：%+v", got)
	}
	if one.Items != 1 || all.Items != 2 {
		t.Errorf("涉及项目数不对：单项 %d、全量 %d", one.Items, all.Items)
	}
}

// 应用前没有这条映射：单项目还原后整条撤掉，After 为空串。
func TestPreviewRestoreItemRemovesOwnHostname(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "a.example"))
	one, err := PreviewRestoreItem(meta, content, "item-a")
	if err != nil {
		t.Fatalf("单项预览失败：%v", err)
	}
	if got := previewRow(t, one, "a.example"); got.Before != "1.1.1.1" || got.After != "" {
		t.Errorf("应用前没有映射，还原后应整条撤掉：%+v", got)
	}
}

// 应用前有旧 IP：单项目还原后换回那个 IP，这是「变回原 IP」这一组的数据来源。
func TestPreviewRestoreItemBringsBackBeforeIP(t *testing.T) {
	content := userTop + block("1.1.1.1 a.example")
	meta := Recovery{
		SchemaVersion: MetadataSchemaVersion,
		Original:      userTop,
		Entries:       []Entry{{Hostname: "a.example", IP: "1.1.1.1", Owners: []string{"item-a"}}},
		Projects:      []ProjectState{{ItemID: "item-a", Before: []Ownership{{Hostname: "a.example", IP: "9.9.9.9"}}}},
	}
	digest, err := Digest(Parse(content))
	if err != nil {
		t.Fatalf("算摘要失败：%v", err)
	}
	meta.Digest = digest

	one, err := PreviewRestoreItem(meta, content, "item-a")
	if err != nil {
		t.Fatalf("单项预览失败：%v", err)
	}
	if got := previewRow(t, one, "a.example"); got.Before != "1.1.1.1" || got.After != "9.9.9.9" {
		t.Errorf("还原后应换回应用前的 IP：%+v", got)
	}
}

// 没应用过（或已还原）的项目没有快照：还原是无害空操作，预览给空而不是报错，
// 与 RestoreItems 入口「不因为没应用过就先报个错」保持同一个口径。
func TestPreviewRestoreItemInactiveIsEmpty(t *testing.T) {
	one, err := PreviewRestoreItem(Recovery{}, userTop, "never-applied")
	if err != nil {
		t.Fatalf("没应用过的项目不该报错：%v", err)
	}
	if len(one.Rows) != 0 || one.Count != 0 {
		t.Errorf("没应用过的项目应给空预览：%+v", one)
	}
}

// 「还原本菜单」一次释放多个项目：共享域名最终整条撤掉，
// 而逐个单项目预览都只会报「不变」。这条钉死联合预览不能被 N 份单项目预览合并替代。
func TestPreviewRestoreItemsReleasesSharedHostname(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "shared.example"))
	content, meta = mustApply(t, meta, content, "item-b", mapping("1.1.1.1", "shared.example", "b-only.example"))

	for _, id := range []string{"item-a", "item-b"} {
		one, err := PreviewRestoreItem(meta, content, id)
		if err != nil {
			t.Fatalf("单项预览 %s 失败：%v", id, err)
		}
		if got := previewRow(t, one, "shared.example"); got.After != got.Before {
			t.Errorf("单独还原 %s 时共享域名不该变：%+v", id, got)
		}
	}

	both, err := PreviewRestoreItems(meta, content, []string{"item-a", "item-b"})
	if err != nil {
		t.Fatalf("联合预览失败：%v", err)
	}
	if got := previewRow(t, both, "shared.example"); got.After != "" {
		t.Errorf("两个项目一起还原后共享域名应整条撤掉：%+v", got)
	}
	if both.Items != 2 {
		t.Errorf("联合预览应报 2 个已释放项目，实际 %d", both.Items)
	}
}

// 联合预览按传入顺序释放：共享域名最终的 IP 由最后释放那个项目的应用前快照决定。
// 执行侧是队列按目录顺序串行，所以预览必须照同一个顺序算，不能自己排序。
func TestPreviewRestoreItemsOrderFollowsQueue(t *testing.T) {
	content := userTop + block("1.1.1.1 shared.example")
	meta := Recovery{
		SchemaVersion: MetadataSchemaVersion,
		Original:      userTop,
		Entries:       []Entry{{Hostname: "shared.example", IP: "1.1.1.1", Owners: []string{"item-a", "item-b"}}},
		Projects: []ProjectState{
			{ItemID: "item-a", Before: []Ownership{{Hostname: "shared.example", IP: "9.9.9.9"}}},
			{ItemID: "item-b", Before: []Ownership{{Hostname: "shared.example", IP: "8.8.8.8"}}},
		},
	}
	digest, err := Digest(Parse(content))
	if err != nil {
		t.Fatalf("算摘要失败：%v", err)
	}
	meta.Digest = digest

	ab, err := PreviewRestoreItems(meta, content, []string{"item-a", "item-b"})
	if err != nil {
		t.Fatalf("联合预览失败：%v", err)
	}
	ba, err := PreviewRestoreItems(meta, content, []string{"item-b", "item-a"})
	if err != nil {
		t.Fatalf("联合预览失败：%v", err)
	}
	if got := previewRow(t, ab, "shared.example"); got.After != "8.8.8.8" {
		t.Errorf("先 a 后 b，最终该是 b 的应用前 IP：%+v", got)
	}
	if got := previewRow(t, ba, "shared.example"); got.After != "9.9.9.9" {
		t.Errorf("先 b 后 a，最终该是 a 的应用前 IP：%+v", got)
	}
}

// 批量里夹着没应用过的项目：跳过它，不报错，也不算进已释放条数。
func TestPreviewRestoreItemsSkipsInactiveItems(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "a.example"))
	got, err := PreviewRestoreItems(meta, content, []string{"never-applied", "item-a"})
	if err != nil {
		t.Fatalf("联合预览失败：%v", err)
	}
	if got.Items != 1 {
		t.Errorf("只该算 1 个已释放项目，实际 %d", got.Items)
	}
	if row := previewRow(t, got, "a.example"); row.After != "" {
		t.Errorf("item-a 还原后应整条撤掉：%+v", row)
	}
}
