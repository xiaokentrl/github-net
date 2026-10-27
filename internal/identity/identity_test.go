// 金标准测试。
//
// 改这份文件里 want* 常量的值 ＝ 改用户机器上已经存在的东西：
// 配置目录名、单例锁名、已装的包名与桌面项、甚至系统 Hosts 里那段管理区段的标记。
// 动它们之前必须过评审，并在评审里写清三件事：
//
//	① 老用户在哪个时刻看到差异（升级时？下次启动时？还是永久两份并存？）；
//	② 老数据怎么迁（搬目录？双读多久？谁删旧入口？），写不出迁移路径就是不可改；
//	③ 迁移失败怎么回来。
//
// 这份测试故意把值写死在这儿，而不是从 internal/identity 读——
// 从源读值的话，改名时两边一起变，测试永远绿，等于没装门。
// 允许随手改的是 identity.DisplayName／Description 那一层（改了只影响显示），
// 对应的 wantDisplayName、wantDescription 跟着改不算「改户口」。
package identity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/hosts"
)

const (
	// 户口层：改动＝改用户机器上已存在的东西，必须先过上面那段评审。
	wantAppID         = "io.github.xiaokentrl.githubnet"
	wantSlug          = "github-net"
	wantDataDir       = "github-net"
	wantHostsMarkerID = "github-net"
	// 系统 Hosts 里那对标记的完整文本，写死在这儿防止「顺手改一下常量」。
	wantHostsBegin = "# BEGIN github-net managed hosts v1"
	wantHostsEnd   = "# END github-net managed hosts v1"
	// 免密 root 助手与规则文件的位置，sudoers 只放行前者，改路径要连规则一起改。
	wantHelperPath  = "/usr/local/sbin/github-net-root-install"
	wantSudoersFile = "/etc/sudoers.d/github-net-reinstall"
	wantDebPath     = "bin/github-net.deb"
	wantBinaryPath  = "/usr/local/bin/github-net"
	wantIconPath    = "/usr/share/icons/hicolor/128x128/apps/github-net.png"
	wantDesktopFile = "io.github.xiaokentrl.githubnet.desktop"

	// 脸层：换了只影响显示，随产品名改，不需要迁移。
	wantDisplayName = "GitHub Net"
	wantDescription = "GitHub 和 Steam 网络加速工具"
)

func TestGoldenValues(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"AppID", AppID, wantAppID},
		{"Slug", Slug, wantSlug},
		{"DataDir", DataDir, wantDataDir},
		{"HostsMarkerID", HostsMarkerID, wantHostsMarkerID},
		{"PackageName", PackageName, wantSlug},
		{"BinaryName", BinaryName, wantSlug},
		{"DesktopID", DesktopID, wantAppID},
		{"DesktopFile", DesktopFile, wantDesktopFile},
		{"DebPath", DebPath, wantDebPath},
		{"HelperPath", HelperPath, wantHelperPath},
		{"SudoersFile", SudoersFile, wantSudoersFile},
		{"BinaryPath", BinaryPath, wantBinaryPath},
		{"IconPath", IconPath, wantIconPath},
		{"DesktopPath", DesktopPath, "/usr/share/applications/" + wantDesktopFile},
		{"DisplayName", DisplayName, wantDisplayName},
		{"Description", Description, wantDescription},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s 实得 %q，金标准是 %q", c.name, c.got, c.want)
		}
	}
}

// 派生值必须真的派生自四个户口常量，否则等于又开了一处抄写。
func TestDerivedFromSlugs(t *testing.T) {
	if PackageName != Slug || BinaryName != Slug || DebPath != "bin/"+Slug+".deb" {
		t.Error("包名/二进制/产物路径没有派生自 Slug")
	}
	if DesktopID != AppID || DesktopFile != AppID+".desktop" {
		t.Error("桌面项没有派生自 AppID")
	}
	if HelperPath != "/usr/local/sbin/"+Slug+"-root-install" || SudoersFile != "/etc/sudoers.d/"+Slug+"-reinstall" {
		t.Error("root 助手与 sudoers 路径没有派生自 Slug")
	}
}

// 形状断言：这些值会被拼进路径、命令和系统文件标记，坏形状是事故不是笔误。
func TestShapes(t *testing.T) {
	if DisplayName == "" || strings.ContainsAny(DisplayName, `/"*?`) || strings.Contains(DisplayName, "..") {
		t.Errorf("DisplayName 不该带路径字符：%q", DisplayName)
	}
	if DataDir == "" || strings.ContainsAny(DataDir, `/*?`) || strings.Contains(DataDir, "/") ||
		DataDir == "." || DataDir == ".." || strings.HasPrefix(DataDir, ".") {
		t.Errorf("DataDir 必须是能安全拼进 $HOME 的单段目录名：%q", DataDir)
	}
	if Slug == "" || strings.ContainsAny(Slug, `/*? `) || DataDir == "." {
		t.Errorf("Slug 只能是小写字母、数字和连字符：%q", Slug)
	}
	if segs := strings.Split(AppID, "."); len(segs) < 3 {
		t.Errorf("AppID 至少三段点分（反向域名），实得 %q", AppID)
	}
	if !strings.HasSuffix(DebPath, ".deb") || !strings.HasPrefix(DebPath, "bin/") {
		t.Errorf("DebPath 应是仓库内的 bin/*.deb：%q", DebPath)
	}
	for _, p := range []string{HelperPath, SudoersFile, BinaryPath, IconPath, DesktopPath} {
		if !strings.HasPrefix(p, "/") {
			t.Errorf("落盘路径必须是绝对路径：%q", p)
		}
	}
}

// 输出键名不许带 IDENTITY_ 前缀（前归 cmd/identity 统一加），也不许重名。
func TestKeyNamesAreUniqueAndUnprefixed(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range Keys() {
		if strings.HasPrefix(k, "IDENTITY_") {
			t.Errorf("键名 %s 不该自带前缀", k)
		}
		if seen[k] {
			t.Errorf("键名重复：%s", k)
		}
		seen[k] = true
		if v, ok := Value(k); !ok || v == "" {
			t.Errorf("键 %s 取不到值", k)
		}
	}
	if len(Keys()) != 16 {
		t.Errorf("键的数量变了（%d）：脚本取值点也要跟着核对", len(Keys()))
	}
	if _, ok := Value("不存在"); ok {
		t.Error("未知键名不该取到值")
	}
}

// 下面这组是「接线还没做完」的看门狗，分两头：
//   - 还没改成取值的调用点（Go 里的区段标记、数据目录、应用 ID，还有免密 root 助手）：
//     它们手上那份字面量必须和金标准一字不差，漂了就红。
//   - 已经改成取值的脚本：它不许再出现写死的户口路径，否则等于又开一处抄写。
//
// 等所有调用点都取值，第一类断言可以删；第二类要一直留着。
func TestNotDuplicatedOutsideIdentity(t *testing.T) {
	root := repoRoot(t)

	t.Run("区段标记", func(t *testing.T) {
		// 直接和 hosts 包对外给出的标记原文比：这是会写进用户系统 Hosts 的那一行字。
		if got := hosts.MarkerBegin(); got != wantHostsBegin {
			t.Errorf("Hosts 起始标记实得 %q，金标准是 %q", got, wantHostsBegin)
		}
		if got := hosts.MarkerEnd(); got != wantHostsEnd {
			t.Errorf("Hosts 结束标记实得 %q，金标准是 %q", got, wantHostsEnd)
		}
		src := readFile(t, filepath.Join(root, "internal/hosts/parser.go"))
		if !strings.Contains(src, HostsMarkerID+" managed hosts v") {
			t.Errorf("parser.go 里的标记模板和 identity.HostsMarkerID=%q 拼不上了", HostsMarkerID)
		}
	})

	t.Run("config.go 的数据目录名", func(t *testing.T) {
		src := readFile(t, filepath.Join(root, "internal/config/config.go"))
		if !strings.Contains(src, `"`+DataDir+`"`) {
			t.Errorf("config.go 里的数据目录名和 identity.DataDir=%q 不一致", DataDir)
		}
	})

	t.Run("main.go 的显示名与应用 ID", func(t *testing.T) {
		src := readFile(t, filepath.Join(root, "main.go"))
		if !strings.Contains(src, `appID      = "`+AppID+`"`) {
			t.Error("main.go 的 appID 和 identity.AppID 不一致：两边正在各写一份")
		}
		if !strings.Contains(src, `appName    = "`+DisplayName+`"`) {
			t.Error("main.go 的 appName 和 identity.DisplayName 不一致")
		}
	})

	t.Run("免密 root 助手保留字面量但必须自证安全", func(t *testing.T) {
		src := stripShellComments(readFile(t, filepath.Join(root, "scripts/github-net-root-install.sh")))
		// 逐字比对（不是「包含」）：`github-net2` 这种加后缀的改动若用 Contains 会照样绿。
		// PKG 是文件里唯一写包名的地方，卸载命令只许引用它——这两条合起来才是「只抄一份」。
		wantExact := []struct {
			what string
			re   *regexp.Regexp
			want string
		}{
			{"PKG 那一行", regexp.MustCompile(`(?m)^PKG="([^"]*)"$`), Slug},
			{"DEB 那一行", regexp.MustCompile(`(?m)^DEB="([^"]*)"$`), `/home/$USER_NAME/$PKG/bin/$PKG.deb`},
			{"卸载命令", regexp.MustCompile(`(?m)^(dpkg --purge "\$PKG" 2>/dev/null \|\| true)$`), `dpkg --purge "$PKG" 2>/dev/null || true`},
		}
		for _, c := range wantExact {
			got := c.re.FindStringSubmatch(src)
			if got == nil {
				t.Errorf("root 助手里找不到 %s 的预期写法（形状变了，第二道金标准要一起更新）", c.what)
				continue
			}
			if got[1] != c.want {
				t.Errorf("root 助手的 %s 实得 %q，identity 这边是 %q", c.what, got[1], c.want)
			}
		}
		// 这三条是安全边界，不是风格问题：免密 root 不许读或跑用户可写的仓库内容。
		// 注释里提到这些词是允许的（文件开头就在解释这个例外），所以只查真正会执行的行。
		for _, forbidden := range []string{"identity-guard.sh", "go run", `. "$`, "source "} {
			if strings.Contains(src, forbidden) {
				t.Errorf("root 助手引用了仓库里的东西（%q）：免密 sudo 会让 root 执行用户可写的代码", forbidden)
			}
		}
	})

	t.Run("已接线的脚本不许再写死户口路径", func(t *testing.T) {
		for _, name := range []string{"scripts/pre-push-reinstall.sh", "scripts/setup-passwordless-reinstall.sh"} {
			src := stripShellComments(readFile(t, filepath.Join(root, name)))
			if !strings.Contains(src, "identity_load") {
				t.Errorf("%s 没有通过 identity 取值", name)
			}
			for _, forbidden := range []string{wantHelperPath, wantSudoersFile, wantDebPath, "dpkg --purge " + Slug} {
				if strings.Contains(src, forbidden) {
					t.Errorf("%s 里又写死了 %q，改成读 IDENTITY_* 变量", name, forbidden)
				}
			}
		}
		if src := readFile(t, filepath.Join(root, "scripts/pre-push-reinstall.sh")); !strings.Contains(src, "identity_guard") {
			t.Error("pre-push 没有在删除、卸载之前调 identity_guard")
		}
	})
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// 测试的工作目录是包目录：internal/identity → 往上两级就是仓库根。
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到 %s：%v", path, err)
	}
	return string(data)
}

// stripShellComments 去掉整行注释，只留下会执行的行。
// 注释里出现某个路径名是解释，不是抄写；判断「有没有第二份字面量」只看代码行。
func stripShellComments(src string) string {
	var out []string
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
