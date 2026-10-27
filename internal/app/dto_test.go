package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/catalog"
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
	snapshot := ItemsSnapshot{Categories: CatalogToCategories(c), Hosts: UnknownHosts(c)}
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

func TestUnknownHostsIsHonestPlaceholder(t *testing.T) {
	c := mustCatalog(t)
	hosts := UnknownHosts(c)
	if len(hosts) != 54 {
		t.Fatalf("状态条数 = %d，期望 54", len(hosts))
	}
	var itemIDs []string
	for _, cat := range c.Categories {
		for _, item := range cat.Items {
			itemIDs = append(itemIDs, item.ID)
		}
	}
	for i, h := range hosts {
		if h.ItemID != itemIDs[i] {
			t.Fatalf("第 %d 条状态指向 %s，期望 %s", i, h.ItemID, itemIDs[i])
		}
		if h.Status != HostsUnknown {
			t.Errorf("项目 %s 的状态 = %q，期望 unknown", h.ItemID, h.Status)
		}
		if h.Revision != 0 || h.ReadAt != "" || h.Message != "" {
			t.Errorf("项目 %s 的占位状态带上了已读过的痕迹: %+v", h.ItemID, h)
		}
	}
	data, err := json.Marshal(UnknownHosts(c)[0])
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if strings.Contains(string(data), `"message"`) {
		t.Fatalf("占位状态不该带 message 字段: %s", data)
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
		if h.Status != HostsUnknown {
			t.Fatalf("Hosts 读取服务还不存在，状态却报 %q，等于伪装真实状态", h.Status)
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
