// Package identity 是产品身份在本仓的唯一手写来源。
//
// 名字分三层，改法完全不同：
//
//	🟢 脸（随时可换，可改）：
//		DisplayName、Description。
//		改了只影响窗口标题、托盘悬浮和安装列表里那行字，不碰身份。
//
//	🟡 户口（默认冻结，拍板才能改）：
//		AppID、Slug、DataDir、HostsMarkerID。
//		这些值已经写在用户机器上——配置目录叫它、单例锁的名叫它、
//		已装包和已固定图标的名也叫它。改前必须过 internal/identity/identity_test.go
//		那把金标准，并在提交说明里写清迁移影响与回退方式。
//		落盘路径由 AppID / Slug 派生，不是独立事实；不要手改派生常量。
//
//	🔴 骨头（不动，不可改）：
//		go.mod 的模块路径。它决定 frontend/bindings 的目录形状，
//		改了只有全仓重写，没有收益。
//
// 🔴 用法（契约，不可改）：
//
//	Go 侧直接引这些常量；构建配置和 shell 脚本用 `go run ./cmd/identity --shell`
//	取一次值。别在别处再抄一份字面量——`4475f5b` 那次改身份扫了 7 个文件，
//	就是抄写的代价。
package identity

const (
	// 🟢 可改：脸层，随时可换，只影响显示。
	// DisplayName 是给人看的自称。
	DisplayName = "GitHub Net"
	// 🟢 可改：脸层，随时可换，只影响显示。
	// Description 是应用描述，出现在安装元数据里。
	Description = "GitHub 和 Steam 网络加速工具"

	// 🟡 拍板才能改：户口层，默认冻结。改前过金标准并写明迁移影响。
	// AppID 是稳定产品 ID：GTK ApplicationID、单例锁名（它加 `.SingleInstance`）、
	// Linux 桌面项文件名和 StartupWMClass 都由它来。
	AppID = "io.github.xiaokentrl.githubnet"
	// 🟡 拍板才能改：户口层，默认冻结。改前过金标准并写明迁移影响；
	// 若动它，还要同步 root 助手里硬编码的 PKG，第二道金标准会反向核对。
	// Slug 是资产名：二进制、包名、图标名和打包产物用它。
	Slug = "github-net"
	// 🟡 拍板才能改：户口层，默认冻结。改前过金标准并写明迁移影响。
	// DataDir 是用户数据目录名：`~/.local/share/<DataDir>/config/config.json`，
	// 缓存和配置目录同样用它一个名。改了等于让用户已存的设置读不到。
	DataDir = "github-net"
	// 🟡 拍板才能改，且是这里最危险的一个：户口层，默认冻结。
	// 改前过金标准、确认 LegacyHostsMarkers 仍认识旧标记，并写明迁移影响。
	// HostsMarkerID 是系统 Hosts 里管理区段标记中的那段名字。
	// 改了它，老区段会被读成「别人写的区段」，判成冲突，
	// 用户已经应用的映射就再也还原不掉。
	HostsMarkerID = "github-net"
)

// 🔴 不可手改：下面这些不是新事实，全部由 AppID 和 Slug 算出来；
// 列在这儿是为了让脚本和构建配置有唯一取法。
// 改源常量即可，手改会造成两份事实。
const (
	// 🔴 不可手改：派生自 Slug。
	// PackageName 是 deb/rpm 的包名。
	PackageName = Slug
	// 🔴 不可手改：派生自 Slug。
	// BinaryName 是安装后的可执行文件名，也是停进程时要匹配的名字。
	BinaryName = Slug
	// 🔴 不可手改：派生自 AppID。
	// DesktopID 是桌面项文件名（不含 `.desktop` 后缀）和 StartupWMClass 用的值。
	DesktopID = AppID
	// 🔴 不可手改：派生自 AppID。
	// DesktopFile 是构建生成的桌面项文件名。
	DesktopFile = AppID + ".desktop"
	// 🔴 不可手改：派生自 Slug。
	// DebPath 是打包产物的固定路径（root 助手只认这一个路径）。
	DebPath = "bin/" + Slug + ".deb"
	// 🔴 不可手改：派生自 Slug。
	// HelperPath 是免密 root 助手的安装位置。
	HelperPath = "/usr/local/sbin/" + Slug + "-root-install"
	// 🔴 不可手改：派生自 Slug。
	// SudoersFile 是免密规则文件。
	SudoersFile = "/etc/sudoers.d/" + Slug + "-reinstall"
	// 🔴 不可手改：派生自 Slug。
	// BinaryPath 是包内二进制的落盘位置。
	BinaryPath = "/usr/local/bin/" + Slug
	// 🔴 不可手改：派生自 Slug。
	// IconPath 是包内图标的落盘位置。
	IconPath = "/usr/share/icons/hicolor/128x128/apps/" + Slug + ".png"
	// 🔴 不可手改：派生自 AppID。
	// DesktopPath 是桌面项的落盘位置。
	DesktopPath = "/usr/share/applications/" + DesktopFile
)

// Entry 是一条「键名 + 值」，供 cmd/identity 输出。
type Entry struct {
	Name  string
	Value string
}

// 🔴 不可手改键名或顺序：除非同步 cmd/identity、脚本和测试。
// entries 是脚本和构建配置能取到的全部值，顺序固定，输出可逐行比对。
// 键名不带 `IDENTITY_` 前缀——前缀由 cmd/identity 统一加，脚本只 eval 一次。
func entries() []Entry {
	return []Entry{
		{"DISPLAY_NAME", DisplayName},
		{"DESCRIPTION", Description},
		{"APP_ID", AppID},
		{"SLUG", Slug},
		{"DATA_DIR", DataDir},
		{"HOSTS_MARKER_ID", HostsMarkerID},
		{"PACKAGE_NAME", PackageName},
		{"BINARY_NAME", BinaryName},
		{"BINARY_PATH", BinaryPath},
		{"DESKTOP_ID", DesktopID},
		{"DESKTOP_FILE", DesktopFile},
		{"DESKTOP_PATH", DesktopPath},
		{"ICON_PATH", IconPath},
		{"DEB_PATH", DebPath},
		{"HELPER_PATH", HelperPath},
		{"SUDOERS_FILE", SudoersFile},
	}
}

// 🔴 契约不可改：调用方依赖顺序与键名。
// Keys 返回全部键名（不带前缀），顺序与 entries 一致。
func Keys() []string {
	list := entries()
	out := make([]string, len(list))
	for i, e := range list {
		out[i] = e.Name
	}
	return out
}

// 🔴 契约不可改：返回副本，调用方不应依赖内部切片。
// Entries 返回键名和值的副本列表。
func Entries() []Entry {
	return entries()
}

// 🔴 契约不可改：键名不带 IDENTITY_ 前缀。
// Value 按键名取值；第二个返回值为假表示键名不存在。
func Value(name string) (string, bool) {
	for _, e := range entries() {
		if e.Name == name {
			return e.Value, true
		}
	}
	return "", false
}
