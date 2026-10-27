package app

import (
	"runtime"
	"strings"
	"time"

	"github.com/xiaokentrl/github-net/internal/catalog"
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
}

func NewService(appName, version string, sequence func() int64) *Service {
	if sequence == nil {
		sequence = func() int64 { return 0 }
	}
	c, err := catalog.Load()
	return &Service{
		appName:        appName,
		version:        version,
		startedAt:      time.Now(),
		sequence:       sequence,
		productCatalog: c,
		// 加载失败时 c 是 nil，先把错误留下，读接口再负责翻译成人能看懂的错误码。
		productCatalogErr: err,
	}
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
// Hosts 状态现在一律是 unknown——后端的 Hosts 读取服务还没实现，unknown 的意思是「还没读过」，
// 不是「未替换」，前端必须按未读取显示，不许把它显示成一种检测结果。
func (s *Service) GetItems() (ItemsSnapshot, error) {
	if s.productCatalogErr != nil {
		// 前端在这里只会拿到错误；快照仍给空数组形状，避免有人直接用返回值时撞上 null。
		return ItemsSnapshot{Categories: []CatalogCategory{}, Hosts: []HostsState{}}, ToPublicError(s.productCatalogErr)
	}
	return ItemsSnapshot{
		Categories: CatalogToCategories(s.productCatalog),
		Hosts:      UnknownHosts(s.productCatalog),
	}, nil
}
