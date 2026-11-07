package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/sources/github"
)

// 预览要标出「今天已下载」：只有这几份会被真删，没下载的点了也不会动。
func TestPreviewSourcesCacheMarksDownloaded(t *testing.T) {
	fake := &fakeSources{enabled: true, cachedToday: []string{"gitcdn"}}
	s := newSourcesService(fake)

	rows, err := s.PreviewSourcesCache([]string{"api"})
	if err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("github 范围该有 3 份源，实际 %d", len(rows))
	}
	byName := map[string]bool{}
	for _, r := range rows {
		byName[r.Name] = r.Downloaded
	}
	if !byName["GitCDN 公共源"] {
		t.Errorf("今天下过的那份该标已下载，实际 %+v", rows)
	}
	if byName["HelloGitHub 公共源"] || byName["GitLab 公共源（ineo6）"] {
		t.Errorf("今天没下过的不该标已下载，实际 %+v", rows)
	}
}

// 预览只读：不能碰删除接缝。
func TestPreviewSourcesCacheDoesNotDelete(t *testing.T) {
	fake := &fakeSources{enabled: true, cachedToday: []string{"gitcdn"}}
	s := newSourcesService(fake)
	if _, err := s.PreviewSourcesCache([]string{"api"}); err != nil {
		t.Fatalf("预览失败: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("预览只读，不该调删除，实际 %d 次", len(fake.calls))
	}
}

// 逐份删文件没法整笔回滚：失败时消息里要说出已经删掉的那几份，别让用户以为一个字没动。
func TestClearSourcesCachePartialFailureNamesCleared(t *testing.T) {
	fake := &fakeSources{
		enabled:  true,
		removed:  []string{"gitcdn"},
		failWith: errors.New("磁盘拒绝这次删除"),
	}
	s := newSourcesService(fake)
	_, err := s.ClearSourcesCache([]string{"api"})
	publicErr := ToPublicError(err)
	if publicErr == nil || publicErr.Code != CodeInternal {
		t.Fatalf("删失败该回 internal_error，实际 %+v", publicErr)
	}
	if !strings.Contains(publicErr.Message, "没能删掉") {
		t.Errorf("消息要说清是没删掉，实际 %q", publicErr.Message)
	}
	if !strings.Contains(publicErr.Message, "GitCDN 公共源") || !strings.Contains(publicErr.Message, "不会退回") {
		t.Errorf("消息要说出已经删掉、不回滚的那份，实际 %q", publicErr.Message)
	}
}

// 装配层真的把落盘缓存接上了，CachedToday 用的是同一份共用拉源器。
func TestFetcherCachedTodayReadsRealCache(t *testing.T) {
	dir := t.TempDir()
	cache := github.NewFileCache(dir)
	f := github.NewFetcher(nil, 0)
	f.UseCache(cache)
	srcs := []github.Source{{ID: "a", Name: "源 A", URL: "https://a.example"}}
	if got := f.CachedToday(srcs); len(got) != 0 {
		t.Errorf("没下过时该是空，实际 %v", got)
	}
	if err := cache.Save(srcs[0], map[string][]github.Entry{"github.com": {}}); err != nil {
		t.Fatalf("准备落盘失败: %v", err)
	}
	if got := f.CachedToday(srcs); len(got) != 1 || got[0] != "a" {
		t.Errorf("下过之后该报出这份源，实际 %v", got)
	}
}
