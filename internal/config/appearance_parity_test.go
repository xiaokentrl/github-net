package config

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// 外观这一屏的值散在四个地方：后端的允许表（本包）、前端主题清单、主题令牌文件、文案文件。
// 漏任何一处的后果都是看得见的：界面显示成「缺文案:theme.xxx.name」、点了没配色的主题，
// 或者某个深色主题里露出一块白底。所以这里一律用两边集合相等，少一个红，多一个也红。
//
// 尺寸类令牌（圆角、各条高度、间距）故意不跟主题走，所以只允许出现在 tokens.css 的 :root 里。
var themeSizeTokens = []string{
	"--radius", "--topbar-h", "--toolbar-h", "--statusbar-h",
	"--sidebar-w", "--node-h", "--node-item-h", "--gap", "--pad",
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// 测试的工作目录是包目录：internal/config → 往上两级就是仓库根。
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func readSource(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("读不到 %s：%v", path, err)
	}
	return string(data)
}

// matchLines 按整行形状取捕获组：形状对不上就一条都抓不到，抓不到会在这里立刻红，
// 而不是让后面的相等断言拿两个空集合判成「通过」。
func matchLines(t *testing.T, src, expr, what string) []string {
	t.Helper()
	re := regexp.MustCompile(expr)
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if m := re.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			out = append(out, m[1])
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s：一行都没匹配到，说明那份清单的整行形状变了（正则：%s）", what, expr)
	}
	return out
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func equalSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// cssRule 是一条「选择器 { 声明块 }」。这里只处理主题文件那种没有嵌套花括号的写法。
type cssRule struct {
	selector string
	body     string
}

func cssRules(t *testing.T, path, src string) []cssRule {
	t.Helper()
	cleaned := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(src, "")
	var out []cssRule
	for _, m := range regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`).FindAllStringSubmatch(cleaned, -1) {
		out = append(out, cssRule{selector: strings.TrimSpace(m[1]), body: m[2]})
	}
	if len(out) == 0 {
		t.Fatalf("%s：一条 CSS 规则都没解析出来，说明文件结构变了", path)
	}
	return out
}

var dataThemeID = regexp.MustCompile(`\[data-theme='([a-z-]+)'\]`)
var cssDecl = regexp.MustCompile(`(--[a-z0-9-]+)\s*:`)

// themeTokens 返回「主题 id → 该主题覆盖的令牌名」。合并在一条选择器列表里的规则
// （比如五个主题共用的一批条带变量）算进每个被点名的主题。
func themeTokens(t *testing.T, path, src string) map[string]map[string]bool {
	t.Helper()
	out := map[string]map[string]bool{}
	for _, rule := range cssRules(t, path, src) {
		ids := dataThemeID.FindAllStringSubmatch(rule.selector, -1)
		if len(ids) == 0 {
			continue
		}
		for _, decl := range cssDecl.FindAllStringSubmatch(rule.body, -1) {
			for _, id := range ids {
				if out[id[1]] == nil {
					out[id[1]] = map[string]bool{}
				}
				out[id[1]][decl[1]] = true
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s：没有任何 html[data-theme=...] 规则，主题文件等于没生效", path)
	}
	return out
}

func tokenNames(set map[string]bool) []string {
	return sortedKeys(mapKeys(set))
}

func sortedKeys(in []string) []string {
	sort.Strings(in)
	return in
}

func allowedNonLightThemes() []string {
	out := make([]string, 0, len(allowedThemes)-1)
	for id := range allowedThemes {
		if id != "light" {
			out = append(out, id)
		}
	}
	return uniqueSorted(out)
}

// 门一：前端主题清单的 id 必须和后端允许表逐个相等。
func TestFrontendThemeListEqualsBackendAllowedThemes(t *testing.T) {
	src := readSource(t, repoRoot(t), "frontend/src/features/theme/themes.ts")
	lines := matchLines(t, src, `^\s*\{ id: '([a-z-]+)', swatches: \[`, "前端主题清单")
	got := uniqueSorted(lines)
	want := uniqueSorted(mapKeys(allowedThemes))
	if !equalSets(got, want) {
		t.Errorf("主题清单和后端允许表不是同一批：前端 %v，后端 %v", got, want)
	}
	if len(lines) != len(want) {
		t.Errorf("前端主题清单的条数和允许表对不上（多出一套或有重复 id）：应当 %d 条，实际 %d 条", len(want), len(lines))
	}
}

// 门二：主题令牌文件必须正好覆盖「允许表去掉晨光白」，一套不多一套不少。
// 晨光白不用写在 themes.css：它就是 tokens.css 的 :root 那一份，原型自己也没给它任何覆盖。
func TestThemesCSSCoversExactlyTheNonLightThemes(t *testing.T) {
	themes := themeTokens(t, "frontend/src/styles/themes.css", readSource(t, repoRoot(t), "frontend/src/styles/themes.css"))
	if !equalSets(themeIDs(themes), allowedNonLightThemes()) {
		t.Errorf("themes.css 覆盖的主题和后端允许表（去掉 light）不是同一批：文件里 %v，应当 %v", themeIDs(themes), allowedNonLightThemes())
	}
}

func themeIDs(themes map[string]map[string]bool) []string {
	return uniqueSorted(mapKeys(themes))
}

// 门三：每个非浅色主题都要覆盖同一批令牌，一个不能少。
// 少一个的后果是那块区域在这个主题里露出浅色底；漏 --on-primary 会让主色按钮上的字看不见。
func TestEveryThemeOverridesTheSameTokenSet(t *testing.T) {
	root := repoRoot(t)
	themes := themeTokens(t, "frontend/src/styles/themes.css", readSource(t, root, "frontend/src/styles/themes.css"))
	want := rootColorTokens(t, readSource(t, root, "frontend/src/styles/tokens.css"))

	for _, id := range themeIDs(themes) {
		got := tokenNames(themes[id])
		if !equalSets(got, want) {
			t.Errorf("主题 %s 的令牌和基准不齐：缺 %v，多 %v", id, diffSets(want, got), diffSets(got, want))
		}
	}
	for _, sizeToken := range themeSizeTokens {
		for _, id := range themeIDs(themes) {
			if themes[id][sizeToken] {
				t.Errorf("主题 %s 覆盖了尺寸令牌 %s：尺寸不跟主题走，只该写在 tokens.css", id, sizeToken)
			}
		}
	}
}

// rootColorTokens 从 tokens.css 的 :root 块里取出全部自定义属性，再剔掉尺寸类，剩下就是每套主题都要覆盖的那批。
func rootColorTokens(t *testing.T, src string) []string {
	t.Helper()
	var body string
	for _, rule := range cssRules(t, "frontend/src/styles/tokens.css", src) {
		if rule.selector == ":root" {
			body = rule.body
		}
	}
	if body == "" {
		t.Fatalf("tokens.css 里没有 :root 规则，基准令牌无从取起")
	}
	out := make([]string, 0, 32)
	for _, decl := range cssDecl.FindAllStringSubmatch(body, -1) {
		if !contains(themeSizeTokens, decl[1]) {
			out = append(out, decl[1])
		}
	}
	return uniqueSorted(out)
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func diffSets(a, b []string) []string {
	var out []string
	for _, s := range a {
		if !contains(b, s) {
			out = append(out, s)
		}
	}
	return out
}

// 门四：文案里每个允许主题都要有名字和说明；也不许多出后端不认的主题，
// 否则那份文案永远用不上，还会掩盖清单漂移。
func TestThemeNamesAndDescriptionsExistForEveryAllowedTheme(t *testing.T) {
	bundle := readSource(t, repoRoot(t), "frontend/src/i18n/messages/zh-CN.json")
	want := uniqueSorted(mapKeys(allowedThemes))
	for _, kind := range []string{"name", "desc"} {
		got := uniqueSorted(matchLines(t, bundle, `^\s*"theme\.([a-z-]+)\.`+kind+`":`, "文案里的主题"+kind))
		if !equalSets(got, want) {
			t.Errorf("文案的主题 %s 和后端允许表不是同一批：文案 %v，后端 %v", kind, got, want)
		}
	}
}

// 门五：缩放预设必须等于后端的四个允许倍数，且每档的数值和文字同口径
// （写了 110 却显示 100% 这种，点下去就是骗人）。
func TestFrontendZoomPresetsEqualsBackendAllowedZooms(t *testing.T) {
	src := readSource(t, repoRoot(t), "frontend/src/features/settings/ZoomPane.tsx")
	lines := matchLines(t, src, `^\s*\{ value: (\d+), label: '(\d+)%' \},?$`, "前端缩放预设")

	got := uniqueSorted(lines)
	want := make([]string, 0, len(allowedZooms))
	for z := range allowedZooms {
		want = append(want, strconv.Itoa(int(roundZoom(z)*100)))
	}
	want = uniqueSorted(want)
	if !equalSets(got, want) {
		t.Errorf("缩放预设和后端允许表不是同一批：前端 %v，后端 %v", got, want)
	}
	for _, line := range lines {
		percent, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("预设数值 %q 不是整数：%v", line, err)
		}
		if !allowedZooms[roundZoom(float64(percent)/100)] {
			t.Errorf("%d%% 折成倍数后不在后端允许表里：后端会直接判配置非法", percent)
		}
	}
	// 数值和文字必须同口径：写了 110 却显示 100%，点下去就是骗人。
	mismatch := 0
	for _, m := range regexp.MustCompile(`(?m)^\s*\{ value: (\d+), label: '(\d+)%' \},?$`).FindAllStringSubmatch(src, -1) {
		if m[1] != m[2] {
			t.Errorf("预设的数值和文字对不上：value=%s 但 label=%s%%", m[1], m[2])
		}
		mismatch++
	}
	if mismatch != len(lines) {
		t.Errorf("缩放预设的整行形状有两套：按 value+label 抓到 %d 条，按 value 抓到 %d 条", mismatch, len(lines))
	}
}
