package app

import (
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/hosts"
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
}

func NewService(appName, version string, sequence func() int64) *Service {
	if sequence == nil {
		sequence = func() int64 { return 0 }
	}
	c, err := catalog.Load()
	s := &Service{
		appName:        appName,
		version:        version,
		startedAt:      time.Now(),
		sequence:       sequence,
		productCatalog: c,
		// 加载失败时 c 是 nil，先把错误留下，读接口再负责翻译成人能看懂的错误码。
		productCatalogErr: err,
		hostsPath:         HostsPath(),
	}
	if err == nil {
		s.settings, s.settingsErr = newSettingsStore(c)
	} else {
		s.settingsErr = err
	}
	return s
}

// newSettingsStore 用目录里的 scopeID 作为校验集合；方法注册表还没实现，
// 所以已注册方法集合是空的——任何默认方法选择都会被拒，不会静默接受。
func newSettingsStore(c *catalog.Catalog) (*config.Store, error) {
	path, err := config.Path()
	if err != nil {
		return nil, err
	}
	return config.NewStore(path, c.ScopeIDs(), nil), nil
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

// GetAppStatus 返回平台、Hosts 写权限探测结果、DNS 解析选型和应用版本。
// Hosts 权限只是「能不能打开来写」的探测，本工具当前不写 Hosts；
// DNS 一栏报的是首期选型（系统解析器），检测任务还没接入，所以 DetectionReady 是 false。
func (s *Service) GetAppStatus() (AppStatus, error) {
	status := AppStatus{
		App:               s.appName,
		Version:           s.version,
		OS:                runtime.GOOS,
		Arch:              runtime.GOARCH,
		HostsPath:         HostsPath(),
		HostsAccess:       ProbeHostsAccess(HostsPath()),
		DNSResolver:       DNSResolverSystem,
		DNSDetectionReady: false,
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
