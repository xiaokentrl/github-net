// Package config 是用户设置的唯一持有者：读盘、校验、按字段 patch 保存和原子替换都在这里。
// 文件形状就是规划里那份带 schemaVersion/revision 的 JSON，不另存第二份。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// SchemaVersion 是当前唯一支持的配置结构版本。
const SchemaVersion = 1

// 网络档位取值；规划要求「首次安装或配置无效时使用省流档」。
const (
	ProfileLow      = "low"
	ProfileStandard = "standard"
	ProfileFull     = "full"
)

// 下面的边界取自唯一正式原型：档位每 hostname 候选上限 2/8/16、
// 缩放只有 90%/100%/110%/125% 四档、侧栏宽度 200–600、队列高度最小 120。
//
// 候选预算这两个数不是原型那对 108，而是按目录实数算出来的（原型抄的是它自己那份估算）：
// 目录去重后 63 个主机名，所以一轮全量理论需要 63×2=126（省流）、63×8=504（标准）、63×16=1008（完整）。
// 默认取 126，意思是「默认档位默认就能盖住一轮全量」，不让第一次使用就撞上「覆盖不全要确认」；
// 可填上限取 1008，意思是「完整档一轮全量也允许开得满」。这两个数和目录规模、档位数字绑在一起，
// 目录或档位改了，internal/app 的预算门会先红，见 budget_parity_test.go。
const (
	MaxCandidateProbesDefault = 126
	MaxCandidateProbesLimit   = 1008
	MinCandidateProbes        = 1
	SidebarWidthMin           = 200
	SidebarWidthMax           = 600
	// QueueHeightMin 取 45（拍板 2026-11-02）：队列收起时那一条也常驻可拖，拖的就是同一个
	// layout.queueHeight，所以「最薄能拖到哪」必须容纳收起态那一行（队列头 44px + 1px 下边框）。
	// 展开态想拖到 45 也由用户自己决定，界面不再另设第二道下限。
	QueueHeightMin = 45
	QueueHeightMax = 2000
	// QueueHeightSmall 是队列高度「小」档的值，也是出厂默认（拍板 2026-10-31）；
	// 前端 LayoutPane 的三档预设里「小」必须同值，相等由 layout_bounds_parity_test 钉住。
	QueueHeightSmall = 180
	// 任务列表列宽：下限 150 是「轮名加计数还看得清」的实测值，上限 1200 只是存储兜底，
	// 界面上真正的上限跟着队列区宽度走（最多占 45%，右边明细必须留过半），两边同值由门钉住。
	QueueListWidthMin = 150
	QueueListWidthMax = 1200
)

var (
	// ErrInvalid 表示某个字段不在允许值里；上层按 invalid_settings 回给前端。
	ErrInvalid = errors.New("设置内容不合法")
	// ErrRevisionConflict 表示这次提交基于旧副本，磁盘已经有新版本，必须刷新后重做。
	ErrRevisionConflict = errors.New("配置版本冲突")
	// ErrEmptyMutationID 表示保存请求没带幂等键。
	ErrEmptyMutationID = errors.New("保存请求缺少 mutationID")
	// ErrSchemaTooNew 表示磁盘配置来自更新的程序版本，旧版本拒绝读取改写。
	ErrSchemaTooNew = errors.New("配置结构版本高于当前程序")
	// ErrSaveFailed 表示写盘失败；revision 不变，前端不能显示成已保存。
	ErrSaveFailed = errors.New("配置写入失败")
)

var (
	allowedProfiles  = map[string]bool{ProfileLow: true, ProfileStandard: true, ProfileFull: true}
	allowedThemes    = map[string]bool{"light": true, "midnight": true, "oled": true, "forest": true, "ocean": true, "sakura": true, "warm-yellow": true}
	allowedZooms     = map[float64]bool{0.9: true, 1: true, 1.1: true, 1.25: true}
	allowedLanguages = map[string]bool{"zh-CN": true, "zh-TW": true}
)

// NetworkConfig 是网络与流量设置。
type NetworkConfig struct {
	Profile                        string `json:"profile"`
	MaxCandidateProbesPerRun       int    `json:"maxCandidateProbesPerRun"`
	ConfirmUpdateDownloadOnMetered bool   `json:"confirmUpdateDownloadOnMetered"`
}

// AcceleratorConfig 按稳定 scopeID 保存默认方法；方法注册表到位前只能是空。
type AcceleratorConfig struct {
	DefaultStrategyByScope map[string]string `json:"defaultStrategyByScope"`
}

// AppearanceConfig 是语言、主题和缩放。
type AppearanceConfig struct {
	Language string  `json:"language"`
	Theme    string  `json:"theme"`
	Zoom     float64 `json:"zoom"`
}

// LayoutConfig 是用户可调整的三个布局尺寸，单位像素。
type LayoutConfig struct {
	SidebarWidth int `json:"sidebarWidth"`
	QueueHeight  int `json:"queueHeight"`
	// QueueListWidth 是队列里左栏「任务列表」的宽度；右边任务明细占剩下的全部。
	QueueListWidth int `json:"queueListWidth"`
	// ToastDisabled 关掉右下角提示面板和它收起后的那颗「提示」胶囊。
	// 出厂默认关掉（设置里那颗开关默认不勾选）。字段仍叫「禁用」是为了不改磁盘格式：
	// 老配置没写过这个字段时，read() 按出厂默认补成 true；显式写过 false 的按用户选择保留。
	ToastDisabled bool `json:"toastDisabled"`
	// QueueHeightLocked 锁住任务队列高度：锁上后拖动条、窗口自愈、布局预设都不再改 queueHeight，
	// 磁盘上那份就停在锁定时的高度，重启自然恢复。零值＝不锁，老配置缺字段也按不锁走。
	QueueHeightLocked bool `json:"queueHeightLocked"`
}

// TreeBehaviorConfig 是两个独立的分类树展开开关，加上树侧勾选的持久化。
type TreeBehaviorConfig struct {
	DetectFocusFollow bool `json:"detectFocusFollow"`
	ApplyFocusFollow  bool `json:"applyFocusFollow"`
	// CheckedItemIDs 是树侧勾选的项目 ID（拍板 2026-11-03：勾选跨重装有效）。
	// 只做长度兜底不校验 ID 存在性：目录改版后前端会把不认识的 ID 过滤掉，
	// 配置层不认识目录，硬校验只会让「目录刚删了项」的合法老配置整份被拒。
	CheckedItemIDs []string `json:"checkedItemIds"`
}

// StartupConfig 是启动相关开关。自动检测→自动应用→自动退出是一条链，下游依赖上游：
// 清单为空就不检测，没检测就谈不上应用，没应用就谈不上退出。
// CloseToTray 只管「人自己点关闭」那条路；自动退出是无人值守跑完就走，直接结束进程，不看这个开关。
type StartupConfig struct {
	AutoStart          bool     `json:"autoStart"`
	StartMinimized     bool     `json:"startMinimized"`
	CloseToTray        bool     `json:"closeToTray"`
	AutoDetectMenus    []string `json:"autoDetectMenus"`
	AutoApplyOnStartup bool     `json:"autoApplyOnStartup"`
	// AutoCloseAfterApply 是「自动应用那一轮跑完后真退出」：结束整个进程，
	// 不是收进托盘，也不是只关窗口。
	AutoCloseAfterApply bool `json:"autoCloseAfterApply"`
}

// Config 是完整配置快照，也是磁盘上的文件内容。
type Config struct {
	SchemaVersion int                `json:"schemaVersion"`
	Revision      int64              `json:"revision"`
	Network       NetworkConfig      `json:"network"`
	Accelerator   AcceleratorConfig  `json:"accelerator"`
	Appearance    AppearanceConfig   `json:"appearance"`
	Layout        LayoutConfig       `json:"layout"`
	TreeBehavior  TreeBehaviorConfig `json:"treeBehavior"`
	Startup       StartupConfig      `json:"startup"`
}

// Patch 是字段级保存请求：没带的字段保持原值，所以一次只改档位的保存
// 不会覆盖并发改过的其它设置。用指针区分「没提交」和「提交了零值」。
type Patch struct {
	NetworkProfile                 *string            `json:"networkProfile"`
	MaxCandidateProbesPerRun       *int               `json:"maxCandidateProbesPerRun"`
	ConfirmUpdateDownloadOnMetered *bool              `json:"confirmUpdateDownloadOnMetered"`
	DefaultStrategyByScope         *map[string]string `json:"defaultStrategyByScope"`
	Language                       *string            `json:"language"`
	Theme                          *string            `json:"theme"`
	Zoom                           *float64           `json:"zoom"`
	SidebarWidth                   *int               `json:"sidebarWidth"`
	QueueHeight                    *int               `json:"queueHeight"`
	QueueHeightLocked              *bool              `json:"queueHeightLocked"`
	QueueListWidth                 *int               `json:"queueListWidth"`
	ToastDisabled                  *bool              `json:"toastDisabled"`
	DetectFocusFollow              *bool              `json:"detectFocusFollow"`
	ApplyFocusFollow               *bool              `json:"applyFocusFollow"`
	CheckedItemIDs                 *[]string          `json:"checkedItemIds"`
	AutoStart                      *bool              `json:"autoStart"`
	StartMinimized                 *bool              `json:"startMinimized"`
	CloseToTray                    *bool              `json:"closeToTray"`
	AutoDetectMenus                *[]string          `json:"autoDetectMenus"`
	AutoApplyOnStartup             *bool              `json:"autoApplyOnStartup"`
	AutoCloseAfterApply            *bool              `json:"autoCloseAfterApply"`
}

// Default 返回代码里定义的默认值；revision 为 0 表示「还没成功保存过」。
func Default() Config {
	return Config{
		SchemaVersion: SchemaVersion,
		Network: NetworkConfig{
			Profile:                        ProfileLow,
			MaxCandidateProbesPerRun:       MaxCandidateProbesDefault,
			ConfirmUpdateDownloadOnMetered: true,
		},
		Accelerator: AcceleratorConfig{DefaultStrategyByScope: map[string]string{}},
		Appearance:  AppearanceConfig{Language: "zh-CN", Theme: "light", Zoom: 1},
		// 队列高度默认取「小」档（拍板 2026-10-31）：主区先让给项目树，要看队列再拖大。
		// 提示面板出厂默认关掉（true＝被禁用）；队列高度出厂不锁，零值就是默认值。
		Layout: LayoutConfig{SidebarWidth: 240, QueueHeight: QueueHeightSmall, QueueListWidth: 220, ToastDisabled: true, QueueHeightLocked: false},
		// 两个焦点跟随出厂默认打开（拍板 2026-10-31）：队列跟着正在跑的项目走是这功能的本意。
		TreeBehavior: TreeBehaviorConfig{DetectFocusFollow: true, ApplyFocusFollow: true},
	}
}

// Path 给出应用专属配置文件路径；各平台位置已由规划固定，不写仓库目录也不散落主目录。
func Path() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("%w: 拿不到 LOCALAPPDATA", ErrSaveFailed)
		}
		return filepath.Join(base, "GitHubNet", "config", "config.json"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "GitHubNet", "config", "config.json"), nil
	default:
		// Linux 按惯例放配置目录（拍板 2026-10-31）：XDG_CONFIG_HOME 优先，默认 ~/.config/github-net/config.json。
		// 开发期早先的版本把它放在数据目录，由 MigrateLegacy 搬过来（不写具体版本号：编号回退过一次）。
		if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
			return filepath.Join(dir, "github-net", "config.json"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "github-net", "config.json"), nil
	}
}

// legacyPath 是开发期早先版本的配置文件旧位置（数据目录），只有 Linux 动过，另两个平台没有旧档。
func legacyPath() (string, bool) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return "", false
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return filepath.Join(dir, "github-net", "config", "config.json"), true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	return filepath.Join(home, ".local", "share", "github-net", "config", "config.json"), true
}

// MigrateLegacy 把旧数据目录里的配置搬一次家：新位置还没有而旧位置有，
// 就整体复制过去（原件保留当备份，不做删除）。搬不动不算致命——NewStore 会用默认值开新档。
func MigrateLegacy(newPath string) {
	if _, err := os.Stat(newPath); err == nil {
		return
	}
	old, ok := legacyPath()
	if !ok {
		return
	}
	raw, err := os.ReadFile(old)
	if err != nil || len(raw) == 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(newPath, raw, 0o600)
}

// Validate 检查全部字段。scopeIDs 是产品目录里的合法加速范围，
// strategies 是已注册且适用于该范围的方法，menuIDs 是产品目录里的主菜单 ID；
// 三个集合都可以为空，空集合意味着「出现这类值就一律拒绝」，不静默放行。
func (c Config) Validate(scopeIDs map[string]bool, strategies map[string]bool, menuIDs map[string]bool) error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: 配置结构版本 %d 不是当前支持的 %d", ErrInvalid, c.SchemaVersion, SchemaVersion)
	}
	if !allowedProfiles[c.Network.Profile] {
		return fmt.Errorf("%w: 网络档位只能是省流、标准或完整检测", ErrInvalid)
	}
	if c.Network.MaxCandidateProbesPerRun < MinCandidateProbes || c.Network.MaxCandidateProbesPerRun > MaxCandidateProbesLimit {
		return fmt.Errorf("%w: 每轮候选探测总上限要在 %d 到 %d 之间", ErrInvalid, MinCandidateProbes, MaxCandidateProbesLimit)
	}
	if !allowedLanguages[c.Appearance.Language] {
		return fmt.Errorf("%w: 语言 %s 不在支持列表（简体中文/繁体中文）", ErrInvalid, c.Appearance.Language)
	}
	if !allowedThemes[c.Appearance.Theme] {
		return fmt.Errorf("%w: 主题 %s 不在支持的七套里", ErrInvalid, c.Appearance.Theme)
	}
	if !allowedZooms[roundZoom(c.Appearance.Zoom)] {
		return fmt.Errorf("%w: 缩放只有 90%%、100%%、110%%、125%% 四档", ErrInvalid)
	}
	if c.Layout.SidebarWidth < SidebarWidthMin || c.Layout.SidebarWidth > SidebarWidthMax {
		return fmt.Errorf("%w: 侧栏宽度要在 %d 到 %d 像素之间", ErrInvalid, SidebarWidthMin, SidebarWidthMax)
	}
	if c.Layout.QueueHeight < QueueHeightMin || c.Layout.QueueHeight > QueueHeightMax {
		return fmt.Errorf("%w: 队列高度要在 %d 到 %d 像素之间", ErrInvalid, QueueHeightMin, QueueHeightMax)
	}
	if c.Layout.QueueListWidth < QueueListWidthMin || c.Layout.QueueListWidth > QueueListWidthMax {
		return fmt.Errorf("%w: 任务列表宽度要在 %d 到 %d 像素之间", ErrInvalid, QueueListWidthMin, QueueListWidthMax)
	}
	if c.Startup.AutoApplyOnStartup && len(c.Startup.AutoDetectMenus) == 0 {
		return fmt.Errorf("%w: 启动自动应用要先在「启动后自动检测」里选至少一个主菜单", ErrInvalid)
	}
	if c.Startup.AutoCloseAfterApply && !c.Startup.AutoApplyOnStartup {
		return fmt.Errorf("%w: 自动应用完成后退出要先开启「检测完成后自动应用」", ErrInvalid)
	}
	if err := c.validateAutoDetectMenus(menuIDs); err != nil {
		return err
	}
	for scopeID, strategyID := range c.Accelerator.DefaultStrategyByScope {
		if !scopeIDs[scopeID] {
			return fmt.Errorf("%w: 加速范围 %s 不在产品目录里", ErrInvalid, scopeID)
		}
		if !strategies[strategyID] {
			return fmt.Errorf("%w: 方法 %s 当前没有注册，范围 %s 的选择无法保存", ErrInvalid, strategyID, scopeID)
		}
	}
	return nil
}

// validateAutoDetectMenus 校「启动后自动检测」那份主菜单清单。
// 空项、重复、不在目录里，三条都只在这一处判。
// 界面只会从菜单里勾，出现这三类值说明配置文件被手改过；这里拒收而不是顺手抹平，
// 因为抹平之后界面上显示的和盘上存的就不再是同一回事。
func (c Config) validateAutoDetectMenus(menuIDs map[string]bool) error {
	seen := make(map[string]bool, len(c.Startup.AutoDetectMenus))
	for _, menuID := range c.Startup.AutoDetectMenus {
		if menuID == "" {
			return fmt.Errorf("%w: 自动检测的主菜单清单里有空项", ErrInvalid)
		}
		if seen[menuID] {
			return fmt.Errorf("%w: 自动检测的主菜单 %s 重复出现", ErrInvalid, menuID)
		}
		seen[menuID] = true
		if !menuIDs[menuID] {
			return fmt.Errorf("%w: 主菜单 %s 不在产品目录里", ErrInvalid, menuID)
		}
	}
	return nil
}

func roundZoom(z float64) float64 {
	return math.Round(z*1000) / 1000
}

// BackupPath 是损坏配置的隔离副本位置（有界：只保留最近一份）。
func BackupPath(path string) string {
	return path + ".bad"
}

// ProfileCandidateLimit 返回档位决定的每 hostname 候选上限，供界面显示；
// 实际执行仍由后端硬上限约束，前端限制不能代替后端校验。
func ProfileCandidateLimit(profile string) (int, bool) {
	switch profile {
	case ProfileLow:
		return 2, true
	case ProfileStandard:
		return 8, true
	case ProfileFull:
		return 16, true
	}
	return 0, false
}

// ProfileConcurrency 返回档位决定的「同一时刻最多几个网络请求」。
// 完整档不给超过 8：8 是规划定的安全硬上限，提高并发只会加大瞬时带宽与同时连接压力，
// 换不来更多候选覆盖（候选数由上一那个函数管）。
// 检测执行编排按这个数字开工作协程，前端那份说明文字必须和它相等。
func ProfileConcurrency(profile string) (int, bool) {
	switch profile {
	case ProfileLow:
		return 2, true
	case ProfileStandard:
		return 8, true
	case ProfileFull:
		return 8, true
	}
	return 0, false
}

// readState 区分四种读取结果，决定是走默认值、拒绝改写还是备份重置。
type readState int

const (
	readMissing readState = iota
	readOK
	readBroken
	readTooNew
)

func read(path string) (Config, readState, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, readMissing, nil
	}
	if err != nil {
		return Config{}, readBroken, err
	}
	var probe struct {
		SchemaVersion int `json:"schemaVersion"`
		Layout        struct {
			ToastDisabled *bool `json:"toastDisabled"`
		} `json:"layout"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Config{}, readBroken, fmt.Errorf("配置文件不是合法 JSON")
	}
	if probe.SchemaVersion > SchemaVersion {
		return Config{}, readTooNew, fmt.Errorf("%w: 文件里的版本是 %d，当前程序只支持 %d", ErrSchemaTooNew, probe.SchemaVersion, SchemaVersion)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, readBroken, fmt.Errorf("配置文件字段不合法: %v", err)
	}
	// 老配置文件里没写过 toastDisabled：Go 零值 false 会被当成「用户要显示」。
	// 出厂默认是关掉的，缺字段按默认补上；显式写过 true/false 的一律尊重。
	if probe.Layout.ToastDisabled == nil {
		c.Layout.ToastDisabled = Default().Layout.ToastDisabled
	}
	return c, readOK, nil
}

// Store 负责一个配置文件的读写，所有写入都在锁里串行执行。
type Store struct {
	path       string
	scopeIDs   map[string]bool
	strategies map[string]bool
	menuIDs    map[string]bool

	mu           sync.Mutex
	cfg          Config
	loaded       bool
	loadErr      error
	notice       string
	lastMutation string
}

// NewStore 创建配置服务；三个集合都可以传 nil，表示这类值一律不接受。
// menuIDs 是产品目录里的主菜单 ID，用来校「启动后自动检测」勾了哪些菜单。
func NewStore(path string, scopeIDs, strategies, menuIDs map[string]bool) *Store {
	if scopeIDs == nil {
		scopeIDs = map[string]bool{}
	}
	if strategies == nil {
		strategies = map[string]bool{}
	}
	if menuIDs == nil {
		menuIDs = map[string]bool{}
	}
	return &Store{path: path, scopeIDs: scopeIDs, strategies: strategies, menuIDs: menuIDs}
}

// Path 返回这个 Store 管理配置文件的位置，供状态界面显示和排障。
func (s *Store) Path() string { return s.path }

// Notice 返回最近一次加载产生的用户可见说明，例如「原配置已损坏，已备份到某处并用默认值运行」。
func (s *Store) Notice() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoadedLocked(); err != nil {
		return s.notice
	}
	return s.notice
}

// Snapshot 返回带 revision 的完整配置；文件不存在时给内存默认值，不为了读取而写盘。
func (s *Store) Snapshot() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoadedLocked(); err != nil {
		return Config{}, err
	}
	return s.cfg, nil
}

// ensureLoadedLocked 只在第一次读取时碰磁盘：缺文件用默认值、版本更高就报错并且绝不改写、
// 内容坏了就备份后用默认值，并把原因写成用户可见的 notice。
func (s *Store) ensureLoadedLocked() error {
	if s.loaded {
		return s.loadErr
	}
	s.loaded = true
	c, state, err := read(s.path)
	switch state {
	case readMissing:
		s.cfg = Default()
		return nil
	case readTooNew:
		s.notice = "配置文件来自更新的程序版本，本次既不读取也不改写它。"
		s.loadErr = err
		return err
	case readOK:
		if validateErr := c.Validate(s.scopeIDs, s.strategies, s.menuIDs); validateErr == nil {
			s.cfg = c
			return nil
		} else {
			err = validateErr
		}
	}
	// 走到这里说明文件读不出可用内容：先隔离原件，再用默认值继续运行。
	backupErr := s.quarantineLocked()
	if backupErr == nil {
		s.notice = fmt.Sprintf("配置文件不可用（%v），原文件已转到 %s，本次使用默认设置。", err, BackupPath(s.path))
	} else {
		s.notice = fmt.Sprintf("配置文件不可用（%v），备份也失败（%v），本次使用默认设置。", err, backupErr)
	}
	s.cfg = Default()
	return nil
}

// quarantineLocked 把当前文件内容另存为唯一的隔离副本，然后删掉原件，
// 保证下一次保存不会被写进一个内容不明的文件。
func (s *Store) quarantineLocked() error {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(BackupPath(s.path), raw, 0o600); err != nil {
		return err
	}
	return os.Remove(s.path)
}

// Save 按字段 patch 保存：比对 revision → 校验完整候选配置 → 同目录临时文件落盘并原子替换 → 递增 revision。
// 任何一步失败都不改内存状态，也不留下半成品文件。
func (s *Store) Save(p Patch, expectedRevision int64, mutationID string) (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoadedLocked(); err != nil {
		return Config{}, err
	}
	mutationID = strings.TrimSpace(mutationID)
	if mutationID == "" {
		return Config{}, fmt.Errorf("%w：保存请求必须带幂等键", ErrEmptyMutationID)
	}
	// 同一个 mutationID 重复提交直接返回当前结果，双击和网络重试都不会二次写入。
	if s.lastMutation == mutationID {
		return s.cfg, nil
	}
	if expectedRevision != s.cfg.Revision {
		return Config{}, fmt.Errorf("%w: 你的副本是第 %d 版，当前已是第 %d 版", ErrRevisionConflict, expectedRevision, s.cfg.Revision)
	}

	next := s.cfg
	applyPatch(&next, p)
	// 这里不静默替用户改依赖选项：矛盾组合由 Validate 明确拒绝，
	// 否则界面显示的和真正存下来的会是两回事。
	if next.Accelerator.DefaultStrategyByScope == nil {
		next.Accelerator.DefaultStrategyByScope = map[string]string{}
	}
	if err := next.Validate(s.scopeIDs, s.strategies, s.menuIDs); err != nil {
		return Config{}, err
	}
	next.Revision = s.cfg.Revision + 1
	if err := writeAtomic(s.path, next); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrSaveFailed, err)
	}
	s.cfg = next
	s.lastMutation = mutationID
	return next, nil
}

func applyPatch(c *Config, p Patch) {
	if p.NetworkProfile != nil {
		c.Network.Profile = *p.NetworkProfile
	}
	if p.MaxCandidateProbesPerRun != nil {
		c.Network.MaxCandidateProbesPerRun = *p.MaxCandidateProbesPerRun
	}
	if p.ConfirmUpdateDownloadOnMetered != nil {
		c.Network.ConfirmUpdateDownloadOnMetered = *p.ConfirmUpdateDownloadOnMetered
	}
	if p.DefaultStrategyByScope != nil {
		c.Accelerator.DefaultStrategyByScope = *p.DefaultStrategyByScope
	}
	if p.Language != nil {
		c.Appearance.Language = *p.Language
	}
	if p.Theme != nil {
		c.Appearance.Theme = *p.Theme
	}
	if p.Zoom != nil {
		c.Appearance.Zoom = *p.Zoom
	}
	if p.SidebarWidth != nil {
		c.Layout.SidebarWidth = *p.SidebarWidth
	}
	if p.QueueHeight != nil {
		c.Layout.QueueHeight = *p.QueueHeight
	}
	if p.QueueHeightLocked != nil {
		c.Layout.QueueHeightLocked = *p.QueueHeightLocked
	}
	if p.QueueListWidth != nil {
		c.Layout.QueueListWidth = *p.QueueListWidth
	}
	if p.ToastDisabled != nil {
		c.Layout.ToastDisabled = *p.ToastDisabled
	}
	if p.DetectFocusFollow != nil {
		c.TreeBehavior.DetectFocusFollow = *p.DetectFocusFollow
	}
	if p.ApplyFocusFollow != nil {
		c.TreeBehavior.ApplyFocusFollow = *p.ApplyFocusFollow
	}
	if p.CheckedItemIDs != nil {
		ids := *p.CheckedItemIDs
		// 去重 + 长度兜底：目录总共几十项，超限的只截到上限，不报错——
		// 勾选是意图不是事实，坏清单不值得让整份保存失败。
		seen := make(map[string]bool, len(ids))
		out := make([]string, 0, len(ids))
		for _, id := range ids {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, id)
		}
		if len(out) > 512 {
			out = out[:512]
		}
		c.TreeBehavior.CheckedItemIDs = out
	}
	if p.AutoStart != nil {
		c.Startup.AutoStart = *p.AutoStart
	}
	if p.CloseToTray != nil {
		c.Startup.CloseToTray = *p.CloseToTray
	}
	if p.StartMinimized != nil {
		c.Startup.StartMinimized = *p.StartMinimized
	}
	// 清单是指针指向的切片：没带这个字段＝不改，带了空切片＝清空。
	// 清空时界面上必须把下游两个开关一起放进同一个补丁显式关掉，
	// 否则这份候选配置会因为依赖不成立被 Validate 整批拒掉。
	if p.AutoDetectMenus != nil {
		c.Startup.AutoDetectMenus = *p.AutoDetectMenus
	}
	if p.AutoApplyOnStartup != nil {
		c.Startup.AutoApplyOnStartup = *p.AutoApplyOnStartup
	}
	if p.AutoCloseAfterApply != nil {
		c.Startup.AutoCloseAfterApply = *p.AutoCloseAfterApply
	}
}

// writeAtomic 在同目录建唯一临时文件、同步落盘再改名替换；目录 0700，文件 0600。
func writeAtomic(path string, c Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
