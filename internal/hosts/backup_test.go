package hosts

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

const (
	digestA = "1111111111111111111111111111111111111111111111111111111111111111"
	userTop = "127.0.0.1 localhost\n::1 ip6-localhost\n"
)

func TestDigestComparesMappingsNotBytes(t *testing.T) {
	cases := []struct {
		name  string
		left  string
		right string
		same  bool
	}{
		{"行序不同", block("140.82.113.4 github.com", "185.199.108.153 raw.githubusercontent.com"),
			block("185.199.108.153 raw.githubusercontent.com", "140.82.113.4 github.com"), true},
		{"同一个 IP 换等价写法", block("0:0:0:0:0:0:0:1 dual.example"), block("::1 dual.example"), true},
		{"行内主机名重复", block("1.1.1.1 dup.example dup.example"), block("1.1.1.1 dup.example"), true},
		{"行内主机名换序", block("1.1.1.1 b.example a.example"), block("1.1.1.1 a.example b.example"), true},
		{"大小写与空格", block("1.1.1.1 Up.Example   b.example"), block("1.1.1.1 up.example b.example"), true},
		{"换 IP 就是改了", block("1.1.1.1 github.com"), block("1.1.1.2 github.com"), false},
		{"加主机名就是改了", block("1.1.1.1 a.example"), block("1.1.1.1 a.example b.example"), false},
		{"删主机名就是改了", block("1.1.1.1 a.example b.example"), block("1.1.1.1 a.example"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := Digest(Parse(tc.left))
			if err != nil {
				t.Fatal(err)
			}
			r, err := Digest(Parse(tc.right))
			if err != nil {
				t.Fatal(err)
			}
			if (l == r) != tc.same {
				t.Errorf("摘要比对不对：得到 %s / %s，期望相同=%t", l, r, tc.same)
			}
		})
	}
}

func TestDigestSeparatesNoBlockFromEmptyBlock(t *testing.T) {
	absent, err := Digest(Parse(userTop))
	if err != nil {
		t.Fatal(err)
	}
	present, err := Digest(Parse(block()))
	if err != nil {
		t.Fatal(err)
	}
	if absent == present {
		t.Error("「没有区段」和「有区段但一条映射都没有」摘要相同，冲突判读会失真")
	}
}

func TestDigestRefusesUnreadableBlock(t *testing.T) {
	got, err := Digest(Parse(block("1.1.1.1 ok.example", "# 用户插进来的注释")))
	wantErr(t, err, ErrConflict, "区段判读不了时算摘要")
	if got != "" {
		t.Errorf("判读不了的区段不该返回摘要，得到 %q", got)
	}
}

func TestCheckClassifiesContentAgainstMetadata(t *testing.T) {
	written, meta := mustApply(t, Recovery{}, userTop, "item-a", Mapping{IP: "1.1.1.1", Hostnames: []string{"a.example"}})

	cases := []struct {
		name    string
		content string
		meta    Recovery
		status  CheckStatus
	}{
		{"两边都干净", userTop, Recovery{}, CheckClean},
		{"写过的区段没被动", written, meta, CheckMatched},
		{"区段被改了 IP", strings.Replace(written, "1.1.1.1", "9.9.9.9", 1), meta, CheckConflict},
		{"区段被加了行", strings.Replace(written, MarkerEnd(), "9.9.9.9 sneaky.example\n"+MarkerEnd(), 1), meta, CheckConflict},
		{"区段被整段删掉", userTop, meta, CheckConflict},
		{"有标记但元数据是空的", written, Recovery{}, CheckConflict},
		{"区段判读不了", strings.Replace(written, "a.example", "a.example # 注释", 1), meta, CheckConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, reason := Check(tc.content, tc.meta)
			if status != tc.status {
				t.Fatalf("判成 %s（%q），期望 %s", status, reason, tc.status)
			}
			if tc.status == CheckConflict && reason == "" {
				t.Error("冲突必须带一句能直接显示的话")
			}
			if tc.status != CheckConflict && reason != "" {
				t.Errorf("不是冲突就不该带原因，得到 %q", reason)
			}
		})
	}
}

func TestCheckEmptyBlockWithoutMetadataIsConflict(t *testing.T) {
	// 只剩两行标记＝本工具写过但一条映射都不剩，元数据又没有任何记录，不能当成干净状态。
	status, reason := Check(block(), Recovery{})
	if status != CheckConflict {
		t.Fatalf("判成 %s（%q），期望 conflict", status, reason)
	}
}

func TestMetadataRoundTripKeepsEveryField(t *testing.T) {
	_, meta := mustApply(t, Recovery{}, userTop, "item-a",
		Mapping{IP: "1.1.1.1", Hostnames: []string{"a.example", "b.example"}})
	data, err := meta.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalMetadata(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, meta) {
		t.Errorf("编回去不是同一份元数据:\n得到 %+v\n期望 %+v", back, meta)
	}
	again, err := back.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Errorf("同一份元数据编出两个字节串:\n%s\n%s", again, data)
	}
}

func TestMarshalRefusesInvalidMetadata(t *testing.T) {
	cases := []struct {
		name string
		meta Recovery
	}{
		{"摘要不是十六进制", Recovery{SchemaVersion: MetadataSchemaVersion, Digest: "not-a-digest"}},
		{"格式版本不认识", Recovery{SchemaVersion: 2, Digest: digestA}},
		{"认领项目没有快照", Recovery{SchemaVersion: MetadataSchemaVersion, Digest: digestA,
			Entries:  []Entry{{Hostname: "a.example", IP: "1.1.1.1", Owners: []string{"ghost"}}},
			Projects: []ProjectState{{ItemID: "item-a"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.meta.Marshal()
			wantErr(t, err, ErrBadMetadata, "编出去之前")
		})
	}
}

func TestUnmarshalMetadataRejectsBadShapes(t *testing.T) {
	entry := func(host, ip string, owners ...string) string {
		o, _ := json.Marshal(owners)
		return `{"hostname":"` + host + `","ip":"` + ip + `","owners":` + string(o) + `}`
	}
	project := func(id string, before ...string) string {
		return `{"itemId":"` + id + `","before":[` + strings.Join(before, ",") + `]}`
	}
	own := func(host, ip string) string {
		return `{"hostname":"` + host + `","ip":"` + ip + `"}`
	}
	oneEntry := entry("a.example", "1.1.1.1", "item-a")
	oneProject := project("item-a", own("a.example", ""))

	cases := []struct {
		name string
		json string
	}{
		{"格式版本", `{"schemaVersion":2,"digest":"` + digestA + `}"`},
		{"未知字段", `{"schemaVersion":1,"digest":"` + digestA + `","extra":1}`},
		{"坏 JSON", `{"schemaVersion":1,`},
		{"空内容", ``},
		{"摘要太短且是奇数", `{"schemaVersion":1,"digest":"abc"}`},
		{"摘要短但十六进制合法", `{"schemaVersion":1,"digest":"abcd","entries":null,"projects":null}`},
		{"摘要大写", `{"schemaVersion":1,"digest":"` + strings.Repeat("A", 64) + `","entries":null,"projects":null}`},
		{"条目 IP 不规范化", metaJSON(entry("a.example", "0:0:0:0:0:0:0:1"), oneProject)},
		{"主机名重复", metaJSON(oneEntry+","+oneEntry, oneProject)},
		{"条目没排序", metaJSON(entry("b.example", "2.2.2.2", "item-a")+","+oneEntry, oneProject)},
		{"主机名不是规范写法", metaJSON(entry("A.Example", "1.1.1.1", "item-a"), oneProject)},
		{"所有者没排序", metaJSON(entry("a.example", "1.1.1.1", "item-b", "item-a"),
			project("item-a", own("a.example", ""))+","+project("item-b", own("a.example", "")))},
		{"所有者重复", metaJSON(entry("a.example", "1.1.1.1", "item-a", "item-a"), oneProject)},
		{"所有者是空 ID", metaJSON(entry("a.example", "1.1.1.1", ""), oneProject)},
		{"所有者没有项目快照", metaJSON(entry("a.example", "1.1.1.1", "ghost"), oneProject)},
		{"所有者有快照但没记这个主机名", metaJSON(entry("a.example", "1.1.1.1", "item-a"),
			project("item-a", own("other.example", "")))},
		{"项目 ID 是空的", metaJSON(oneEntry, project("  ", own("a.example", "")))},
		{"项目重复", metaJSON(oneEntry, oneProject+","+oneProject)},
		{"项目没排序", metaJSON(entry("a.example", "1.1.1.1", "item-b"),
			project("item-b", own("a.example", ""))+","+project("item-a", own("a.example", "")))},
		{"应用前快照主机名重复", metaJSON(oneEntry, project("item-a", own("b.example", ""), own("b.example", "")))},
		{"应用前快照没排序", metaJSON(oneEntry, project("item-a", own("b.example", ""), own("a.example", "")))},
		{"应用前快照 IP 不规范化", metaJSON(oneEntry, project("item-a", own("a.example", "0:0:0:0:0:0:0:1")))},
	}

	t.Run("合法空元数据", func(t *testing.T) {
		// 对照组：整份元数据一个字段都没填，读回来必须是「什么都没写过」的零值，不能报错。
		if _, err := UnmarshalMetadata([]byte(`{"schemaVersion":1,"digest":"` + digestA + `"}`)); err != nil {
			t.Fatalf("形状合法的元数据被拒了：%v", err)
		}
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnmarshalMetadata([]byte(tc.json))
			if err == nil {
				t.Fatalf("这份元数据形状不对，却读成功了：%s", tc.json)
			}
			if !errors.Is(err, ErrBadMetadata) {
				t.Errorf("报错没归到「元数据不可用」这一档：%v", err)
			}
		})
	}
}

func TestUnmarshalMetadataAcceptsOwnerlessRecord(t *testing.T) {
	// 契约允许的形状之一：本工具写的映射，暂时没有活动项目认领。
	content := userTop + block("1.1.1.1 a.example")
	meta := Recovery{
		SchemaVersion: MetadataSchemaVersion,
		Original:      userTop,
		Entries:       []Entry{{Hostname: "a.example", IP: "1.1.1.1"}},
		Projects:      []ProjectState{{ItemID: "item-a", Before: []Ownership{{Hostname: "a.example", IP: "9.9.9.9"}}}},
	}
	var err error
	meta.Digest, err = Digest(Parse(content))
	if err != nil {
		t.Fatal(err)
	}
	data, err := meta.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalMetadata(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, meta) {
		t.Errorf("无主映射编回去变了样:\n得到 %+v\n期望 %+v", back, meta)
	}
}

func TestRecoveryUsageFlags(t *testing.T) {
	content, meta := mustApply(t, Recovery{}, userTop, "item-a", Mapping{IP: "2.2.2.2", Hostnames: []string{"a.example"}})
	content, meta = mustApply(t, meta, content, "item-b", Mapping{IP: "1.1.1.1", Hostnames: []string{"b.example"}})

	if !meta.InUse() || meta.AllReleased() {
		t.Error("还在用的元数据被判成空了")
	}
	if got := strings.Join(meta.ActiveItemIDs(), ","); got != "item-a,item-b" {
		t.Errorf("活动项目没按 ID 排序，得到 %q", got)
	}

	empty, _, restored, err := RestoreAll(meta, content)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(restored, ",") != "item-a,item-b" {
		t.Errorf("还原清单不对：%v", restored)
	}
	if empty.InUse() || !empty.AllReleased() {
		t.Error("全部还原后还认账，落盘那层就永远不敢删元数据")
	}
	if len(empty.ActiveItemIDs()) != 0 {
		t.Errorf("全部还原后还有活动项目：%v", empty.ActiveItemIDs())
	}
	if empty.Original != userTop {
		t.Errorf("全部还原把原始事务快照弄丢了：%q", empty.Original)
	}
}

func metaJSON(entries, projects string) string {
	return `{"schemaVersion":1,"original":"127.0.0.1 localhost\n","digest":"` + digestA +
		`","entries":[` + entries + `],"projects":[` + projects + `]}`
}

func mustApply(t *testing.T, meta Recovery, content string, itemID string, mappings ...Mapping) (string, Recovery) {
	t.Helper()
	next, nextContent, err := Apply(meta, content, itemID, mappings)
	if err != nil {
		t.Fatalf("项目 %s 应用失败：%v", itemID, err)
	}
	return nextContent, next
}
