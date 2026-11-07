package app

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/sources/github"
)

// fakeSources 记下「清理已选缓存」这一步收到什么清单、按用例需要返回失败或删除结果。
// 它不落盘：这条命令的单测不许碰本机真实配置目录。
type fakeSources struct {
	enabled     bool
	calls       [][]github.Source
	removed     []string
	failWith    error
	pruneCalls  int
	cachedToday []string
}

func (f *fakeSources) ClearCache(srcs []github.Source) ([]string, error) {
	f.calls = append(f.calls, srcs)
	// 真实的 FileCache 是「删掉几份、另外几份失败」一起返回；假件照这个形状，
	// 才能验「部分失败」时界面看得到已经删掉的那几份。
	return f.removed, f.failWith
}

func (f *fakeSources) CacheEnabled() bool { return f.enabled }

// 预览用：假件默认当作「今天都还没下载过」，需要时用例自己塞 cachedToday。
func (f *fakeSources) CachedToday(_ []github.Source) []string { return f.cachedToday }

// PruneCache 记下启动清理有没有走到公共源这一层（启动时只走一次，界内界外由 github 包自己的用例验）。
func (f *fakeSources) PruneCache() error {
	f.pruneCalls++
	return f.failWith
}

// newSourcesService 只装配这条命令读得到的东西：目录 + 注入的清理接缝。
func newSourcesService(clearer sourcesClearer) *Service {
	return &Service{productCatalog: detectCatalog(), sourceCache: clearer}
}

// hostNames 把清理清单拼成一行，方便整串比对（顺序也是断言的一部分）。
func sourceIDs(srcs []github.Source) string {
	ids := make([]string, 0, len(srcs))
	for _, src := range srcs {
		ids = append(ids, src.ID)
	}
	return strings.Join(ids, ",")
}

func TestClearSourcesCacheCleansCheckedScope(t *testing.T) {
	fake := &fakeSources{enabled: true}
	s := newSourcesService(fake)

	got, err := s.ClearSourcesCache([]string{"api"})
	if err != nil {
		t.Fatalf("勾中 github 范围的项目应当清理成功，实际报错：%v", err)
	}
	if got.Items != 1 {
		t.Errorf("Items 该记勾中 1 个项目，实际 %d", got.Items)
	}
	if len(got.Sources) != 3 {
		t.Errorf("github 范围该有 3 份公共源，实际 %v", got.Sources)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("清理接缝该被调 1 次，实际 %d 次", len(fake.calls))
	}
	if ids := sourceIDs(fake.calls[0]); ids != "gitcdn,hellogithub,gitlab" {
		t.Errorf("送去清理的源清单不对，实际 %q", ids)
	}
}

// 一份源都没删掉（今天还没下载过）不是失败：Cleared 给空数组，界面上报「今天还没下载过」。
func TestClearSourcesCacheReportsNothingCleared(t *testing.T) {
	fake := &fakeSources{enabled: true, removed: nil}
	s := newSourcesService(fake)

	got, err := s.ClearSourcesCache([]string{"api"})
	if err != nil {
		t.Fatalf("没清到东西也该是成功，实际报错：%v", err)
	}
	if got.Cleared == nil {
		t.Error("Cleared 该是空数组而不是 nil，前端拿到 null 要额外判空")
	}
	if len(got.Cleared) != 0 {
		t.Errorf("没删到文件时 Cleared 该是空的，实际 %v", got.Cleared)
	}
}

// Cleared 只报真删掉的那几份，顺序跟着源清单走——磁盘返回的先后不作数。
func TestClearSourcesCacheClearedKeepsSourceOrder(t *testing.T) {
	fake := &fakeSources{enabled: true, removed: []string{"gitlab"}}
	s := newSourcesService(fake)

	got, err := s.ClearSourcesCache([]string{"api", "bare"})
	if err != nil {
		t.Fatalf("清理失败：%v", err)
	}
	if len(got.Cleared) != 1 || got.Cleared[0] != "GitLab 公共源（ineo6）" {
		t.Errorf("Cleared 该只有那一份被删的源，实际 %v", got.Cleared)
	}
	if got.Items != 2 {
		t.Errorf("两个 github 项目该算 2 项，实际 %d", got.Items)
	}
}

// 勾了哪个菜单就清哪个菜单的源：跨菜单时两边的源并成一份清单，范围去重。
func TestClearSourcesCacheMergesScopesAndDedups(t *testing.T) {
	fake := &fakeSources{enabled: true}
	s := newSourcesService(fake)

	got, err := s.ClearSourcesCache([]string{"api", "steam", "api", " "})
	if err != nil {
		t.Fatalf("清理失败：%v", err)
	}
	if got.Items != 2 {
		t.Errorf("重复 ID 和空 ID 不该计数，Items 该是 2，实际 %d", got.Items)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("一次入口调用只清一轮，实际调了 %d 次", len(fake.calls))
	}
	// steam 范围现在没有公共源，并集还是 github 那三份。
	if ids := sourceIDs(fake.calls[0]); ids != "gitcdn,hellogithub,gitlab" {
		t.Errorf("跨菜单并集后的清单不对，实际 %q", ids)
	}
}

// 勾中的菜单都不用公共源（只有商店）：这不是失败，也不该去动清理接缝。
func TestClearSourcesCacheScopeWithoutSourcesSucceeds(t *testing.T) {
	fake := &fakeSources{enabled: true}
	s := newSourcesService(fake)

	got, err := s.ClearSourcesCache([]string{"steam"})
	if err != nil {
		t.Fatalf("勾中不用公共源的菜单该成功回「没得清」，实际报错：%v", err)
	}
	if got.Sources == nil || len(got.Sources) != 0 {
		t.Errorf("Sources 该是空数组，实际 %v", got.Sources)
	}
	if len(fake.calls) != 0 {
		t.Errorf("没有要清的源时不该碰清理接缝，实际调了 %d 次", len(fake.calls))
	}
}

// 落盘那一层没接上：说「清掉了 0 份」是骗人的，要按失败回错误码。
func TestClearSourcesCacheRequiresDiskLayer(t *testing.T) {
	fake := &fakeSources{enabled: false}
	s := newSourcesService(fake)

	_, err := s.ClearSourcesCache([]string{"api"})
	publicErr := ToPublicError(err)
	if publicErr == nil || publicErr.Code != CodeInternal {
		t.Fatalf("没接落盘该回 internal_error，实际 %+v", publicErr)
	}
	if !strings.Contains(publicErr.Message, "源缓存目录没接上") {
		t.Errorf("消息要说清是目录没接上，实际 %q", publicErr.Message)
	}
	if len(fake.calls) != 0 {
		t.Errorf("接缝说没接上时不该再调删除，实际调了 %d 次", len(fake.calls))
	}
}

func TestClearSourcesCacheRejectsBadBatch(t *testing.T) {
	cases := []struct {
		name  string
		ids   []string
		code  string
		frage string
	}{
		{"没勾中", []string{}, CodeInvalidItemID, "没有勾中任何项目"},
		{"全是空白 ID", []string{" ", ""}, CodeInvalidItemID, "没有勾中任何项目"},
		{"目录里没有", []string{"api", "ghost"}, CodeInvalidItemID, "产品目录里没有这个项目"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeSources{enabled: true}
			s := newSourcesService(fake)
			_, err := s.ClearSourcesCache(tc.ids)
			publicErr := ToPublicError(err)
			if publicErr == nil || publicErr.Code != tc.code {
				t.Fatalf("%s 该回 %s，实际 %+v", tc.name, tc.code, publicErr)
			}
			if !strings.Contains(publicErr.Message, tc.frage) {
				t.Errorf("消息该含 %q，实际 %q", tc.frage, publicErr.Message)
			}
			// 整批拒绝：一项对不上就一份都不清，不留「清了一半」。
			if len(fake.calls) != 0 {
				t.Errorf("%s 时不该调用删除，实际调了 %d 次", tc.name, len(fake.calls))
			}
		})
	}
}

func TestClearSourcesCacheDeleteFailureIsVisible(t *testing.T) {
	fake := &fakeSources{enabled: true, failWith: errors.New("磁盘拒绝这次删除")}
	s := newSourcesService(fake)

	_, err := s.ClearSourcesCache([]string{"api"})
	publicErr := ToPublicError(err)
	if publicErr == nil || publicErr.Code != CodeInternal {
		t.Fatalf("删失败该回 internal_error，实际 %+v", publicErr)
	}
	if !strings.Contains(publicErr.Message, "没能删掉") {
		t.Errorf("消息要说清是没删掉，实际 %q", publicErr.Message)
	}
	if publicErr.Unwrap() == nil {
		t.Error("底层原因要留在 cause 里给日志，不能只留一句中文")
	}
}

func TestClearSourcesCacheWithoutCatalogFails(t *testing.T) {
	s := &Service{productCatalogErr: catalog.ErrInvalid, sourceCache: &fakeSources{enabled: true}}
	_, err := s.ClearSourcesCache([]string{"api"})
	publicErr := ToPublicError(err)
	if publicErr == nil || publicErr.Code != CodeCatalogInvalid {
		t.Fatalf("目录没加载成该回 catalog_invalid，实际 %+v", publicErr)
	}
}

// 装配层真的把落盘缓存接到了那一份共用拉源器上——不是只写了单测里那个假件。
// 这条如果没接上，界面上清完缓存、下一轮检测照样用内存里那份旧的，「清理」就成了空动作。
//
// 换掉整个 github.Default 再跑，是为了不把接好缓存的那一份留给别的用例。
func TestNewServiceWiresSourceCache(t *testing.T) {
	if _, err := config.Path(); err != nil {
		t.Skipf("拿不到配置目录，这一轮验不了接线：%v", err)
	}
	old := github.Default
	github.Default = github.NewFetcher(&http.Client{}, time.Hour)
	t.Cleanup(func() { github.Default = old })

	NewService("GitHub Net", "0.1.0", nil, nil)
	if !github.Default.CacheEnabled() {
		t.Error("NewService 没把当天落盘缓存接到共用拉源器上")
	}
}
