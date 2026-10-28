package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 界面文案的门：前端每一处 t('某个键') 都必须在 zh-CN.json 里有这一条。
// 少了这条门，界面上会直接印出「缺文案:settings.about.title」这种自检串——
// 用户看到的就是坏掉的界面，而这在类型检查里完全合法，只能靠这一条抓。

// usedKey 抓 t('...') 里的字面键；带参数的 t('...', {..}) 也一样抓得到。
var usedKey = regexp.MustCompile(`\bt\('([A-Za-z0-9_.\-]+)'\s*[,)]`)

func TestEveryLiteralMessageKeyExists(t *testing.T) {
	root := repoRoot(t)
	localePath := filepath.Join("frontend", "src", "i18n", "messages", "zh-CN.json")
	var messages map[string]json.RawMessage
	if err := json.Unmarshal([]byte(readAt(t, root, localePath)), &messages); err != nil {
		t.Fatalf("翻译文件 %s 不是扁平的键值对象：%v", localePath, err)
	}
	if len(messages) == 0 {
		t.Fatalf("翻译文件 %s 一条文案都没有，门自己先失效", localePath)
	}

	// 键 → 第一个用到它的位置，报错时能直接定位。
	missing := map[string]string{}
	err := filepath.WalkDir(filepath.Join(root, "frontend", "src"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name := filepath.Base(path)
		if !strings.HasSuffix(name, ".ts") && !strings.HasSuffix(name, ".tsx") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, m := range usedKey.FindAllStringSubmatch(string(src), -1) {
			if _, defined := messages[m[1]]; defined {
				continue
			}
			if _, seen := missing[m[1]]; !seen {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					rel = path
				}
				missing[m[1]] = filepath.ToSlash(rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫前端源码失败：%v", err)
	}

	keys := make([]string, 0, len(missing))
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		t.Errorf("%s 里 t(%q) 用的这条文案在 %s 中不存在", missing[key], key, localePath)
	}
}
