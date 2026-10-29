package app

import (
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/event"
	"github.com/xiaokentrl/github-net/internal/hosts"
	"github.com/xiaokentrl/github-net/internal/privilege"
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

	// detectReady 表示检测执行器已经换装（目录和设置都就绪）。
	// 它决定 GetAppStatus 的 DNSDetectionReady，前端据此放开检测入口。
	detectReady bool

	// priv 是特权辅助服务客户端；应用/还原类命令经由它落到受限服务。
	// 测试注入假实现；nil 表示这一层没接（应用/恢复任务在队列里以可见失败收场）。
	priv privilege.Caller

	// updater 查 GitHub 最新稳定版；测试可注入假实现。
	updater *updater.Client

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
		stamp:             currentBuildStamp(),
	}
	// 队列的执行动作在目录和设置都就绪时才换上真实检测执行器；缺哪样都先走拒答，
	// 不降级硬跑。换装和排任务的入口 DetectItems 是同一步放行的，见 TECHNICAL_PLAN 阶段三。
	var detectExec task.Executor = pendingExecutor{}
	if err == nil {
		s.settings, s.settingsErr = newSettingsStore(c)
		if s.settings != nil {
			detectExec = newDetectExecutor(c, s.settings, nil, results, event.NewDetectEmitter(taskEmit))
			s.detectReady = true
		}
	}
	s.priv = privilege.NewClient()
	s.updater = updater.NewClient()
	s.tasks = newTaskQueue(newDispatchExecutor(detectExec, s.priv, results), taskEmit)
	return s
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
// Hosts 状态是每次调用现读的：只读打开系统 Hosts，找出本工具的管理区段，按项目逐个判定。
// 读不到文件报 readFailed、区段判读不了报 conflict，这两种都不会被显示成「未替换」。
func (s *Service) GetItems() (ItemsSnapshot, error) {
	if s.productCatalogErr != nil {
		// 前端在这里只会拿到错误；快照仍给空数组形状，避免有人直接用返回值时撞上 null。
		return ItemsSnapshot{Categories: []CatalogCategory{}, Hosts: []HostsState{}}, ToPublicError(s.productCatalogErr)
	}
	snapshot := hosts.Read(s.hostsPath)
	return ItemsSnapshot{
		Categories: CatalogToCategories(s.productCatalog),
		Hosts:      HostsStates(s.productCatalog, snapshot, s.hostsReads.Add(1)),
	}, nil
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
func (s *Service) SaveSettings(patch config.Patch, expectedRevision int64, mutationID string) (config.Config, error) {
	if s.settings == nil {
		return config.Config{}, s.settingsFailure()
	}
	cfg, err := s.settings.Save(patch, expectedRevision, mutationID)
	if err != nil {
		return config.Config{}, ToPublicError(err)
	}
	return cfg, nil
}
