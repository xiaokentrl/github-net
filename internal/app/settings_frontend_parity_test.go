package app

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/config"
)

// 界面上两处「手抄后端」的地方，抄错了类型检查抓不到，后果都看得见：
//   - 保存请求少发一个字段，那个开关就永远存不进去。这一条门起因于 client.ts 里长期挂着的
//     autoDetectOnStartup: null 占位——后端早就把它换成了 autoDetectMenus，界面还在发旧名字。
//   - 事件名抄错一个字符，那条事件的载荷前端一个字都收不到，界面停在「什么都没发生」。
// 所以两边取集合相等：少一个红，多一个也红。

// TestSavePatchFieldsMatchClientBody：后端 config.Patch 的 json 标签 ⇔ client.ts 发出去的键。
func TestSavePatchFieldsMatchClientBody(t *testing.T) {
	src := readAt(t, repoRoot(t), filepath.Join("frontend", "src", "api", "client.ts"))
	block := regexp.MustCompile(`(?s)const body: Patch = \{(.*?)\n\s*\}`).FindStringSubmatch(src)
	if len(block) != 2 {
		t.Fatal("没在 client.ts 里认出 body: Patch 那一段，门自己先失效")
	}
	sent := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^\s*([A-Za-z][A-Za-z0-9]*):`).FindAllStringSubmatch(block[1], -1) {
		sent[match[1]] = true
	}
	if len(sent) < 10 {
		t.Fatalf("只从 client.ts 抓到 %d 个保存字段，不像真的", len(sent))
	}

	declared := map[string]bool{}
	patch := reflect.TypeOf(config.Patch{})
	for i := 0; i < patch.NumField(); i++ {
		tag := patch.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		declared[strings.Split(tag, ",")[0]] = true
	}

	for _, key := range onlyIn(declared, sent) {
		t.Errorf("后端 Patch 有 %q，client.ts 没发这个字段（这个开关存不进去）", key)
	}
	for _, key := range onlyIn(sent, declared) {
		t.Errorf("client.ts 发了 %q，后端 Patch 里没有这个字段（发出去被忽略，等于留着过期的占位）", key)
	}
}

// TestEventNamesMirroredInFrontend：internal/event 的事件名常量 ⇔ events.ts 订阅的名字。
// 事件不经过绑定生成器，这一层全靠手抄，所以必须逐条对上。
func TestEventNamesMirroredInFrontend(t *testing.T) {
	src := readAt(t, repoRoot(t), filepath.Join("frontend", "src", "api", "events.ts"))
	front := map[string]bool{}
	for _, match := range regexp.MustCompile(`(?m)^const [A-Z_]+ = '([^']+)'`).FindAllStringSubmatch(src, -1) {
		front[match[1]] = true
	}
	if len(front) < 5 {
		t.Fatalf("只从 events.ts 抓到 %d 个事件名，不像真的", len(front))
	}

	backend := eventNames(t)
	for _, key := range onlyIn(backend, front) {
		t.Errorf("后端有事件 %q，前端 events.ts 没订阅这一条", key)
	}
	for _, key := range onlyIn(front, backend) {
		t.Errorf("前端订阅了 %q，后端 internal/event 没有这个名字（这条事件永远不会来）", key)
	}
}

// eventNames 从 internal/event 的源码里取事件名。常量没有导出的聚合入口，反射拿不到，
// 所以读源码按形状取：事件名的值都带一个冒号（app:runtime-status 这种），
// 阶段码和原因码不带冒号，不进这一份清单。
func eventNames(t *testing.T) map[string]bool {
	t.Helper()
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "internal", "event"))
	if err != nil {
		t.Fatalf("读不到 internal/event：%v", err)
	}
	out := map[string]bool{}
	pair := regexp.MustCompile(`(?m)^\s*(?:const\s+)?[A-Za-z][A-Za-z0-9]*\s*=\s*"([^"]*)"`)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		src := readAt(t, root, filepath.Join("internal", "event", entry.Name()))
		for _, match := range pair.FindAllStringSubmatch(src, -1) {
			if strings.Contains(match[1], ":") {
				out[match[1]] = true
			}
		}
	}
	if len(out) < 5 {
		t.Fatalf("只从 internal/event 抓到 %d 个事件名，不像真的", len(out))
	}
	return out
}

func onlyIn(from, has map[string]bool) []string {
	missing := make([]string, 0, len(from))
	for key := range from {
		if !has[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}
