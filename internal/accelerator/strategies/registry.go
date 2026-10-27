package strategies

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Registry 收集策略描述符。注册失败不 panic，也不静默丢弃：
// 错误既作为返回值给显式调用方，也攒进 Errors() 给装配层——
// 因为策略文件是靠 init 登记的，那一刻没人能接住返回值。
type Registry struct {
	mu   sync.RWMutex
	byID map[string]Strategy
	errs []error
}

func NewRegistry() *Registry {
	return &Registry{byID: map[string]Strategy{}}
}

var global = NewRegistry()

// Register 校验描述符的字段形状，合格才收。
// 同一个 ID 重复注册是错误：保留先到的那一份，不让后一份静默覆盖已保存配置指向的方法。
func (r *Registry) Register(s Strategy) error {
	if err := checkShape(s); err != nil {
		r.record(err)
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, dup := r.byID[s.ID]; dup {
		err := fmt.Errorf("加速方法 %s 被重复注册（后一份被忽略，保留的是「%s」）", s.ID, prev.Name)
		r.recordLocked(err)
		return err
	}
	r.byID[s.ID] = s
	return nil
}

func (r *Registry) record(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordLocked(err)
}

func (r *Registry) recordLocked(err error) { r.errs = append(r.errs, err) }

// Errors 返回至今收集到的注册错误。装配层必须在启动时取一次：
// 有错误就说明某个方法根本没进表，界面上就不会有它可选，不能装作注册成功。
func (r *Registry) Errors() []error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]error, len(r.errs))
	copy(out, r.errs)
	return out
}

// All 返回按 ID 排序的只读快照，顺序稳定，界面可以直接按这个顺序列出来。
func (r *Registry) All() []Strategy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Strategy, 0, len(r.byID))
	for _, s := range r.byID {
		out = append(out, s.clone())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) Get(id string) (Strategy, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.byID[id]
	if !ok {
		return Strategy{}, false
	}
	return s.clone(), true
}

// ForScope 列出适用于某个加速范围的方法，同样按 ID 排序。
// 范围是否真的存在于产品目录由 UnknownScopes 判，这里只管筛选。
func (r *Registry) ForScope(scopeID string) []Strategy {
	var out []Strategy
	for _, s := range r.All() {
		for _, got := range s.ScopeIDs {
			if got == scopeID {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// IDs 是给配置服务用的集合：保存「某范围默认用哪个方法」时先查方法在不在表里。
func (r *Registry) IDs() map[string]bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]bool, len(r.byID))
	for id := range r.byID {
		out[id] = true
	}
	return out
}

// UnknownScopes 让装配层拿着产品目录的 scopeID 来核对：
// 策略声明的范围必须是目录里真有的，写错或目录改名要当场报，不能让选择器里出现点不到的范围。
// 这里不直接依赖产品目录包，是为了让策略包能被装配层单独测试，也避免初始化顺序上的循环。
func (r *Registry) UnknownScopes(known map[string]bool) []error {
	var out []error
	for _, s := range r.All() {
		for _, scopeID := range s.ScopeIDs {
			if !known[scopeID] {
				out = append(out, fmt.Errorf("加速方法 %s 声明的范围 %s 不在产品目录里", s.ID, scopeID))
			}
		}
	}
	return out
}

// checkShape 是注册门槛。为什么这些要当场拦：描述符一旦进了表就会被写进用户配置，
// 空文案、坏 ID 或没有实现的方法都会留下没法恢复的脏选择。
func checkShape(s Strategy) error {
	if !validID(s.ID) {
		return fmt.Errorf("加速方法 ID 不合法：%q（要求小写字母、数字、点、下划线或减号，首字符是字母或数字，最长 64）", s.ID)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("加速方法 %s 没有面向用户的名称", s.ID)
	}
	if strings.TrimSpace(s.Description) == "" {
		return fmt.Errorf("加速方法 %s 没有面向用户的说明", s.ID)
	}
	if len(s.ScopeIDs) == 0 {
		return fmt.Errorf("加速方法 %s 没有声明适用的加速范围", s.ID)
	}
	seen := map[string]bool{}
	for _, scopeID := range s.ScopeIDs {
		if strings.TrimSpace(scopeID) == "" {
			return fmt.Errorf("加速方法 %s 的适用范围里有空值", s.ID)
		}
		if seen[scopeID] {
			return fmt.Errorf("加速方法 %s 的适用范围里 %s 重复", s.ID, scopeID)
		}
		seen[scopeID] = true
	}
	if s.Caps == 0 {
		return fmt.Errorf("加速方法 %s 没声明任何能力", s.ID)
	}
	if s.Caps&^knownCapabilities != 0 {
		return fmt.Errorf("加速方法 %s 声明了未知能力位：%d", s.ID, s.Caps&^knownCapabilities)
	}
	// 首期只收「能出候选」的方法；CapabilitySystemDNSOnly 不在这里强制，
	// 因为以后真的加外部解析器时，它就该带上「没有这一位」这个事实，让用户自己判断隐私影响。
	if !s.Caps.has(CapabilityCandidateLookup) {
		return fmt.Errorf("加速方法 %s 没有候选取得能力，当前阶段不收", s.ID)
	}
	if s.Lookup == nil {
		return fmt.Errorf("加速方法 %s 没有实现", s.ID)
	}
	return nil
}

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (s Strategy) clone() Strategy {
	scopes := make([]string, len(s.ScopeIDs))
	copy(scopes, s.ScopeIDs)
	s.ScopeIDs = scopes
	return s
}

// 包级便捷函数，都走 init 注册进来的那张全局表。

func Register(s Strategy) error { return global.Register(s) }

// recordInitError 是给策略文件的 init 用的：那一刻没有调用方能接住返回值，
// 只能把错误存进全局表，等装配层启动时取一次并如实报出来。
func recordInitError(err error) { global.record(err) }

func Errors() []error { return global.Errors() }

func All() []Strategy { return global.All() }

func Get(id string) (Strategy, bool) { return global.Get(id) }

func ForScope(scopeID string) []Strategy { return global.ForScope(scopeID) }

func IDs() map[string]bool { return global.IDs() }

func UnknownScopes(known map[string]bool) []error { return global.UnknownScopes(known) }
