package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 计数基线来自 2026-10-27 对唯一正式原型 CATEGORY_DEFS（第 1131–1208 行）的抽取结果。
const (
	wantCategories = 11
	wantItems      = 54
	wantHostnames  = 65
	wantOccurrence = 68
)

func mustLoad(t *testing.T) *Catalog {
	t.Helper()
	c, err := Load()
	if err != nil {
		t.Fatalf("内嵌目录未通过校验: %v", err)
	}
	return c
}

func TestEmbeddedCatalogValidates(t *testing.T) {
	c := mustLoad(t)
	if c.SchemaVersion != SchemaVersion {
		t.Errorf("目录版本应为 %d，实际 %d", SchemaVersion, c.SchemaVersion)
	}
}

func TestCatalogCountsMatchPrototype(t *testing.T) {
	c := mustLoad(t)

	var items, occurrences int
	for _, cat := range c.Categories {
		items += len(cat.Items)
		for _, item := range cat.Items {
			occurrences += len(item.Hostnames)
		}
	}
	if len(c.Categories) != wantCategories {
		t.Errorf("分类数应为 %d，实际 %d", wantCategories, len(c.Categories))
	}
	if items != wantItems {
		t.Errorf("项目数应为 %d，实际 %d", wantItems, items)
	}
	if got := len(c.UniqueHostnames()); got != wantHostnames {
		t.Errorf("去重后主机名应为 %d，实际 %d", wantHostnames, got)
	}
	if occurrences != wantOccurrence {
		t.Errorf("项目引用主机名的总次数应为 %d，实际 %d", wantOccurrence, occurrences)
	}
}

// TestSharedHostnamesAreTheExpectedOnes 锁住三组共用关系；多出来或少了都算抽取变动。
func TestSharedHostnamesAreTheExpectedOnes(t *testing.T) {
	c := mustLoad(t)

	want := map[string][]string{
		"github.com":                {"github/gh-main", "custom/cu-china"},
		"raw.githubusercontent.com": {"github/gh-raw", "block/bl-steven"},
		"ubisoft.com":               {"gameplat/gp-ubi", "gamespec/gs-r6"},
	}
	shared := 0
	for hostname, owners := range want {
		got := c.ItemsByHostname(hostname)
		if strings.Join(got, ",") != strings.Join(owners, ",") {
			t.Errorf("%s 的引用项目应为 %v，实际 %v", hostname, owners, got)
		}
		shared++
	}

	// 逐项数一遍：除了上面三组，其他主机名只能被一个项目引用。
	seen := map[string]int{}
	for _, cat := range c.Categories {
		for _, item := range cat.Items {
			for _, h := range item.Hostnames {
				seen[h]++
			}
		}
	}
	var extra []string
	for h, n := range seen {
		if n > 1 {
			if _, ok := want[h]; !ok {
				extra = append(extra, h)
			}
		}
	}
	if len(extra) != 0 {
		t.Errorf("出现未登记的共用主机名 %v；如确属预期，请补进本测试并说明来源", extra)
	}
	if shared != 3 {
		t.Errorf("预期 3 组共用关系，实际登记 %d", shared)
	}
}

// TestScopeMapping GitHub 与 Steam 各自是独立范围，且分类与范围一一对应。
func TestScopeMapping(t *testing.T) {
	c := mustLoad(t)

	for _, want := range []struct{ category, scope string }{
		{"github", "github"},
		{"steam", "steam"},
	} {
		cat, err := findCategory(c, want.category)
		if err != nil {
			t.Fatal(err)
		}
		if cat.ScopeID != want.scope {
			t.Errorf("分类 %s 的范围应为 %s，实际 %s", want.category, want.scope, cat.ScopeID)
		}
	}
	if github, _ := findCategory(c, "github"); github.Name != "GitHub" {
		t.Errorf("GitHub 分类名称不对: %q", github.Name)
	}
	if steam, _ := findCategory(c, "steam"); steam.Name != "Steam" {
		t.Errorf("Steam 分类名称不对: %q", steam.Name)
	}
}

// TestUserSuppliedItemHasNoHostname 手动映射由用户填域名，目录里不能伪造预设值。
func TestUserSuppliedItemHasNoHostname(t *testing.T) {
	c := mustLoad(t)

	item, err := findItem(c, "cu-manual")
	if err != nil {
		t.Fatal(err)
	}
	if !item.UserSupplied || len(item.Hostnames) != 0 {
		t.Errorf("手动映射应标记用户自填且无预设主机名，实际 %+v", item)
	}
}

// TestProtoLinesPointAtThePrototypeDefinition 回查原型原文，确认行号与项目一一对应。
// 原型文件不在仓库里时跳过：它是冻结的抽取来源，不是运行时依赖。
func TestProtoLinesPointAtThePrototypeDefinition(t *testing.T) {
	c := mustLoad(t)

	raw, err := os.ReadFile(c.Source.File)
	if err != nil {
		// 原型在仓库根目录，测试工作目录是本包目录（internal/catalog）。
		raw, err = os.ReadFile(filepath.Join("..", "..", c.Source.File))
		if err != nil {
			t.Skipf("原型文件不可读，跳过回查: %v", err)
		}
	}
	lines := strings.Split(string(raw), "\n")
	if c.Source.LineStart < 1 || c.Source.LineEnd > len(lines) {
		t.Fatalf("原型行号范围 %d–%d 超出文件行数 %d", c.Source.LineStart, c.Source.LineEnd, len(lines))
	}

	for _, cat := range c.Categories {
		for _, item := range cat.Items {
			if item.ProtoLine < c.Source.LineStart || item.ProtoLine > c.Source.LineEnd {
				t.Errorf("项目 %s 的行号 %d 不在抽取范围 %d–%d 内", item.ID, item.ProtoLine, c.Source.LineStart, c.Source.LineEnd)
				continue
			}
			line := lines[item.ProtoLine-1]
			if !strings.Contains(line, fmt.Sprintf("{ id:'%s'", item.ID)) {
				t.Errorf("项目 %s 声称来自原型第 %d 行，该行实际是: %s", item.ID, item.ProtoLine, strings.TrimSpace(line))
			}
		}
	}
}

func TestValidateRejectsBrokenCatalogs(t *testing.T) {
	base := func() *Catalog {
		return &Catalog{
			SchemaVersion: SchemaVersion,
			Categories: []Category{{
				ID: "github", Name: "GitHub", ScopeID: "github", Order: 1,
				Items: []Item{{ID: "gh-main", Name: "主站", Order: 1, ProtoLine: 1, Hostnames: []string{"github.com"}}},
			}},
		}
	}

	cases := []struct {
		name   string
		mutate func(*Catalog)
	}{
		{"版本不符", func(c *Catalog) { c.SchemaVersion = 99 }},
		{"没有分类", func(c *Catalog) { c.Categories = nil }},
		{"分类 ID 重复", func(c *Catalog) {
			c.Categories = append(c.Categories, Category{ID: "github", Name: "重名", ScopeID: "other", Order: 2})
		}},
		{"范围重复", func(c *Catalog) {
			c.Categories = append(c.Categories, Category{ID: "steam", Name: "Steam", ScopeID: "github", Order: 2,
				Items: []Item{{ID: "st-store", Name: "商店", Order: 1, ProtoLine: 2, Hostnames: []string{"steamcommunity.com"}}}})
		}},
		{"分类顺序断裂", func(c *Catalog) { c.Categories[0].Order = 7 }},
		{"空分类", func(c *Catalog) { c.Categories[0].Items = nil }},
		{"项目 ID 重复", func(c *Catalog) {
			c.Categories[0].Items = append(c.Categories[0].Items, c.Categories[0].Items[0])
		}},
		{"项目顺序断裂", func(c *Catalog) { c.Categories[0].Items[0].Order = 3 }},
		{"缺少原型行号", func(c *Catalog) { c.Categories[0].Items[0].ProtoLine = 0 }},
		{"无主机名也没标用户自填", func(c *Catalog) { c.Categories[0].Items[0].Hostnames = nil }},
		{"主机名未转小写", func(c *Catalog) { c.Categories[0].Items[0].Hostnames = []string{"GitHub.com"} }},
		{"主机名带尾随点", func(c *Catalog) { c.Categories[0].Items[0].Hostnames = []string{"github.com."} }},
		{"存的是 URL 不是主机名", func(c *Catalog) {
			c.Categories[0].Items[0].Hostnames = []string{"raw.githubusercontent.com/StevenBlack/hosts"}
		}},
		{"带端口", func(c *Catalog) { c.Categories[0].Items[0].Hostnames = []string{"github.com:443"} }},
		{"单段名称", func(c *Catalog) { c.Categories[0].Items[0].Hostnames = []string{"localhost"} }},
		{"非 ASCII 名称", func(c *Catalog) { c.Categories[0].Items[0].Hostnames = []string{"自定义域名"} }},
		{"项目内主机名重复", func(c *Catalog) { c.Categories[0].Items[0].Hostnames = []string{"github.com", "github.com"} }},
	}

	for _, tc := range cases {
		c := base()
		tc.mutate(c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s：本应报错却通过", tc.name)
		}
	}
}

func TestLoadRejectsBadInput(t *testing.T) {
	if _, err := parse([]byte(`{`)); err == nil {
		t.Error("残缺 JSON 应拒绝")
	}
	c, err := parse([]byte(`{"schemaVersion":2,"categories":[]}`))
	if err != nil {
		t.Fatalf("解析本身不该失败: %v", err)
	}
	if err := c.Validate(); err == nil {
		t.Error("未知版本应拒绝")
	}
}

func findCategory(c *Catalog, id string) (*Category, error) {
	for i := range c.Categories {
		if c.Categories[i].ID == id {
			return &c.Categories[i], nil
		}
	}
	return nil, fmt.Errorf("目录里没有分类 %s", id)
}

func findItem(c *Catalog, id string) (*Item, error) {
	for i := range c.Categories {
		for j := range c.Categories[i].Items {
			if c.Categories[i].Items[j].ID == id {
				return &c.Categories[i].Items[j], nil
			}
		}
	}
	return nil, fmt.Errorf("目录里没有项目 %s", id)
}
