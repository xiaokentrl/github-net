package strategies

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/catalog"
)

func validStrategy(id string) Strategy {
	return Strategy{
		ID:          id,
		ScopeIDs:    []string{"github"},
		Name:        "名称",
		Description: "说明",
		Caps:        CapabilityCandidateLookup,
		Lookup:      func(context.Context, Input) (Output, error) { return Output{}, nil },
	}
}

func TestRegisterAcceptsValidShape(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(validStrategy("a-method")); err != nil {
		t.Fatalf("注册失败：%v", err)
	}
	if errs := r.Errors(); len(errs) != 0 {
		t.Fatalf("不该有注册错误：%v", errs)
	}
	got, ok := r.Get("a-method")
	if !ok {
		t.Fatal("按 ID 取不到已注册的方法")
	}
	if got.Name != "名称" {
		t.Fatalf("名称不对：%q", got.Name)
	}
}

// 注册表按 ID 排序输出，界面可以直接按这个顺序列，不需要再排。
func TestAllSortedByID(t *testing.T) {
	r := NewRegistry()
	for _, id := range []string{"zeta", "alpha", "mid"} {
		if err := r.Register(validStrategy(id)); err != nil {
			t.Fatalf("注册 %s 失败：%v", id, err)
		}
	}
	var ids []string
	for _, s := range r.All() {
		ids = append(ids, s.ID)
	}
	if strings.Join(ids, ",") != "alpha,mid,zeta" {
		t.Fatalf("顺序不对：%v", ids)
	}
}

// 同一 ID 重复注册必须报错，并且保留先到的那一份。
// 后一份静默覆盖会让用户配置里保存的方法悄悄换成另一个实现。
func TestDuplicateIDKeepsFirst(t *testing.T) {
	r := NewRegistry()
	first := validStrategy("dup")
	first.Name = "先到的"
	second := validStrategy("dup")
	second.Name = "后到的"
	if err := r.Register(first); err != nil {
		t.Fatalf("第一次注册失败：%v", err)
	}
	err := r.Register(second)
	if err == nil {
		t.Fatal("重复 ID 没有报错")
	}
	if !strings.Contains(err.Error(), "dup") {
		t.Fatalf("错误里没有 ID：%v", err)
	}
	if got, _ := r.Get("dup"); got.Name != "先到的" {
		t.Fatalf("后一份把先到的覆盖了：%q", got.Name)
	}
	if len(r.Errors()) != 1 {
		t.Fatalf("重复注册要攒一条错误，实得 %d", len(r.Errors()))
	}
}

func TestShapeRejections(t *testing.T) {
	cases := []struct {
		name   string
		strat  func() Strategy
		wantIn string
	}{
		{"空 ID", func() Strategy { s := validStrategy(""); return s }, "ID 不合法"},
		{"大写 ID", func() Strategy { return validStrategy("GitHub") }, "ID 不合法"},
		{"点开头 ID", func() Strategy { return validStrategy(".x") }, "ID 不合法"},
		{"超长 ID", func() Strategy { return validStrategy(strings.Repeat("a", 65)) }, "ID 不合法"},
		{"空名称", func() Strategy { s := validStrategy("ok"); s.Name = "  "; return s }, "没有面向用户的名称"},
		{"空说明", func() Strategy { s := validStrategy("ok"); s.Description = ""; return s }, "没有面向用户的说明"},
		{"没有范围", func() Strategy { s := validStrategy("ok"); s.ScopeIDs = nil; return s }, "没有声明适用"},
		{"范围里有空值", func() Strategy { s := validStrategy("ok"); s.ScopeIDs = []string{"github", " "}; return s }, "空值"},
		{"范围重复", func() Strategy { s := validStrategy("ok"); s.ScopeIDs = []string{"github", "github"}; return s }, "重复"},
		{"没有能力", func() Strategy { s := validStrategy("ok"); s.Caps = 0; return s }, "没声明任何能力"},
		{"未知能力位", func() Strategy { s := validStrategy("ok"); s.Caps |= 1 << 20; return s }, "未知能力位"},
		{"缺候选取得能力", func() Strategy { s := validStrategy("ok"); s.Caps = CapabilitySystemDNSOnly; return s }, "没有候选取得能力"},
		{"没有实现", func() Strategy { s := validStrategy("ok"); s.Lookup = nil; return s }, "没有实现"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRegistry()
			err := r.Register(tc.strat())
			if err == nil {
				t.Fatal("该拒的没拒")
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("错误文本 %q 里没有 %q", err.Error(), tc.wantIn)
			}
			if len(r.All()) != 0 {
				t.Fatal("不合格的描述符进了表")
			}
			if len(r.Errors()) != 1 {
				t.Fatalf("注册错误要攒下来给装配层，实得 %d 条", len(r.Errors()))
			}
		})
	}
}

func TestForScopeAndIDs(t *testing.T) {
	r := NewRegistry()
	a := validStrategy("a")
	a.ScopeIDs = []string{"github", "steam"}
	b := validStrategy("b")
	b.ScopeIDs = []string{"steam"}
	if err := r.Register(a); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(b); err != nil {
		t.Fatal(err)
	}
	if got := r.ForScope("github"); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("github 范围该只有 a：%v", got)
	}
	if got := r.ForScope("steam"); len(got) != 2 {
		t.Fatalf("steam 范围该有两个：%v", got)
	}
	if got := r.ForScope("不存在"); len(got) != 0 {
		t.Fatalf("没有匹配范围时应返回空：%v", got)
	}
	if ids := r.IDs(); len(ids) != 2 || !ids["a"] || !ids["b"] {
		t.Fatalf("IDs 不对：%v", ids)
	}
}

// 描述符里的范围必须是产品目录里真有的，写错要当场报——
// 否则范围选择器里会出现点不到的范围。
func TestUnknownScopes(t *testing.T) {
	r := NewRegistry()
	s := validStrategy("a")
	s.ScopeIDs = []string{"github", "not-in-catalog"}
	if err := r.Register(s); err != nil {
		t.Fatal(err)
	}
	errs := r.UnknownScopes(map[string]bool{"github": true})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "not-in-catalog") {
		t.Fatalf("没认出未知范围：%v", errs)
	}
	if len(r.UnknownScopes(map[string]bool{"github": true, "not-in-catalog": true})) != 0 {
		t.Fatal("范围都在目录里时不该报错")
	}
}

// 取出去的快照改了不能回头影响表里的内容。
func TestReturnedStrategiesAreClones(t *testing.T) {
	r := NewRegistry()
	s := validStrategy("a")
	s.ScopeIDs = []string{"github"}
	if err := r.Register(s); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get("a")
	got.ScopeIDs[0] = "被改坏了"
	again, _ := r.Get("a")
	if again.ScopeIDs[0] != "github" {
		t.Fatalf("表里的范围被外部改动了：%v", again.ScopeIDs)
	}
	for _, one := range r.All() {
		one.ScopeIDs[0] = "又被改坏了"
	}
	third, _ := r.Get("a")
	if third.ScopeIDs[0] != "github" {
		t.Fatalf("All 的快照不是副本：%v", third.ScopeIDs)
	}
}

func TestErrorsSliceIsCopy(t *testing.T) {
	r := NewRegistry()
	r.Register(validStrategy(""))
	first := r.Errors()
	first[0] = nil
	if r.Errors()[0] == nil {
		t.Fatal("Errors 返回的是内部切片")
	}
}

// 真实注册表的基线：DNS 方法覆盖除 github 外的全部范围，五个 GitHub 方法（三源 + 全部 + 已有储存）只覆盖 github，
// 注册零错误、范围和产品目录完全对齐。这一条同时也是 init 的自检——注册静默失败会在这里暴露。
func TestGlobalRegistryBaseline(t *testing.T) {
	if errs := Errors(); len(errs) != 0 {
		t.Fatalf("init 注册有错误：%v", errs)
	}
	all := All()
	if len(all) != 6 {
		t.Fatalf("应有 6 个方法（DNS + 三源 + 全部 + 已有储存），实得 %d：%v", len(all), strategyIDs(all))
	}
	s, ok := Get(StrategyIDDNSHosts)
	if !ok {
		t.Fatalf("DNS 方法不在表里")
	}
	if s.Name != "自动查找可用 IP" {
		t.Fatalf("用户可见名称不对：%q", s.Name)
	}
	if !s.Caps.has(CapabilitySystemDNSOnly) {
		t.Fatal("DNS 方法必须声明只走系统 DNS")
	}
	if s.Lookup == nil {
		t.Fatal("没有实现")
	}

	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	scopes := cat.ScopeIDs()
	// DNS 方法声明的是「除 github 外的全部范围」：GitHub 的候选来自公共源和本机储存，不走 DNS。
	if len(s.ScopeIDs) != len(scopes)-1 {
		t.Fatalf("适用范围数 %d，应等于目录范围数 %d 减去 github", len(s.ScopeIDs), len(scopes))
	}
	if len(scopes) != 11 {
		t.Fatalf("目录应有 11 个范围，实得 %d", len(scopes))
	}
	if slices.Contains(s.ScopeIDs, "github") {
		t.Fatalf("DNS 方法不该再声明 github 范围")
	}
	for _, id := range s.ScopeIDs {
		if !scopes[id] {
			t.Fatalf("策略声明的范围 %s 不在目录里", id)
		}
	}
	if errs := UnknownScopes(scopes); len(errs) != 0 {
		t.Fatalf("范围和目录对不上：%v", errs)
	}
	// github 范围：三源 + 全部 + 已有储存，共 5 个（DNS 解析法已按拍板移出 GitHub）；
	// 别的范围（如 steam）只有 DNS。
	if got := ForScope("github"); len(got) != 5 {
		t.Fatalf("github 范围应有 5 个方法，实得 %v", strategyIDs(got))
	}
	for _, s := range ForScope("github") {
		if s.ID == StrategyIDDNSHosts {
			t.Fatalf("GitHub 范围不该再列 DNS 解析法")
		}
	}
	if got := ForScope("steam"); len(got) != 1 || got[0].ID != StrategyIDDNSHosts {
		t.Fatalf("steam 范围该只有 DNS 方法，实得 %v", strategyIDs(got))
	}
	if _, ok := Get("不存在的Id"); ok {
		t.Fatal("查不到 ID 却返回了真")
	}
}

func strategyIDs(list []Strategy) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.ID
	}
	return out
}
