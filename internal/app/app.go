package app

import (
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/hosts"
	"github.com/xiaokentrl/github-net/internal/platform"
	"github.com/xiaokentrl/github-net/internal/privilege"
	"github.com/xiaokentrl/github-net/internal/store"
	"github.com/xiaokentrl/github-net/internal/task"
	"github.com/xiaokentrl/github-net/internal/updater"
)

// Service 是暴露给前端的唯一门面；这里只放前端确实需要的读操作。
type Service struct {
	appName   string
	version   string
	startedAt time.Time
	sequence  func() int64

	// productCatalog 是内嵌的产品目录；productCatalogErr 记录加载失败原因，
	// 每次读目录都会把它转成稳定错误码回给前端，不在启动时静默吞掉。
	productCatalog    *catalog.Catalog
	productCatalogErr error

	// settings 是用户配置服务。构造它需要知道合法 scopeID，
	// 所以目录加载失败时这里是 nil，读设置会明确报错而不是回退成「没配置」。
	settings    *config.Store
	settingsErr error

	// hostsPath 是系统 Hosts 的路径，每次读目录都现读一遍，不缓存结果。
	// hostsReads 是「第几次读」的计数，只用来给前端一个能比大小的 revision。
	hostsPath  string
	hostsReads atomic.Int64

	// tasks 是内存任务队列。它在进程生命周期里只建一次，重启即清空——
	// 队列不落地，没有历史文件可读可写。
	tasks *task.Queue

	// detectResults 存每个项目最近一轮的检测结论，详情页的候选表从它取。
	detectResults *detectResultStore

	// candidateStore 是候选底账（SQLite，配置目录旁的 detect-history.db）。
	// 打不开就是 nil：检测照跑，只是不留底账。读接口下一轮接。
	candidateStore *store.Store

	// rounds 是批量检测共享预算的账本（每轮一份，内存态）。
	rounds *roundBudgets

	// detectReady 表示检测执行器已经换装（目录和设置都就绪）。
	// 它决定 GetAppStatus 的 DNSDetectionReady，前端据此放开检测入口。
	detectReady bool

	// priv 是特权辅助服务客户端；应用/还原类命令经由它落到受限服务。
	// 测试注入假实现；nil 表示这一层没接（应用/恢复任务在队列里以可见失败收场）。
	priv privilege.Caller

	// updater 查 GitHub 最新稳定版；测试可注入假实现。
	updater *updater.Client
	// pubKey 是随包分发的验签公钥；测试注入自己的测试公钥。
	// nil 意味着拒绝一切升级——没有公钥的客户端不做「不验签」的下载安装。
	pubKey ed25519.PublicKey

	// autostart 是开机自启动的平台实现；测试注入假实现。
	autostart platform.Autostart

	// openPath 是「请系统打开本地文件」的接缝：nil 即真实实现（xdg-open 等）。
	// 测试注入记录函数，免得 CI 容器里真去弹系统安装器。
	openPath func(path string) error

	// strategyTable 是方法注册表的读取接缝，零值即生产行为（读包级全局表）。
	// 声明在 Service 上而不是每个命令各自读包级函数，是为了让方法选择区的
	// 「真表走不到的分支」有测试落点，见 strategies.go。
	strategyTable strategyTable

	// stamp 是这份构建自带的 VCS 标记，进程活着就不会变，所以构造时读一次。
	stamp buildStamp
}

// NewService 建暴露给前端的门面。taskEmit 是队列进度事件的投递出口，
// 装配层传 Wails 事件总线的 Emit；测试和还没接总线的阶段传 nil，表示不发事件。
func NewService(appName, version string, sequence func() int64, taskEmit func(name string, payload any)) *Service {
	if sequence == nil {
		sequence = func() int64 { return 0 }
	}
	c, err := catalog.Load()
	results := newDetectResultStore()
	rounds := newRoundBudgets()
	// 全进程网络并发闸：无论多少条检测任务并行，同时活动的 DNS/IP 请求不过 8 个。
	probeGate := accelerator.NewGate(accelerator.MaxConcurrency)
	// 候选底账开在配置文件旁边。开不出来不拦启动：底账是锦上添花的记录，检测结果照常有。
	var candDB *store.Store
	if p, perr := config.Path(); perr == nil {
		if db, derr := store.Open(filepath.Join(filepath.Dir(p), "detect-history.db")); derr == nil {
			candDB = db
		}
	}
	s := &Service{
		appName:        appName,
		version:        version,
		startedAt:      time.Now(),
		sequence:       sequence,
		productCatalog: c,
		// 加载失败时 c 是 nil，先把错误留下，读接口再负责翻译成人能看懂的错误码。
		productCatalogErr: err,
		hostsPath:         HostsPath(),
		detectResults:     results,
		candidateStore:    candDB,
		rounds:            rounds,
		stamp:             currentBuildStamp(),
	}
	// 队列的执行动作在目录和设置都就绪时才换上真实检测执行器；缺哪样都先走拒答，
	// 不降级硬跑。换装和排任务的入口 DetectItems 是同一步放行的，见 TECHNICAL_PLAN 阶段三。
	var detectExec task.Executor = pendingExecutor{}
	if err == nil {
		s.settings, s.settingsErr = newSettingsStore(c)
		if s.settings != nil {
			detectExec = newDetectExecutor(c, s.settings, nil, results, event.NewDetectEmitter(taskEmit), probeGate, rounds, s.appliedIPsFor, s.saveDetectRound, factoryDefaultStrategy)
			s.detectReady = true
		}
	}
	s.priv = privilege.NewClient()
	s.updater = updater.NewClient()
	s.pubKey = updater.PublicKey()
	s.autostart = platform.NewAutostart()
	s.tasks = newTaskQueue(newDispatchExecutor(detectExec, s.priv, results), taskEmit)
	return s
}

// saveDetectRound 把跑完的一轮写进候选底账。没开成库就什么都不做；
// 写失败在这里收掉——底账少一行不影响检测结论，也不该把错误抛回队列。
func (s *Service) saveDetectRound(taskRound, scopeID, strategyID string, r *accelerator.Result) {
	if s.candidateStore == nil {
		return
	}
	_ = s.candidateStore.SaveRound(taskRound, scopeID, strategyID, r)
}

// factoryDefaultStrategy 是「用户还没选过方法」时的出厂默认（拍板 2026-10-31）：
// 「全部公共源」声明适用的范围（现在就是 github）直接用它。
// 从注册表现查「全部」自己声明的范围，不抄死范围清单；方法没注册或范围不适用就没有默认。
// 这个默认放装配层而不是 config.Default()：配置包不该认识「github」这个范围名，
// 否则默认值自己都过不了「空集合一律拒绝」的校验门。
func factoryDefaultStrategy(scopeID string) (string, bool) {
	s, ok := strategies.Get(strategies.StrategyIDAllSources)
	if !ok {
		return "", false
	}
	for _, got := range s.ScopeIDs {
		if got == scopeID {
			return s.ID, true
		}
	}
	return "", false
}

// newSettingsStore 用目录里的 scopeID 和方法注册表里的已注册方法作为校验集合。
// 注册表靠各策略文件的 init 登记，所以这里必须真的引用它一次，那张表才是填好的。
// 校验集合为空时任何「某范围默认用某方法」的保存都会被拒——不静默接受一个点不到的方法。
func newSettingsStore(c *catalog.Catalog) (*config.Store, error) {
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	return config.NewStore(path, c.ScopeIDs(), strategies.IDs()), nil
}

// RuntimeInfo 返回当前进程的真实运行环境，供前端显示与绑定链路自检。
func (s *Service) RuntimeInfo() (RuntimeInfo, error) {
	return RuntimeInfo{
		AppName:        s.appName,
		Version:        s.version,
		OS:             runtime.GOOS,
		Arch:           runtime.GOARCH,
		StartedAt:      s.startedAt.UTC().Format(time.RFC3339),
		StatusSequence: s.sequence(),
	}, nil
}

// Ping 只为验证「后端错误能传回前端」这条路：空令牌一律拒绝，不做任何检测。
func (s *Service) Ping(token string) (string, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return "", NewError(CodeEmptyToken, "令牌不能为空", nil)
	}
	return trimmed, nil
}

// GetItems 返回产品目录和每个项目当前的 Hosts 状态，是目录的唯一来源；前端不再自己存一份列表。
// Hosts 状态是每次调用现读的：只读打开系统 Hosts，找出本工具的管理区段，再问一次特权服务
// 「最后一次成功写入是哪批映射」，两边比对后按项目逐个判定。
// 读不到文件报 readFailed、区段判读不了或被改过报 conflict，这两种都不会被显示成「未替换」。
func (s *Service) GetItems() (ItemsSnapshot, error) {
	if s.productCatalogErr != nil {
		// 前端在这里只会拿到错误；快照仍给空数组形状，避免有人直接用返回值时撞上 null。
		return ItemsSnapshot{Categories: []CatalogCategory{}, Hosts: []HostsState{}}, ToPublicError(s.productCatalogErr)
	}
	snapshot := hosts.Read(s.hostsPath)
	return ItemsSnapshot{
		Categories: CatalogToCategories(s.productCatalog),
		Hosts:      HostsStates(s.productCatalog, snapshot, s.readAppliedState(), s.hostsReads.Add(1)),
	}, nil
}

// readAppliedState 问一次特权服务「最后一次成功写入是哪批映射」。
// 写入记录在 root 目录里，界面进程读不到，只能经由服务；服务没接或问了出错都带原因回来，
// 调用方据此按冲突处理，不拿「问不着」冒充「没写过」。
func (s *Service) readAppliedState() AppliedState {
	if s.priv == nil {
		return AppliedState{Reason: "特权辅助服务这一层没接上"}
	}
	written, err := s.priv.State()
	if err != nil {
		return AppliedState{Reason: unwrapMessage(err)}
	}
	return AppliedState{Known: true, Written: written}
}

// appliedIPsFor 读一轮检测开局时这些主机名已经写在管理区段上的地址，交给检测执行器当已知候选。
// 和 GetItems 一样是现读一次，不复用别处的缓存：一轮检测要的是「按下检测那一刻在用的地址」。
func (s *Service) appliedIPsFor(hostnames []string) map[string][]string {
	snapshot := hosts.Read(s.hostsPath)
	if !snapshot.OK() {
		return nil
	}
	return appliedFor(snapshot.Block, hostnames)
}

// GetAppStatus 返回平台、Hosts 写权限探测结果、DNS 解析选型和应用版本，
// 外加这份构建的源码提交时间与哈希（「关于我们」用，来自二进制自带的 VCS 标记）。
// Hosts 权限只是「能不能打开来写」的探测，本工具当前不写 Hosts；
// DNS 一栏报的是首期选型（系统解析器），DNSDetectionReady 表示检测执行器已换装、
// 前端可以放开检测入口；目录或设置读不出来时它是 false，不假装能测。
func (s *Service) GetAppStatus() (AppStatus, error) {
	status := AppStatus{
		App:               s.appName,
		Version:           s.version,
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		HostsPath:         HostsPath(),
		HostsAccess:       ProbeHostsAccess(HostsPath()),
		DNSResolver:       DNSResolverSystem,
		DNSDetectionReady: s.detectReady,
		BuildTime:         s.stamp.Time,
		BuildRevision:     s.stamp.Revision,
		BuildModified:     s.stamp.Modified,
	}
	if s.settings != nil {
		status.ConfigPath = s.settings.Path()
		status.ConfigNotice = s.settings.Notice()
	} else {
		status.ConfigNotice = s.settingsFailure().Message
	}
	return status, nil
}

// SettingsStore 把配置 store 交给托盘的 ThemeController 用（main.go 装配层）。
// 只暴露 store 本身，不给前端——ThemeController 不注册为 Wails 服务，界面无法调用。
func (s *Service) SettingsStore() *config.Store {
	return s.settings
}

// settingsFailure 保证「没有配置服务」这件事一定带回一个错误，绝不返回 (空配置, nil)。
func (s *Service) settingsFailure() *Error {
	if s.settingsErr != nil {
		return ToPublicError(s.settingsErr)
	}
	return NewError(CodeInternal, "配置服务没准备好，请重启应用", nil)
}

// GetSettings 返回带 schemaVersion 和 revision 的完整配置快照。
// 文件不存在时给代码里的默认值，不为了读取而写盘。
func (s *Service) GetSettings() (config.Config, error) {
	if s.settings == nil {
		return config.Config{}, s.settingsFailure()
	}
	cfg, err := s.settings.Snapshot()
	if err != nil {
		return config.Config{}, ToPublicError(err)
	}
	return cfg, nil
}

// SaveSettings 按字段 patch 保存：先比 revision，再校验完整配置，最后原子替换并递增 revision。
// 失败一律明确返回，前端不能把没落盘的东西显示成已保存。
//
// 配置落盘之后还有一类「跟着设置走」的系统副作用：开机自启动的桌面入口。
// 它跟在配置后面做，失败了如实报「设置已保存、但这一项没改成」——
// 配置是权威，副作用下一轮保存或重启应用还会再对齐。
func (s *Service) SaveSettings(patch config.Patch, expectedRevision int64, mutationID string) (config.Config, error) {
	if s.settings == nil {
		return config.Config{}, s.settingsFailure()
	}
	cfg, err := s.settings.Save(patch, expectedRevision, mutationID)
	if err != nil {
		return config.Config{}, ToPublicError(err)
	}
	if patch.AutoStart != nil {
		if err := s.applyAutostart(*patch.AutoStart); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// applyAutostart 把开机自启动改到和配置一致；不支持的平台给实话，不假装成功。
func (s *Service) applyAutostart(enabled bool) error {
	var err error
	if enabled {
		err = s.autostart.Enable()
	} else {
		err = s.autostart.Disable()
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, platform.ErrUnsupported) {
		return NewError(CodePlatformUnsupported, "当前平台还不支持开机自启动", err)
	}
	return NewError(CodeAutostartFailed, "设置已保存，但开机自启动没能改成："+err.Error(), err)
}
