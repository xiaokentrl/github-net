// Package catalog 是产品目录（分类、项目、目标 hostname）的唯一数据来源。
// 数据由 2026-10-27 从唯一正式原型的 CATEGORY_DEFS 一次性抽取，运行时不解析 HTML。
package catalog

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

//go:embed catalog.json
var catalogJSON []byte

// SchemaVersion 是当前目录文件结构版本；只接受这一版，不兼容旧结构。
const SchemaVersion = 1

// Catalog 是整份产品目录。Categories 的顺序就是原型左侧菜单的显示顺序。
type Catalog struct {
	SchemaVersion int        `json:"schemaVersion"`
	Source        Source     `json:"source"`
	Categories    []Category `json:"categories"`
}

// Source 记录抽取来源，供人工抽样核对：文件、区域和行号范围。
type Source struct {
	File        string `json:"file"`
	Region      string `json:"region"`
	LineStart   int    `json:"lineStart"`
	LineEnd     int    `json:"lineEnd"`
	ExtractedAt string `json:"extractedAt"`
	Note        string `json:"note"`
}

// Category 是一个服务分类，ScopeID 是该分类共享加速方法选择的加速范围。
type Category struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	ScopeID string `json:"scopeID"`
	Order   int    `json:"order"`
	Items   []Item `json:"items"`
}

// Item 是一个可单独检测的项目。Hostnames 只存规范化后的主机名，不存完整 URL。
// UserSupplied 为 true 表示该项目的主机名由用户自己填，目录里没有预设值。
type Item struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Order        int      `json:"order"`
	ProtoLine    int      `json:"protoLine"`
	Hostnames    []string `json:"hostnames"`
	UserSupplied bool     `json:"userSupplied,omitempty"`
}

// ErrInvalid 表示目录内容不合格。上层按这个哨兵转成稳定错误码，不再解析中文文案。
var ErrInvalid = errors.New("目录内容不合法")

// Load 解析并校验内嵌目录，任何一项不合格都返回错误，不做静默修正。
// 解析失败和内容不合格都带上 ErrInvalid，让上层能统一按「目录不合法」处理。
func Load() (*Catalog, error) {
	c, err := parse(catalogJSON)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalid, err)
	}
	return c, nil
}

// parse 只做 JSON 解析，把校验留给 Validate。
func parse(data []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("目录解析失败: %w", err)
	}
	return &c, nil
}

// Validate 检查：版本、ID 唯一、顺序连续、范围引用有效、主机名已规范化。
func (c *Catalog) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("目录版本 %d 不受支持，当前只接受 %d", c.SchemaVersion, SchemaVersion)
	}
	if len(c.Categories) == 0 {
		return fmt.Errorf("目录没有任何分类")
	}

	categoryIDs := make(map[string]bool, len(c.Categories))
	itemIDs := make(map[string]string, 64)
	scopes := make(map[string]bool, len(c.Categories))
	for i, cat := range c.Categories {
		if cat.ID == "" || cat.Name == "" {
			return fmt.Errorf("第 %d 个分类缺少 ID 或名称", i+1)
		}
		if categoryIDs[cat.ID] {
			return fmt.Errorf("分类 ID 重复: %s", cat.ID)
		}
		categoryIDs[cat.ID] = true
		if cat.ScopeID == "" {
			return fmt.Errorf("分类 %s 没有指定加速范围 scopeID", cat.ID)
		}
		if scopes[cat.ScopeID] {
			return fmt.Errorf("多个分类共用同一个加速范围 scopeID %s，首期不允许", cat.ScopeID)
		}
		scopes[cat.ScopeID] = true
		if cat.Order != i+1 {
			return fmt.Errorf("分类 %s 的显示顺序是 %d，应为 %d", cat.ID, cat.Order, i+1)
		}
		if len(cat.Items) == 0 {
			return fmt.Errorf("分类 %s 没有任何项目", cat.ID)
		}

		for j, item := range cat.Items {
			if item.ID == "" || item.Name == "" {
				return fmt.Errorf("分类 %s 第 %d 个项目缺少 ID 或名称", cat.ID, j+1)
			}
			if owner, ok := itemIDs[item.ID]; ok {
				return fmt.Errorf("项目 ID %s 重复，出现在分类 %s 和 %s", item.ID, owner, cat.ID)
			}
			itemIDs[item.ID] = cat.ID
			if item.Order != j+1 {
				return fmt.Errorf("项目 %s 的显示顺序是 %d，应为 %d", item.ID, item.Order, j+1)
			}
			if item.ProtoLine <= 0 {
				return fmt.Errorf("项目 %s 缺少原型行号，无法回查来源", item.ID)
			}
			if len(item.Hostnames) == 0 && !item.UserSupplied {
				return fmt.Errorf("项目 %s 没有主机名，也没有标记为用户自填", item.ID)
			}
			seen := make(map[string]bool, len(item.Hostnames))
			for _, h := range item.Hostnames {
				if err := checkHostname(item.ID, h); err != nil {
					return err
				}
				if seen[h] {
					return fmt.Errorf("项目 %s 的主机名 %s 在本项目内重复", item.ID, h)
				}
				seen[h] = true
			}
		}
	}
	return nil
}

// checkHostname 只接受已经规范化过的 ASCII 主机名：小写、无尾随点、无端口路径。
func checkHostname(itemID, h string) error {
	if h == "" {
		return fmt.Errorf("项目 %s 有空主机名", itemID)
	}
	if strings.ToLower(h) != h {
		return fmt.Errorf("项目 %s 的主机名 %s 未转小写", itemID, h)
	}
	if strings.HasSuffix(h, ".") {
		return fmt.Errorf("项目 %s 的主机名 %s 带尾随点", itemID, h)
	}
	if strings.ContainsAny(h, "/:?)@_ ") || !strings.Contains(h, ".") {
		return fmt.Errorf("项目 %s 的主机名 %s 不是纯主机名", itemID, h)
	}
	if len(h) > 253 {
		return fmt.Errorf("项目 %s 的主机名 %s 超过 253 字符", itemID, h)
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("项目 %s 的主机名 %s 有一段长度不合法", itemID, h)
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("项目 %s 的主机名 %s 有一段以连字符开头或结尾", itemID, h)
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
				return fmt.Errorf("项目 %s 的主机名 %s 含非法字符 %q（目录只用 ASCII 主机名）", itemID, h, r)
			}
		}
	}
	return nil
}

// UniqueHostnames 返回整份目录去重、排序后的主机名，供探测预算核算使用。
func (c *Catalog) UniqueHostnames() []string {
	set := make(map[string]bool, 64)
	for _, cat := range c.Categories {
		for _, item := range cat.Items {
			for _, h := range item.Hostnames {
				set[h] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// ItemsByHostname 返回引用了某个主机名的分类 ID，按目录顺序排列。
// 多个项目共用同一主机名是允许的正常情况，这里只把关系摊开供测试和界面核对。
func (c *Catalog) ItemsByHostname(hostname string) []string {
	var owners []string
	for _, cat := range c.Categories {
		for _, item := range cat.Items {
			for _, h := range item.Hostnames {
				if h == hostname {
					owners = append(owners, cat.ID+"/"+item.ID)
				}
			}
		}
	}
	return owners
}
