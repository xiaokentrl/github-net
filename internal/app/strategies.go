package app

import (
	"strings"

	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/config"
)

// strategyTable 是「方法注册表从哪儿读」这个接缝。
// 零值就是生产行为：读包级那张由各方法文件 init 登记进来的全局表，装配层不需要额外接线。
// 留成可注入是为了覆盖真表上走不到的分支——比如「某个范围一个方法都没有」，
// 装配层那道注册门（registry_assembly_test.go）本来就要求每个目录范围至少有一个可选方法。
// 不能改成往全局表里塞假方法：同 ID 重复注册会把错误写进全局 Errors()，把那道门照脏。
type strategyTable struct {
	get      func(id string) (strategies.Strategy, bool)
	forScope func(scopeID string) []strategies.Strategy
	errs     func() []error
}

func (t strategyTable) lookup(id string) (strategies.Strategy, bool) {
	if t.get == nil {
		return strategies.Get(id)
	}
	return t.get(id)
}

func (t strategyTable) list(scopeID string) []strategies.Strategy {
	if t.forScope == nil {
		return strategies.ForScope(scopeID)
	}
	return t.forScope(scopeID)
}

// registryErrors 取注册表的失败记录。这些错误只记得到方法 ID，记不到范围
// （注册那一刻没有调用方知道它属于哪个菜单），所以它是全表级事实：
// 每个范围的快照都原样带上，界面显示一次就够，不要按范围去解释成因。
func (t strategyTable) registryErrors() []error {
	if t.errs == nil {
		return strategies.Errors()
	}
	return t.errs()
}

// strategyProblem 是「这个选择为什么现在不能用」。只分三种，够用：
// 能用、方法根本不在表里、方法在表但它声明的适用范围里没有这个范围。
type strategyProblem uint8

const (
	strategyUsable strategyProblem = iota
	strategyNotRegistered
	strategyWrongScope
)

// check 核对一个方法 ID 在这个范围里到底能不能用。
// 这一道门必须在命令层：配置服务的校验只查「范围在目录里 + 方法在注册表里」两个集合，
// 不查方法自己声明的适用范围，所以「把 GitHub 的方法存进 Steam 范围」在配置层是合法的。
// 判定复用检测执行器那一份 strategyApplies，两处必须同一个口径，否则界面让存、检测又拒。
func (t strategyTable) check(strategyID, scopeID string) strategyProblem {
	s, ok := t.lookup(strategyID)
	if !ok {
		return strategyNotRegistered
	}
	if !strategyApplies(s, scopeID) {
		return strategyWrongScope
	}
	return strategyUsable
}

// StrategyOption 是方法选择区里的一行：可读的文案、能力，和它「是不是这个范围当前保存的选择」。
// 能力位摊成三个布尔，是因为前端要按它决定要不要提示隐私影响，
// 而把 Go 侧的位掩码原样发出去等于让界面去猜第几位是什么。
type StrategyOption struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	ScopeIDs     []string `json:"scopeIds"`
	CanLookup    bool     `json:"canLookup"`
	KeepsTLSName bool     `json:"keepsTlsHostname"`
	SystemDNS    bool     `json:"systemDnsOnly"`
	Selected     bool     `json:"selected"`
}

// StrategySnapshot 是 GetStrategies 的返回形状，也是 SelectStrategy 保存成功后的最新形状。
// Options 永远可解成数组：一个方法都没有时是空数组而不是 null。
// SavedID 是该范围当前保存的选择，没选过是空串；SavedUsable 与 SavedNote 说它现在还能不能用。
// 不能用时后端只说「要重选」，绝不自动换成表里的第一个方法——用哪个方法是用户定的事。
// Revision 是这份快照来自的配置版本号，下一次保存要原样带回 expectedRevision。
// Notices 是注册表的失败记录原话：某个方法没进表，选择区里就会少一行，这件事得有界面落点。
type StrategySnapshot struct {
	ScopeID     string           `json:"scopeId"`
	Options     []StrategyOption `json:"options"`
	SavedID     string           `json:"savedId"`
	SavedUsable bool             `json:"savedUsable"`
	SavedNote   string           `json:"savedNote,omitempty"`
	Revision    int64            `json:"revision"`
	Notices     []string         `json:"notices"`
}

// GetStrategies 列出某个加速范围当前可选的方法，并给出这个范围已保存的选择。
// 列表只来自注册表：范围存在但一个方法都没注册，就是空数组，界面上如实显示「没有可选方法」。
func (s *Service) GetStrategies(scopeID string) (StrategySnapshot, error) {
	if s.productCatalogErr != nil {
		return emptyStrategySnapshot(strings.TrimSpace(scopeID)), ToPublicError(s.productCatalogErr)
	}
	id, err := s.requireScope(scopeID)
	if err != nil {
		return emptyStrategySnapshot(id), err
	}
	cfg, err := s.currentConfig()
	if err != nil {
		return emptyStrategySnapshot(id), err
	}
	return s.strategySnapshot(id, cfg), nil
}

// SelectStrategy 保存某个范围以后用哪个加速方法。它只是记账：不启动检测、不写 Hosts、不碰系统代理。
// 落盘沿用设置的并发规则（expectedRevision + 幂等 mutationID），所以签名比契约里那两个参数多两个。
// 拒绝时一个字都不写：配置里的旧选择保持原样，界面上也不会出现「已保存」的假象。
func (s *Service) SelectStrategy(scopeID, strategyID string, expectedRevision int64, mutationID string) (StrategySnapshot, error) {
	if s.productCatalogErr != nil {
		return emptyStrategySnapshot(strings.TrimSpace(scopeID)), ToPublicError(s.productCatalogErr)
	}
	id, err := s.requireScope(scopeID)
	if err != nil {
		return emptyStrategySnapshot(id), err
	}
	want := strings.TrimSpace(strategyID)
	if want == "" {
		return emptyStrategySnapshot(id), NewError(CodeStrategyUnavailable, "没给方法 ID，不知道该保存哪个方法", nil)
	}
	switch s.strategyTable.check(want, id) {
	case strategyNotRegistered:
		return emptyStrategySnapshot(id), NewError(CodeStrategyUnavailable, "要保存的方法已经不在注册表里，换一个可用的方法再保存", nil)
	case strategyWrongScope:
		return emptyStrategySnapshot(id), NewError(CodeStrategyUnavailable, "要保存的方法不适用于这个加速范围，换一个适用于本范围的方法再保存", nil)
	}

	cfg, err := s.currentConfig()
	if err != nil {
		return emptyStrategySnapshot(id), err
	}
	// Patch 里的这张表是整体替换，所以先把现有的抄一份、只改本范围那一个键：
	// 直接发一张只有本范围的表，会把别的范围已保存的选择一起清掉。
	next := cloneDefaultStrategies(cfg.Accelerator.DefaultStrategyByScope)
	next[id] = want
	saved, err := s.settings.Save(config.Patch{DefaultStrategyByScope: &next}, expectedRevision, mutationID)
	if err != nil {
		return emptyStrategySnapshot(id), ToPublicError(err)
	}
	return s.strategySnapshot(id, saved), nil
}

// strategySnapshot 用一份配置快照拼出某个范围的选择区形状。
// 读命令和保存成功后都走这里，保证界面拿到的「已保存选择」和落盘的内容是同一份来源。
func (s *Service) strategySnapshot(scopeID string, cfg config.Config) StrategySnapshot {
	savedID := strings.TrimSpace(cfg.Accelerator.DefaultStrategyByScope[scopeID])
	list := s.strategyTable.list(scopeID)
	options := make([]StrategyOption, 0, len(list))
	for _, strategy := range list {
		options = append(options, StrategyOption{
			ID:           strategy.ID,
			Name:         strategy.Name,
			Description:  strategy.Description,
			ScopeIDs:     append([]string{}, strategy.ScopeIDs...),
			CanLookup:    strategy.Caps&strategies.CapabilityCandidateLookup != 0,
			KeepsTLSName: strategy.Caps&strategies.CapabilityKeepsTLSHostname != 0,
			SystemDNS:    strategy.Caps&strategies.CapabilitySystemDNSOnly != 0,
			Selected:     savedID != "" && strategy.ID == savedID,
		})
	}

	snapshot := StrategySnapshot{
		ScopeID: scopeID,
		Options: options,
		SavedID: savedID,
		// 空选择不是「坏选择」：没选过的时候可用位给 true，界面只在该重选时才提示。
		SavedUsable: savedID == "",
		Revision:    cfg.Revision,
		Notices:     []string{},
	}
	if savedID != "" {
		switch s.strategyTable.check(savedID, scopeID) {
		case strategyNotRegistered:
			snapshot.SavedNote = "这个范围之前保存的方法已经不在注册表里，重新选一个才能检测"
		case strategyWrongScope:
			snapshot.SavedNote = "这个范围之前保存的方法不适用于本范围，重新选一个才能检测"
		case strategyUsable:
			snapshot.SavedUsable = true
		}
	}
	for _, err := range s.strategyTable.registryErrors() {
		snapshot.Notices = append(snapshot.Notices, err.Error())
	}
	return snapshot
}

// requireScope 核对范围真的在产品目录里。空值和不认识的值共用 unknown_scope：
// 界面对这两种的处理是同一件事——这一屏没有可显示的选择区，没必要拆成两个码。
func (s *Service) requireScope(scopeID string) (string, error) {
	id := strings.TrimSpace(scopeID)
	if id == "" {
		return "", NewError(CodeUnknownScope, "没给加速范围，不知道该列哪个范围的方法", nil)
	}
	if !s.productCatalog.ScopeIDs()[id] {
		return id, NewError(CodeUnknownScope, "加速范围 "+id+" 不在产品目录里，界面上没有这个范围的选择区", nil)
	}
	return id, nil
}

// currentConfig 读一次配置快照；没有配置服务时绝不返回「空配置 + nil」。
func (s *Service) currentConfig() (config.Config, error) {
	if s.settings == nil {
		return config.Config{}, s.settingsFailure()
	}
	cfg, err := s.settings.Snapshot()
	if err != nil {
		return config.Config{}, ToPublicError(err)
	}
	return cfg, nil
}

// cloneDefaultStrategies 复制「哪个范围用哪个方法」这张表，nil 输入给空表。
func cloneDefaultStrategies(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	for scopeID, strategyID := range in {
		out[scopeID] = strategyID
	}
	return out
}

// emptyStrategySnapshot 给失败路径：形状仍然是数组，不让调用方撞上 null。
func emptyStrategySnapshot(scopeID string) StrategySnapshot {
	return StrategySnapshot{
		ScopeID: scopeID,
		Options: []StrategyOption{},
		Notices: []string{},
	}
}
