package app

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 错误码表的门：后端声明的每一个 Code* 常量，前端那份登记表必须逐字对上。
// 前端按码把「code: 人话」里的机器码摘掉再显示，漏登的码摘不掉，
// 界面上就会直接印出「hosts_failed: 没能打开 Hosts 文件」这种自检串——
// 类型检查抓不到，只能靠这条比两边。多登的也算失败，别让前端留着后端已经删掉的码。

// goCodeConst 抓 Go 里的 `CodeXxx = "xxx"` 常量声明（整行形状，不吞注释里的引号）。
var goCodeConst = regexp.MustCompile(`^\s*Code[A-Za-z0-9_]*\s*=\s*"([a-z0-9_]+)"\s*$`)

// tsCodeLine 抓 TS 数组里的一条 `'xxx',`（整行形状，单双引号都收，行尾逗号可选）。
var tsCodeLine = regexp.MustCompile(`^\s*["']([a-z0-9_]+)["'],?\s*$`)

func TestErrorCodeListsMatch(t *testing.T) {
	root := repoRoot(t)

	goCodes := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "internal", "app"), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, line := range strings.Split(string(src), "\n") {
			if m := goCodeConst.FindStringSubmatch(line); m != nil {
				goCodes[m[1]] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("扫后端错误码失败：%v", err)
	}
	if len(goCodes) < 20 {
		t.Fatalf("只抓到 %d 个后端错误码，多半是门自己的正则失效了", len(goCodes))
	}

	tsPath := filepath.Join("frontend", "src", "api", "errors.ts")
	tsCodes := map[string]bool{}
	inList := false
	for _, line := range strings.Split(readAt(t, root, tsPath), "\n") {
		switch {
		case !inList && strings.HasPrefix(strings.TrimSpace(line), "export const APP_ERROR_CODES"):
			inList = true
			continue
		case !inList:
			continue
		case strings.HasPrefix(strings.TrimSpace(line), "]"):
			inList = false
			continue
		}
		if m := tsCodeLine.FindStringSubmatch(line); m != nil {
			tsCodes[m[1]] = true
		}
	}
	if inList {
		t.Fatalf("%s 里的 APP_ERROR_CODES 数组没有正常收尾，门读不出清单", tsPath)
	}
	if len(tsCodes) == 0 {
		t.Fatalf("没从 %s 读到任何错误码，门自己先失效", tsPath)
	}

	var missing, stale []string
	for code := range goCodes {
		if !tsCodes[code] {
			missing = append(missing, code)
		}
	}
	for code := range tsCodes {
		if !goCodes[code] {
			stale = append(stale, code)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 || len(stale) > 0 {
		t.Fatalf("错误码表两边不一致：后端有、前端 %s 没登记 = %s；前端登记了、后端已经没有 = %s", tsPath, strings.Join(missing, " "), strings.Join(stale, " "))
	}
}
