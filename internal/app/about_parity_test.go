package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 这一组测试是「关于我们」这一屏的防漂移门：界面上那三个外链和许可证名字
// 看着像文案，其实是事实——仓库搬走、许可证换了，页面还写着旧的就是在骗人。
// 判据一律用整行形状抓出来再做相等比较，不用「包含」：
// 包含会被「在后面多加一段」骗过去，相等不会。

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// 测试的工作目录是包目录：internal/app → 往上两级就是仓库根。
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func readAt(t *testing.T, root, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		t.Fatalf("读不到 %s：%v", path, err)
	}
	return string(data)
}

// aboutConst 按整行形状抓 `export const 名字 = '值'`。
// 形状对不上就一条都抓不到，这里立刻红，而不是让后面的相等断言拿空字符串判成通过。
func aboutConst(t *testing.T, src, name string) string {
	t.Helper()
	re := regexp.MustCompile(`^export const ` + name + ` = '([^']+)'$`)
	var found []string
	for _, line := range strings.Split(src, "\n") {
		if m := re.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			found = append(found, m[1])
		}
	}
	if len(found) != 1 {
		t.Fatalf("前端常量 %s 应当正好一行，实际 %d 行", name, len(found))
	}
	return found[0]
}

// modulePath 从 go.mod 的 module 行取值。这是仓库地址在仓库里的唯一权威写法。
func modulePath(t *testing.T, root string) string {
	t.Helper()
	src := readAt(t, root, "go.mod")
	re := regexp.MustCompile(`^module (\S+)$`)
	var found []string
	for _, line := range strings.Split(src, "\n") {
		if m := re.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil {
			found = append(found, m[1])
		}
	}
	if len(found) != 1 {
		t.Fatalf("go.mod 的 module 行应当正好一行，实际 %d 行", len(found))
	}
	return found[0]
}

func TestAboutHomepageEqualsModulePath(t *testing.T) {
	root := repoRoot(t)
	home := aboutConst(t, readAt(t, root, "frontend/src/features/about/aboutInfo.ts"), "REPO_HOME_URL")
	// go.mod 的模块路径就是 `<主机>/<属主>/<仓库>`，它一动这一条就红，
	// 逼着前端那一份跟着改；反过来只想改前端、不改模块路径也一样红。
	if want := "https://" + modulePath(t, root); home != want {
		t.Errorf("关于页首页地址 = %q，期望 %q（由 go.mod 的 module 路径派生）", home, want)
	}
}

func TestAboutLinksAreExactChildrenOfHomepage(t *testing.T) {
	root := repoRoot(t)
	src := readAt(t, root, "frontend/src/features/about/aboutInfo.ts")
	home := aboutConst(t, src, "REPO_HOME_URL")
	issues := aboutConst(t, src, "REPO_ISSUES_URL")
	license := aboutConst(t, src, "REPO_LICENSE_URL")
	if issues != home+"/issues/new" {
		t.Errorf("反馈问题地址 = %q，期望 %q", issues, home+"/issues/new")
	}
	if license != home+"/blob/main/LICENSE" {
		t.Errorf("许可协议地址 = %q，期望 %q", license, home+"/blob/main/LICENSE")
	}
}

func TestAboutLicenseNameComesFromLicenseFile(t *testing.T) {
	root := repoRoot(t)
	name := aboutConst(t, readAt(t, root, "frontend/src/features/about/aboutInfo.ts"), "LICENSE_NAME")
	firstLine := strings.TrimRight(readAt(t, root, "LICENSE"), "\r\n")
	if idx := strings.IndexByte(firstLine, '\n'); idx >= 0 {
		firstLine = firstLine[:idx]
	}
	// LICENSE 第一行是许可证全名（例如 `MIT License`），界面上只报名字。
	// 相等或「全名 = 短名 + 一个空格 + 尾巴」都算对；别的形状说明许可证换过了。
	if firstLine != name && !strings.HasPrefix(firstLine, name+" ") {
		t.Errorf("界面许可证名 %q 和 LICENSE 第一行 %q 对不上", name, firstLine)
	}
}

func TestReadmeUsesTheSameRepositoryURL(t *testing.T) {
	root := repoRoot(t)
	home := aboutConst(t, readAt(t, root, "frontend/src/features/about/aboutInfo.ts"), "REPO_HOME_URL")
	readme := readAt(t, root, "README.md")
	if !strings.Contains(readme, home) {
		t.Errorf("README 里找不到关于页用的仓库地址 %q，两边已经各说各话", home)
	}
}

// brandGradient 抓出一个 CSS 文件里唯一那条 linear-gradient 的值，并把连续空白压成一个空格。
// 「唯一」是判据的一部分：文件里出现两条，或者一条都没有，都直接判红，
// 不靠包含比较——加一条渐变还想蒙过去，正是这条门要挡的事。
func brandGradient(t *testing.T, src, path string) string {
	t.Helper()
	re := regexp.MustCompile(`linear-gradient\([^)]*\)`)
	found := re.FindAllString(src, -1)
	if len(found) != 1 {
		t.Fatalf("%s 里的渐变应当正好一条，实际 %d 条", path, len(found))
	}
	return strings.Join(strings.Fields(found[0]), " ")
}

// 品牌标识「GH」出现在两个地方：顶栏的小块和关于屏的大块。
// 原型第 25 行与第 471 行本来就是同一对色值，六套主题也都不覆盖它，所以这里不引令牌
// （引进 tokens.css 的 :root 会逼着五套主题各写一份覆盖，反而把「品牌不跟主题走」做丢了）。
// 两处必须逐字一致，谁改了忘另一处，这一条就红。
func TestBrandGradientMatchesTopbarLogo(t *testing.T) {
	root := repoRoot(t)
	topbar := brandGradient(t, readAt(t, root, "frontend/src/components/layout/AppShell.module.css"), "AppShell.module.css")
	about := brandGradient(t, readAt(t, root, "frontend/src/features/settings/SettingsDialog.module.css"), "SettingsDialog.module.css")
	if topbar != about {
		t.Errorf("关于屏的品牌渐变 = %q，和顶栏的 %q 不一样", about, topbar)
	}
}
