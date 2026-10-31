package hosts

import (
	"reflect"
	"strings"
	"testing"
)

func mapping(ip string, hosts ...string) Mapping {
	return Mapping{IP: ip, Hostnames: hosts}
}

func TestApplyFirstWriteAppendsBlockAndSnapshots(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "b.example", "a.example"))

	want := userTop + "\n" + block("1.1.1.1 a.example b.example")
	if content != want {
		t.Errorf("写出来的全文不对:\n得到 %q\n期望 %q", content, want)
	}
	if meta.Original != userTop {
		t.Errorf("原始事务快照没记下来：%q", meta.Original)
	}
	if !meta.Exists() || !meta.InUse() {
		t.Error("写过一次之后元数据必须算「存在且在用」")
	}
	// 一条主机名一条记录，条目按主机名排序，认领者只有这个项目。
	wantEntries := []Entry{
		{Hostname: "a.example", IP: "1.1.1.1", Owners: []string{"item-a"}},
		{Hostname: "b.example", IP: "1.1.1.1", Owners: []string{"item-a"}},
	}
	if !reflect.DeepEqual(meta.Entries, wantEntries) {
		t.Errorf("条目不对:\n得到 %+v\n期望 %+v", meta.Entries, wantEntries)
	}
	wantBefore := []Ownership{{Hostname: "a.example"}, {Hostname: "b.example"}}
	if !reflect.DeepEqual(meta.Projects, []ProjectState{{ItemID: "item-a", Before: wantBefore}}) {
		t.Errorf("应用前快照不对：%+v", meta.Projects)
	}
	if status, reason := Check(content, meta); status != CheckMatched {
		t.Errorf("刚写完的区段自己比对不上：%s（%q）", status, reason)
	}
}

func TestApplyKeepsUserLinesAndBlockPosition(t *testing.T) {
	// 区段夹在中间：替换必须原地，前后用户行逐字节不动。
	head := "# 我的 hosts\n127.0.0.1   LOCALHOST\n"
	tail := "9.9.9.9 tail.example\n"
	old := block("8.8.8.8 old.example")
	before := head + old + tail
	meta := mustMetaWithOldBlock(t, head, old, tail)

	content, next := mustApply(t, meta, before, "item-a", mapping("1.1.1.1", "mid.example"))
	if !strings.HasPrefix(content, head) || !strings.HasSuffix(content, tail) {
		t.Fatalf("区段外的用户行被改了：%q", content)
	}
	if strings.Contains(content, "old.example") || !strings.Contains(content, "1.1.1.1 mid.example") {
		t.Errorf("区段没被原地换掉：%q", content)
	}
	if next.Original != before {
		t.Errorf("元数据还在用，原始事务快照不许被覆盖：%q", next.Original)
	}
}

func TestApplyReplacesSameIPTwoSpellingsWithCanonicalOne(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a",
		mapping("0:0:0:0:0:0:0:68", "ipv6.example"), mapping("1.1.1.1", "a.example"))
	if !strings.Contains(content, "::68 ipv6.example") {
		t.Errorf("IP 没按规范写法落进区段：%q", content)
	}
	if len(meta.Entries) != 2 || meta.Entries[1].IP != "::68" {
		t.Errorf("条目规范化不对：%+v", meta.Entries)
	}
}

func TestApplyRejectsBadRequests(t *testing.T) {
	cases := []struct {
		name     string
		itemID   string
		mappings []Mapping
		target   error
	}{
		{"项目 ID 是空的", "", []Mapping{mapping("1.1.1.1", "a.example")}, ErrBadRecord},
		{"没给任何主机名", "item-a", nil, ErrBadRecord},
		{"IP 不合法", "item-a", []Mapping{mapping("300.1.1.1", "a.example")}, ErrBadRecord},
		{"IP 是空的", "item-a", []Mapping{mapping("", "a.example")}, ErrBadRecord},
		{"主机名列表是空的", "item-a", []Mapping{{IP: "1.1.1.1"}}, ErrBadRecord},
		{"主机名带端口", "item-a", []Mapping{mapping("1.1.1.1", "a.example:8080")}, ErrBadRecord},
		{"主机名是路径", "item-a", []Mapping{mapping("1.1.1.1", "a.example/evil")}, ErrBadRecord},
		{"主机名是 IP", "item-a", []Mapping{mapping("1.1.1.1", "1.2.3.4")}, ErrBadRecord},
		{"同一个主机名给两个 IP", "item-a", []Mapping{mapping("1.1.1.1", "a.example"), mapping("2.2.2.2", "a.example")}, ErrBadRecord},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, content, err := Apply(Recovery{}, userTop, tc.itemID, tc.mappings)
			wantErr(t, err, tc.target, "项目 item-a 的这份应用请求")
			if content != userTop {
				t.Errorf("请求被拒还改了全文：%q", content)
			}
			if !reflect.DeepEqual(next, Recovery{}) {
				t.Errorf("请求被拒还改了元数据：%+v", next)
			}
		})
	}
}

func TestApplyRefusesConflictingBlock(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "a.example"))
	tampered := strings.Replace(content, "1.1.1.1", "9.9.9.9", 1)

	next, got, err := Apply(meta, tampered, "item-b", []Mapping{mapping("2.2.2.2", "b.example")})
	wantErr(t, err, ErrConflict, "区段被外部改过之后的应用")
	if got != tampered {
		t.Error("冲突时不许动全文")
	}
	if !reflect.DeepEqual(next, meta) {
		t.Errorf("冲突时不许动元数据:\n得到 %+v\n期望 %+v", next, meta)
	}
	if status, _ := Check(tampered, meta); status != CheckConflict {
		t.Error("这份区段本该判成冲突")
	}
}

func TestApplyDoesNotMutateInputs(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "a.example"))
	beforeContent := content
	beforeMeta, _ := meta.Marshal()

	_, _ = mustApply(t, meta, content, "item-b", mapping("2.2.2.2", "b.example"))

	if content != beforeContent {
		t.Error("上一次的全文被后一次应用改掉了")
	}
	afterMeta, err := meta.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(afterMeta) != string(beforeMeta) {
		t.Errorf("上一次的元数据被后一次应用改掉了:\n%s\n%s", afterMeta, beforeMeta)
	}
}

func TestApplyRereleaseDropsOnlyOwnClaim(t *testing.T) {
	// A 先写 a.example，B 再写同一个 IP 的同一个主机名＝合并认领；
	// A 第二次应用不再带它＝只摘掉自己，记录留给 B。
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "shared.example"))
	content, meta = mustApply(t, meta, content, "item-b", mapping("1.1.1.1", "shared.example", "b-only.example"))
	if got := entryOf(meta, "shared.example").Owners; !reflect.DeepEqual(got, []string{"item-a", "item-b"}) {
		t.Fatalf("共享主机名的认领者集合不对：%v", got)
	}

	content, meta = mustApply(t, meta, content, "item-a", mapping("3.3.3.3", "a-new.example"))
	if len(meta.Entries) != 3 {
		t.Fatalf("条目数不对：%+v", meta.Entries)
	}
	if got := entryOf(meta, "shared.example"); got.IP != "1.1.1.1" || !reflect.DeepEqual(got.Owners, []string{"item-b"}) {
		t.Errorf("共享记录该只摘掉 item-a，得到 %+v", got)
	}
	if got := entryOf(meta, "b-only.example"); got.IP != "1.1.1.1" || !reflect.DeepEqual(got.Owners, []string{"item-b"}) {
		t.Errorf("B 独用的映射被 A 的第二次应用改了：%+v", got)
	}
	if entryOf(meta, "a-new.example").IP != "3.3.3.3" {
		t.Errorf("A 新写的映射没落进条目：%+v", meta.Entries)
	}
	if ipOf(t, content, "shared.example") != "1.1.1.1" {
		t.Errorf("B 还认着的映射不能从区段里消失：%q", content)
	}
	// A 这次摘掉了 shared.example，但快照里必须留着它，还原时才能把它放回 B 名下之前的地址。
	if got := projectOf(meta, "item-a").Before; len(got) != 2 ||
		got[0].Hostname != "a-new.example" || got[1].Hostname != "shared.example" {
		t.Errorf("A 的应用前快照没跟着本次应用重写：%+v", got)
	}
	if status, _ := Check(content, meta); status != CheckMatched {
		t.Errorf("A 摘掉自己之后区段与摘要对不上：%s", status)
	}
}

func TestSharedSameIPRestoresCleanlyInBothOrders(t *testing.T) {
	// 契约里最容易出错的一条：两个项目共用一条映射时，无论谁先还原，
	// 全部还原都必须回到原始 Hosts，不能留下一条没人认领的映射。
	base := func() (string, Recovery) {
		content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "shared.example"))
		return mustApply(t, meta, content, "item-b", mapping("1.1.1.1", "shared.example"))
	}

	content, meta := base()
	all, allContent, restored, err := RestoreAll(meta, content)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(restored, ",") != "item-a,item-b" {
		t.Errorf("全部还原清单不对：%v", restored)
	}
	if allContent != userTop {
		t.Errorf("全部还原没回到原始 Hosts：%q", allContent)
	}
	if !all.AllReleased() {
		t.Errorf("全部还原后元数据还挂着记录：%+v", all.Entries)
	}

	// 反过来的顺序：B 先还原，A 后还原，结果必须一模一样。
	content, meta = base()
	metaB, contentB := mustRestore(t, meta, content, "item-b")
	metaB, contentB = mustRestore(t, metaB, contentB, "item-a")
	if contentB != userTop {
		t.Errorf("B 先还原、A 后还原没回到原始 Hosts：%q", contentB)
	}
	if !metaB.AllReleased() {
		t.Errorf("两个项目都还原完，元数据还挂着记录：%+v", metaB.Entries)
	}
}

func TestSharedHostnamesKeptWhileOtherOwnerRemains(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "shared.example"))
	content, meta = mustApply(t, meta, content, "item-b", mapping("1.1.1.1", "shared.example", "b-only.example"))

	restored, content := mustRestore(t, meta, content, "item-a")
	if ipOf(t, content, "shared.example") != "1.1.1.1" {
		t.Errorf("item-b 还认着 shared.example，这条不能撤：%q", content)
	}
	if ipOf(t, content, "b-only.example") != "1.1.1.1" {
		t.Errorf("还原 item-a 把 item-b 独用的映射弄掉了：%q", content)
	}
	if ipOf(t, content, "a.example") != "" {
		t.Errorf("区段里冒出了没写过的映射：%q", content)
	}
	if strings.Join(restored.ActiveItemIDs(), ",") != "item-b" {
		t.Errorf("活动项目集合不对：%v", restored.ActiveItemIDs())
	}
	if status, _ := Check(content, restored); status != CheckMatched {
		t.Error("还原后的区段与摘要对不上")
	}

	// 剩下 item-b 一个人，它是最后一个所有者：这条映射当初也不是它先写的，整条撤掉。
	_, content = mustRestore(t, restored, content, "item-b")
	if content != userTop {
		t.Errorf("最后一个所有者还原后区段没清空：%q", content)
	}
}

func TestApplyRefusesToChangeOtherProjectsAddress(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "shared.example"))
	content, meta = mustApply(t, meta, content, "item-a", mapping("1.1.1.1", "shared.example", "a2.example"))

	next, got, err := Apply(meta, content, "item-b", []Mapping{mapping("2.2.2.2", "shared.example")})
	wantErr(t, err, ErrSharedOwner, "别的项目还认着这个主机名时的改地址请求")
	if got != content {
		t.Error("分歧时不许动全文")
	}
	if !reflect.DeepEqual(next, meta) {
		t.Errorf("分歧时不许动元数据：%+v", next)
	}
	if ipOf(t, content, "shared.example") != "1.1.1.1" {
		t.Error("被拒的请求把别人的映射写掉了")
	}

	// 同地址就无所谓：并进去，谁也不吃亏。
	content, meta = mustApply(t, meta, content, "item-b", mapping("1.1.1.1", "shared.example"))
	if got := entryOf(meta, "shared.example").Owners; !reflect.DeepEqual(got, []string{"item-a", "item-b"}) {
		t.Errorf("同地址共享没合并认领：%v", got)
	}
	if status, reason := Check(content, meta); status != CheckMatched {
		t.Errorf("合并后区段与摘要对不上：%s（%q）", status, reason)
	}
}

func TestApplyOverOwnerlessHistoricalMappingRestoresIt(t *testing.T) {
	// 元数据里合法的一种形状：本工具写的映射，暂时没有活动项目认领。
	// 接手它的项目还原时，必须把原映射放回去，而不是整条撤掉。
	content := userTop + block("9.9.9.9 old.example")
	meta := mustOwnerlessMeta(t, content)

	content, meta = mustApply(t, meta, content, "item-a", mapping("1.1.1.1", "old.example"))
	if ipOf(t, content, "old.example") != "1.1.1.1" {
		t.Fatalf("没接手成无主映射：%q", content)
	}
	if got := projectOf(meta, "item-a").Before; !reflect.DeepEqual(got, []Ownership{{Hostname: "old.example", IP: "9.9.9.9"}}) {
		t.Errorf("接手无主映射时的应用前快照不对：%+v", got)
	}

	restored, content := mustRestore(t, meta, content, "item-a")
	if ipOf(t, content, "old.example") != "9.9.9.9" {
		t.Errorf("还原没把原映射放回去：%q", content)
	}
	if got := entryOf(restored, "old.example"); got.Hostname != "old.example" || len(got.Owners) != 0 {
		t.Errorf("放回的原映射应该没有认领者：%+v", got)
	}
	if status, reason := Check(content, restored); status != CheckMatched {
		t.Errorf("放回后区段与摘要对不上：%s（%q）", status, reason)
	}
	// 无主映射还在，元数据就不算释放完，落盘那层不能删它。
	if restored.AllReleased() {
		t.Error("区段里还有工具写的映射，元数据却被判成释放完")
	}
}

func TestRestoreItemTwiceIsNotSilentlyAccepted(t *testing.T) {
	written, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "a.example"))
	restored, content := mustRestore(t, meta, written, "item-a")

	next, got, err := Apply(restored, content, "item-a", nil)
	wantErr(t, err, ErrBadRecord, "还原后再应用空请求")
	if next.InUse() || got != content {
		t.Error("空请求被拒却不干净")
	}

	_, _, err = RestoreItem(restored, content, "item-a")
	wantErr(t, err, ErrItemNotActive, "已经还原过的项目再次还原")

	_, _, err = RestoreItem(meta, written, "ghost")
	wantErr(t, err, ErrItemNotActive, "没应用过的项目请求还原")
}

func TestRestoreAllRefusesConflictAndKeepsScene(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "a.example"))
	tampered := strings.Replace(content, MarkerEnd(), "1.2.3.4 sneaky.example\n"+MarkerEnd(), 1)

	next, got, restored, err := RestoreAll(meta, tampered)
	wantErr(t, err, ErrConflict, "区段被外部加过行之后的全部还原")
	if got != tampered || len(restored) != 0 || !reflect.DeepEqual(next, meta) {
		t.Errorf("冲突的全部还原动了现场：%q / %v / %+v", got, restored, next)
	}
}

func TestApplyAndRestoreKeepCRLFAndUnterminatedLastLine(t *testing.T) {
	original := "127.0.0.1 localhost\r\n::1 ip6-localhost"
	content, meta := mustApply(t, Recovery{}, original, "item-a", mapping("1.1.1.1", "a.example"))
	if !strings.Contains(content, "\r\n") || strings.Contains(strings.ReplaceAll(content, "\r\n", ""), "\r") {
		t.Errorf("CRLF 风格没保持住：%q", content)
	}
	if !strings.HasSuffix(content, "\r\n") {
		t.Errorf("原来没写完的最后一行必须补成整行：%q", content)
	}
	_, content = mustRestore(t, meta, content, "item-a")
	if content != original+"\r\n" {
		t.Errorf("还原后没回到原文（只差结尾换行）：%q", content)
	}
}

func TestApplyIsRepeatableForSameRequest(t *testing.T) {
	first, metaA := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "b.example", "a.example"))
	second, metaB := mustApply(t, metaA, first, "item-a", mapping("1.1.1.1", "a.example", "b.example"))
	if second != first {
		t.Errorf("同一份请求再应用一次，全文变了:\n%q\n%q", second, first)
	}
	a, _ := metaA.Marshal()
	b, _ := metaB.Marshal()
	if string(a) != string(b) {
		t.Errorf("同一份请求再应用一次，元数据变了:\n%s\n%s", a, b)
	}
}

// ipOf 从区段记录里取某个主机名当前指向的地址；区段里没有它就是空串。
// 渲染按 IP 成行，一个 IP 后面的主机名不止一个，所以断言不能拼整行字符串。
func ipOf(t *testing.T, content, hostname string) string {
	t.Helper()
	block := Parse(content)
	if !block.Usable() {
		t.Fatalf("区段判读不了：%s", block.Reason)
	}
	for _, rec := range block.Records {
		for _, host := range rec.Hostnames {
			if host == hostname {
				return rec.IP
			}
		}
	}
	return ""
}

// mustOwnerlessMeta 造一份「区段里有一条本工具写的映射、但没有任何活动项目认领它」的元数据。
// 这是契约允许的落盘形状之一，接手它的项目还原时必须把原映射放回去。
func mustOwnerlessMeta(t *testing.T, content string) Recovery {
	t.Helper()
	meta := Recovery{
		SchemaVersion: MetadataSchemaVersion,
		Original:      userTop,
		Entries:       []Entry{{Hostname: "old.example", IP: "9.9.9.9"}},
		Projects:      []ProjectState{{ItemID: "item-c", Before: []Ownership{{Hostname: "other.example", IP: "8.8.8.8"}}}},
	}
	var err error
	if meta.Digest, err = Digest(Parse(content)); err != nil {
		t.Fatal(err)
	}
	if status, reason := Check(content, meta); status != CheckMatched {
		t.Fatalf("这份元数据本该判成 matched：%s（%q）", status, reason)
	}
	return meta
}

func TestApplyTwiceThenRestoreGoesBackToOriginal(t *testing.T) {
	// 同一个项目连着改两次地址，应用前快照必须留住「最初」那一次，不能被中间态覆盖：
	// 否则还原完还剩一条用户从来没要过的地址，全部还原也回不到原始 Hosts。
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", mapping("1.1.1.1", "x.example"))
	content, meta = mustApply(t, meta, content, "item-a", mapping("2.2.2.2", "x.example"))
	if ip := ipOf(t, content, "x.example"); ip != "2.2.2.2" {
		t.Fatalf("第二次应用没换成新地址：%q", content)
	}
	if got := projectOf(meta, "item-a").Before; !reflect.DeepEqual(got, []Ownership{{Hostname: "x.example"}}) {
		t.Errorf("第二次应用把应用前快照覆盖成中间态了：%+v", got)
	}

	restored, content := mustRestore(t, meta, content, "item-a")
	if content != userTop {
		t.Errorf("改过两次再还原，没回到原始 Hosts：%q", content)
	}
	if !restored.AllReleased() {
		t.Errorf("还原完还留着条目：%+v", restored.Entries)
	}
}

func TestRestorePutsBackHostnameReleasedByLaterApply(t *testing.T) {
	// 接手无主映射 → 之后一次应用不再写它（条目被撤）→ 还原时必须把它放回原处。
	// 这条门钉住「处置主机名集合＝我还认领的 ∪ 快照记过的」里那半边。
	content, meta := mustApply(t, mustOwnerlessMeta(t, userTop+block("9.9.9.9 old.example")),
		userTop+block("9.9.9.9 old.example"), "item-a", mapping("1.1.1.1", "old.example"))
	content, meta = mustApply(t, meta, content, "item-a", mapping("3.3.3.3", "other.example"))
	if ip := ipOf(t, content, "old.example"); ip != "" {
		t.Fatalf("A 不再认领的无主映射该从区段里撤掉：%q", content)
	}

	restored, content := mustRestore(t, meta, content, "item-a")
	if ip := ipOf(t, content, "old.example"); ip != "9.9.9.9" {
		t.Errorf("还原没把上次应用摘掉的原映射放回去：%q", content)
	}
	if ip := ipOf(t, content, "other.example"); ip != "" {
		t.Errorf("A 本次写的映射还原后还在：%q", content)
	}
	if got := entryOf(restored, "old.example"); got.Hostname != "old.example" || len(got.Owners) != 0 {
		t.Errorf("放回去的原映射不该带认领者：%+v", got)
	}
	// item-c 的快照跟这条无关，不能被顺手删掉。
	if strings.Join(restored.ActiveItemIDs(), ",") != "item-c" {
		t.Errorf("还原 item-a 动了别人的项目快照：%v", restored.ActiveItemIDs())
	}
}

func mustRestore(t *testing.T, meta Recovery, content string, itemID string) (Recovery, string) {
	t.Helper()
	next, nextContent, err := RestoreItem(meta, content, itemID)
	if err != nil {
		t.Fatalf("项目 %s 还原失败：%v", itemID, err)
	}
	return next, nextContent
}

// entryOf 按主机名取条目：条目按主机名排序，测试里不数下标，改了排序规则不该跟着红。
func entryOf(meta Recovery, hostname string) Entry {
	for _, e := range meta.Entries {
		if e.Hostname == hostname {
			return e
		}
	}
	return Entry{}
}

func projectOf(meta Recovery, itemID string) ProjectState {
	for _, p := range meta.Projects {
		if p.ItemID == itemID {
			return p
		}
	}
	return ProjectState{}
}

func mustMetaWithOldBlock(t *testing.T, head, old, tail string) Recovery {
	t.Helper()
	meta := Recovery{
		SchemaVersion: MetadataSchemaVersion,
		Original:      head + old + tail,
		Entries:       []Entry{{Hostname: "old.example", IP: "8.8.8.8", Owners: []string{"item-a"}}},
		Projects:      []ProjectState{{ItemID: "item-a", Before: []Ownership{{Hostname: "old.example"}}}},
	}
	var err error
	if meta.Digest, err = Digest(Parse(head + old + tail)); err != nil {
		t.Fatal(err)
	}
	return meta
}
