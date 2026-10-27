package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/hosts"
)

func mustCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load()
	if err != nil {
		t.Fatalf("读取内嵌目录失败: %v", err)
	}
	return c
}

func TestCatalogToCategoriesKeepsCountsAndOrder(t *testing.T) {
	c := mustCatalog(t)
	cats := CatalogToCategories(c)
	if len(cats) != 11 {
		t.Fatalf("分类数 = %d，期望 11", len(cats))
	}
	var items int
	for i, cat := range cats {
		if cat.ID != c.Categories[i].ID || cat.Name != c.Categories[i].Name {
			t.Fatalf("第 %d 个分类和内部目录顺序不一致: 对外 %q，内部 %q", i, cat.ID, c.Categories[i].ID)
		}
		if cat.Order != i+1 {
			t.Errorf("分类 %s 的 order = %d，期望 %d", cat.ID, cat.Order, i+1)
		}
		if len(cat.Items) == 0 {
			t.Errorf("分类 %s 的项目列表为空", cat.ID)
		}
		for j, item := range cat.Items {
			if item.ID != c.Categories[i].Items[j].ID {
				t.Fatalf("分类 %s 第 %d 个项目顺序不一致", cat.ID, j)
			}
			if item.Order != j+1 {
				t.Errorf("项目 %s 的 order = %d，期望 %d", item.ID, item.Order, j+1)
			}
			if item.ID == "" || item.Name == "" {
				t.Errorf("项目 %s 缺少 ID 或名称", item.ID)
			}
		}
		items += len(cat.Items)
	}
	if items != 54 {
		t.Fatalf("项目总数 = %d，期望 54", items)
	}
}

func TestScopeIDIsTheCategoryID(t *testing.T) {
	for _, cat := range CatalogToCategories(mustCatalog(t)) {
		if cat.ScopeID != cat.ID {
			t.Errorf("分类 %s 的 scopeId = %q，和分类 ID 不一致", cat.ID, cat.ScopeID)
		}
	}
}

func TestHostnamesMarshalAsArrayNeverNull(t *testing.T) {
	cats := CatalogToCategories(mustCatalog(t))
	var manual *CatalogItem
	for i := range cats {
		for j := range cats[i].Items {
			if cats[i].Items[j].ID == "cu-manual" {
				manual = &cats[i].Items[j]
			}
		}
	}
	if manual == nil {
		t.Fatal("目录里找不到 cu-manual")
	}
	if manual.Hostnames == nil {
		t.Fatal("cu-manual 的 hostnames 是 nil，转换后必须是空数组")
	}
	if len(manual.Hostnames) != 0 || !manual.UserSupplied {
		t.Fatalf("cu-manual = %v, userSupplied=%v，期望空数组且 true", manual.Hostnames, manual.UserSupplied)
	}

	data, err := json.Marshal(manual)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if !strings.Contains(string(data), `"hostnames":[]`) {
		t.Fatalf("JSON 里没有空数组形状: %s", data)
	}
	if strings.Contains(string(data), "null") {
		t.Fatalf("JSON 里出现 null: %s", data)
	}
}

func TestUserSuppliedOnlyOnManualItem(t *testing.T) {
	var flagged []string
	for _, cat := range CatalogToCategories(mustCatalog(t)) {
		for _, item := range cat.Items {
			if item.UserSupplied {
				flagged = append(flagged, item.ID)
			}
			if len(item.Hostnames) > 0 && item.UserSupplied {
				t.Errorf("项目 %s 既有预设主机名又标记为用户自填", item.ID)
			}
		}
	}
	if len(flagged) != 1 || flagged[0] != "cu-manual" {
		t.Fatalf("用户自填项目 = %v，期望只有 cu-manual", flagged)
	}
}

func TestSnapshotJSONHasNoAuditFields(t *testing.T) {
	c := mustCatalog(t)
	snapshot := ItemsSnapshot{Categories: CatalogToCategories(c), Hosts: HostsStates(c, emptySnapshot(), 1)}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	s := string(data)
	for _, want := range []string{`"categories":`, `"items":`, `"hostnames":`, `"scopeId":`, `"userSupplied":`, `"hosts":`, `"itemId":`, `"status":`} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON 缺少字段 %s", want)
		}
	}
	// 逐字段名带引号和冒号比对：hostname 里就有 resources.jetbrains.com 这种含 "source" 的字符串，不能裸词匹配。
	for _, forbidden := range []string{`"protoLine"`, `"schemaVersion"`, `"lineStart"`, `"lineEnd"`, `"extractedAt"`, `"source":`, `"note"`} {
		if strings.Contains(s, forbidden) {
			t.Errorf("JSON 泄漏了内部审计字段 %q", forbidden)
		}
	}

	// 反向对照：内部目录确实带这些字段，说明上面的断言不是空跑。
	inner, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("序列化内部目录失败: %v", err)
	}
	for _, want := range []string{`"protoLine"`, `"schemaVersion"`, `"source":`} {
		if !strings.Contains(string(inner), want) {
			t.Errorf("内部目录里没有 %q，对照失效", want)
		}
	}
}

// emptySnapshot 是「读到了文件但里面一个区段标记都没有」的形状，也就是本机现在的真实情况。
func emptySnapshot() hosts.Snapshot {
	return hosts.Snapshot{Path: "/etc/hosts", ReadAt: time.Date(2026, 10, 28, 3, 0, 0, 0, time.UTC)}
}

func readHosts(t *testing.T, content string) hosts.Snapshot {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写临时 Hosts 失败：%v", err)
	}
	return hosts.Read(path)
}

func stateFor(t *testing.T, states []HostsState, itemID string) HostsState {
	t.Helper()
	for _, state := range states {
		if state.ItemID == itemID {
			return state
		}
	}
	t.Fatalf("状态列表里没有项目 %s", itemID)
	return HostsState{}
}

func TestHostsStatesOrderAndReadTrace(t *testing.T) {
	c := mustCatalog(t)
	states := HostsStates(c, emptySnapshot(), 7)
	if len(states) != 54 {
		t.Fatalf("状态条数 = %d，期望 54", len(states))
	}
	var itemIDs []string
	for _, cat := range c.Categories {
		for _, item := range cat.Items {
			itemIDs = append(itemIDs, item.ID)
		}
	}
	for i, state := range states {
		if state.ItemID != itemIDs[i] {
			t.Fatalf("第 %d 条状态指向 %s，期望 %s", i, state.ItemID, itemIDs[i])
		}
		// 读过了就必须带 revision 和读取时间，前端靠这两个值丢旧快照、说「什么时候读的」。
		if state.Revision != 7 || state.ReadAt == "" {
			t.Errorf("项目 %s 没带上这次读取的痕迹: %+v", state.ItemID, state)
		}
	}
	data, err := json.Marshal(states[0])
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if strings.Contains(string(data), `"message"`) {
		t.Fatalf("没问题的状态不该带 message 字段: %s", data)
	}
}

func TestHostsStatesWithoutBlockIsNoneForEveryItem(t *testing.T) {
	c := mustCatalog(t)
	states := HostsStates(c, emptySnapshot(), 1)
	for _, state := range states {
		if state.Status != HostsNone {
			t.Fatalf("项目 %s 的状态 = %q，区段不存在时只能报 none，不能报成检测结果", state.ItemID, state.Status)
		}
		if state.Message != "" {
			t.Errorf("项目 %s 的 none 带了原因文本 %q，界面上会像出了问题", state.ItemID, state.Message)
		}
	}
}

func TestHostsStatesReadFailedCarriesReason(t *testing.T) {
	c := mustCatalog(t)
	snap := hosts.Snapshot{ReadAt: time.Now(), ReadErr: "文件不存在：/etc/hosts"}
	states := HostsStates(c, snap, 3)
	for _, state := range states {
		if state.Status != HostsReadFailed || state.Message != "文件不存在：/etc/hosts" {
			t.Fatalf("读不到时必须逐条报 readFailed 并带上原因，实际 %+v", state)
		}
	}
}

func TestHostsStatesDamagedBlockIsConflictForAll(t *testing.T) {
	c := mustCatalog(t)
	// 有头没尾：整段区段判读不了，谁都不许报成 none 或 applied。
	snap := readHosts(t, "127.0.0.1 localhost\n"+hosts.MarkerBegin()+"\n1.2.3.4 github.com\n")
	states := HostsStates(c, snap, 1)
	if snap.Block.Usable() {
		t.Fatal("这条构造应当判读不了")
	}
	for _, state := range states {
		if state.Status != HostsConflict || state.Message == "" {
			t.Fatalf("区段判读不了要逐条报 conflict 并带原因，实际 %+v", state)
		}
	}
}

func TestHostsStatesBlockWithoutSummary(t *testing.T) {
	c := mustCatalog(t)
	// 区段完整可读，但本工具还没写过、也就没有可比对的写入摘要：
	// 有映射的项目按 conflict 报（不假装已生效），没映射的项目照实报 none。
	content := "127.0.0.1 localhost\n" +
		hosts.MarkerBegin() + "\n140.82.113.4 github.com\n" + hosts.MarkerEnd() + "\n"
	snap := readHosts(t, content)
	if !snap.OK() || !snap.Block.Usable() || !snap.Block.HasRecord("github.com") {
		t.Fatalf("构造的区段应当可读且含 github.com，实际 %+v", snap.Block)
	}
	states := HostsStates(c, snap, 2)

	var conflicted, none int
	for _, state := range states {
		switch state.Status {
		case HostsConflict:
			conflicted++
			if state.Message == "" {
				t.Errorf("项目 %s 报 conflict 却没说原因", state.ItemID)
			}
		case HostsNone:
			none++
		default:
			t.Errorf("项目 %s 报了没料到的档位 %q", state.ItemID, state.Status)
		}
	}
	// github.com 被 gh-main 和 cu-china 两个项目共用（目录里量出来的），所以正好两条 conflict。
	if conflicted != 2 || none != 52 {
		t.Errorf("conflict=%d none=%d，期望 2 和 52（共 54 项）", conflicted, none)
	}

	// 区段外的用户手写行不算本工具记录：同一份文件去掉标记，那一行就该报 none。
	noBlock := readHosts(t, "127.0.0.1 localhost\n140.82.113.4 github.com\n")
	for _, state := range HostsStates(c, noBlock, 1) {
		if state.Status != HostsNone {
			t.Fatalf("区段外的手写行被当成了本工具记录：%+v", state)
		}
	}
}

func TestToPublicErrorBranches(t *testing.T) {
	if ToPublicError(nil) != nil {
		t.Error("nil 错误应返回 nil")
	}

	original := NewError(CodeEmptyToken, "令牌不能为空", nil)
	if got := ToPublicError(original); got != original {
		t.Errorf("已经是公开错误，应原样返回，得到 %+v", got)
	}

	loadErr := fmt.Errorf("%w: 缺字段", catalog.ErrInvalid)
	got := ToPublicError(loadErr)
	if got.Code != CodeCatalogInvalid {
		t.Errorf("目录错误码 = %q，期望 %q", got.Code, CodeCatalogInvalid)
	}
	if !errors.Is(got, catalog.ErrInvalid) {
		t.Error("转换后丢失了原始错误，errors.Is 追不到 catalog.ErrInvalid")
	}

	wrapped := fmt.Errorf("读配置: %w", errors.New("权限不足"))
	got = ToPublicError(wrapped)
	if got.Code != CodeInternal {
		t.Errorf("未知错误码 = %q，期望 %q", got.Code, CodeInternal)
	}
	if strings.Contains(got.Message, "权限不足") {
		t.Errorf("内部文本透给了前端: %q", got.Message)
	}
	if !errors.Is(got, wrapped) {
		t.Error("未知错误的 cause 丢失")
	}
}

func TestGetItemsServesCatalogWithHonestStatus(t *testing.T) {
	s := NewService("github-net", "test", nil)
	// 指向临时文件，测试不读真实的 /etc/hosts：这台机器上的 Hosts 会变，断言不能跟着变。
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n127.0.1.1 kentrl\n"), 0o600); err != nil {
		t.Fatalf("写临时 Hosts 失败：%v", err)
	}
	s.hostsPath = path
	defer func() { s.hostsPath = HostsPath() }()

	snapshot, err := s.GetItems()
	if err != nil {
		t.Fatalf("内嵌目录合法，GetItems 不该失败: %v", err)
	}
	if len(snapshot.Categories) != 11 {
		t.Errorf("分类数 = %d，期望 11", len(snapshot.Categories))
	}
	unique := map[string]bool{}
	var items int
	for _, cat := range snapshot.Categories {
		for _, item := range cat.Items {
			items++
			for _, h := range item.Hostnames {
				unique[h] = true
			}
		}
	}
	if items != 54 {
		t.Errorf("项目数 = %d，期望 54", items)
	}
	if len(unique) != 65 {
		t.Errorf("去重后主机名 = %d，期望 65", len(unique))
	}
	if len(snapshot.Hosts) != 54 {
		t.Fatalf("状态条数 = %d，期望 54", len(snapshot.Hosts))
	}
	for _, h := range snapshot.Hosts {
		// 临时文件里没有区段标记，唯一诚实的结论是「已读过、区段里没有本工具的记录」。
		if h.Status != HostsNone {
			t.Fatalf("项目 %s 的状态 = %q，无标记的 Hosts 只能报 none", h.ItemID, h.Status)
		}
		if h.Revision != 1 || h.ReadAt == "" {
			t.Fatalf("项目 %s 没带上这次读取的痕迹: %+v", h.ItemID, h)
		}
	}

	// 目录确实来自后端数据而不是转换时的空壳：抽一项核对具体值。
	var raw *CatalogItem
	for i := range snapshot.Categories {
		for j := range snapshot.Categories[i].Items {
			if snapshot.Categories[i].Items[j].ID == "gh-raw" {
				raw = &snapshot.Categories[i].Items[j]
			}
		}
	}
	if raw == nil {
		t.Fatal("GetItems 里没有 gh-raw")
	}
	if raw.Name != "Raw 文件" || len(raw.Hostnames) != 1 || raw.Hostnames[0] != "raw.githubusercontent.com" {
		t.Errorf("gh-raw 内容与目录不一致: %+v", raw)
	}
}

// revision 是「第几次读」的计数：前端靠它丢掉旧快照，所以每次读都必须比上一次大。
func TestGetItemsRevisionIncreasesPerRead(t *testing.T) {
	s := NewService("github-net", "test", nil)
	path := filepath.Join(t.TempDir(), "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1 localhost\n"), 0o600); err != nil {
		t.Fatalf("写临时 Hosts 失败：%v", err)
	}
	s.hostsPath = path

	first, err := s.GetItems()
	if err != nil {
		t.Fatalf("GetItems 不该失败: %v", err)
	}
	second, err := s.GetItems()
	if err != nil {
		t.Fatalf("第二次 GetItems 不该失败: %v", err)
	}
	if !(first.Hosts[0].Revision < second.Hosts[0].Revision) {
		t.Errorf("第二次读到的 revision = %d，没比第一次 %d 大", second.Hosts[0].Revision, first.Hosts[0].Revision)
	}
	if first.Hosts[0].ReadAt > second.Hosts[0].ReadAt {
		t.Errorf("读取时间倒退了：%q 晚于 %q", first.Hosts[0].ReadAt, second.Hosts[0].ReadAt)
	}
}

func TestGetItemsFailsWithStableCodeWhenCatalogIsBroken(t *testing.T) {
	s := NewService("github-net", "test", nil)
	s.productCatalog = nil
	s.productCatalogErr = fmt.Errorf("%w: 缺字段", catalog.ErrInvalid)

	snapshot, err := s.GetItems()
	if err == nil {
		t.Fatal("目录坏了必须报错，不能返回空目录让界面显示成「没有任何项目」")
	}
	var publicErr *Error
	if !errors.As(err, &publicErr) || publicErr.Code != CodeCatalogInvalid {
		t.Errorf("错误码 = %v，期望 %q", err, CodeCatalogInvalid)
	}
	if snapshot.Categories == nil || snapshot.Hosts == nil {
		t.Errorf("出错时快照字段应是空数组而不是 null: %+v", snapshot)
	}
	if len(snapshot.Categories) != 0 || len(snapshot.Hosts) != 0 {
		t.Errorf("出错时不该带任何目录内容: %+v", snapshot)
	}
}

// 前端已按这条文本断言，改错误格式时这里必须先红：错误码常量的值不许漂移。
func TestEmptyTokenWireTextAndCode(t *testing.T) {
	s := NewService("github-net", "test", nil)
	_, err := s.Ping("   ")
	if err == nil {
		t.Fatal("空令牌应被拒绝")
	}
	if err.Error() != "empty_token: 令牌不能为空" {
		t.Errorf("错误文本 = %q，前端已有断言依赖这个字符串", err.Error())
	}
	var publicErr *Error
	if !errors.As(err, &publicErr) || publicErr.Code != CodeEmptyToken {
		t.Errorf("错误码常量与线上文本不一致: %+v", err)
	}
}
