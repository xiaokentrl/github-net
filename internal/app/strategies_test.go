package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
)

// newStrategyService 建一个带临时配置文件的服务，并把方法注册表的读取接缝交给用例。
// allowed 是配置服务自己的校验集合，用例可以往里放假方法 ID，用来证明「配置层收得下、
// 命令层必须拦」那道门；但假方法绝不注册进全局表——同 ID 重复注册会把错误写进
// 全局 Errors()，把装配层那道注册门（registry_assembly_test.go）照脏。
func newStrategyService(t *testing.T, table strategyTable, allowed ...string) (*Service, string) {
	t.Helper()
	c := mustCatalog(t)
	ids := strategies.IDs()
	for _, id := range allowed {
		ids[id] = true
	}
	path := filepath.Join(t.TempDir(), "config", "config.json")
	return &Service{
		appName:        "GitHub Net",
		version:        "0.1.0",
		productCatalog: c,
		strategyTable:  table,
		settings:       config.NewStore(path, c.ScopeIDs(), ids),
	}, path
}

// fakeTable 给一张只属于这条用例的方法表：list 是「范围 → 可选方法」，byID 是「方法 ID → 描述符」。
func fakeTable(t *testing.T, list map[string][]strategies.Strategy, byID map[string]strategies.Strategy, errs ...error) strategyTable {
	t.Helper()
	for scopeID, methods := range list {
		for _, method := range methods {
			// 假表自己得自洽，否则用例测的是拼进来的数据而不是被测代码。
			if got, ok := byID[method.ID]; !ok || !strategyApplies(got, scopeID) {
				t.Fatalf("假表不自洽：范围 %s 列了方法 %s，但方法表里没它或它不适用", scopeID, method.ID)
			}
		}
	}
	return strategyTable{
		get: func(id string) (strategies.Strategy, bool) {
			s, ok := byID[id]
			return s, ok
		},
		forScope: func(scopeID string) []strategies.Strategy {
			out := append([]strategies.Strategy{}, list[scopeID]...)
			sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
			return out
		},
		errs: func() []error { return append([]error{}, errs...) },
	}
}

func fakeStrategy(id, name string, scopeIDs ...string) strategies.Strategy {
	return strategies.Strategy{
		ID:          id,
		ScopeIDs:    scopeIDs,
		Name:        name,
		Description: "用例用的假方法",
		Caps:        strategies.CapabilityCandidateLookup | strategies.CapabilitySystemDNSOnly,
		Lookup:      func(context.Context, strategies.Input) (strategies.Output, error) { return strategies.Output{}, nil },
	}
}

// scopesWithMethods 取目录里前 want 个「注册表中有可选方法」的加速范围。
// 现在真表里只有一个方法，且它声明适用全部范围，所以这个前提本来就该成立；
// 哪天方法收窄了适用范围，这条前提要当场红，而不是让用例悄悄跳过。
func scopesWithMethods(t *testing.T, c *catalog.Catalog, want int) []string {
	t.Helper()
	var out []string
	for _, cat := range c.Categories {
		if len(strategies.ForScope(cat.ScopeID)) == 0 {
			continue
		}
		seen := false
		for _, got := range out {
			if got == cat.ScopeID {
				seen = true
			}
		}
		if !seen {
			out = append(out, cat.ScopeID)
		}
		if len(out) >= want {
			return out
		}
	}
	t.Fatalf("目录里带可选方法的范围只有 %d 个，用例要 %d 个：前提没了", len(out), want)
	return nil
}

func optionIDs(snap StrategySnapshot) []string {
	out := make([]string, 0, len(snap.Options))
	for _, opt := range snap.Options {
		out = append(out, opt.ID)
	}
	return out
}

func publicCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("期望错误，实际返回 nil")
	}
	var publicErr *Error
	if !errors.As(err, &publicErr) {
		t.Fatalf("错误没带稳定码：%v", err)
	}
	return publicErr.Code
}

func TestGetStrategiesListsApplicableMethodsAndSavedChoice(t *testing.T) {
	svc, _ := newStrategyService(t, strategyTable{})
	scopeID := scopesWithMethods(t, svc.productCatalog, 1)[0]

	snap, err := svc.GetStrategies(scopeID)
	if err != nil {
		t.Fatalf("列方法应成功：%v", err)
	}
	if snap.ScopeID != scopeID || snap.Revision != 0 {
		t.Errorf("快照头部不对：%+v", snap)
	}
	if len(snap.Options) == 0 {
		t.Fatalf("范围 %s 一个方法都没列出来", scopeID)
	}
	for _, opt := range snap.Options {
		if !strategyApplies(strategies.Strategy{ScopeIDs: opt.ScopeIDs}, scopeID) {
			t.Errorf("方法 %s 声明的范围 %v 不含 %s，不该出现在这个范围的选择区", opt.ID, opt.ScopeIDs, scopeID)
		}
		if opt.Name == "" || opt.Description == "" {
			t.Errorf("方法 %s 缺面向用户的文案", opt.ID)
		}
		// 那一个真方法三个能力全带上，所以三位都该是 true；能力位没摊平就会在这里红。
		if !opt.CanLookup || !opt.KeepsTLSName || !opt.SystemDNS {
			t.Errorf("方法 %s 的能力位没摊平：%+v", opt.ID, opt)
		}
		if opt.Selected {
			t.Errorf("还没保存过选择，%s 不该被标成已选", opt.ID)
		}
	}
	ids := optionIDs(snap)
	sorted := append([]string{}, ids...)
	sort.Strings(sorted)
	if fmt.Sprint(ids) != fmt.Sprint(sorted) {
		t.Errorf("列表不是按方法 ID 排序，界面没法稳定渲染：%v", ids)
	}
	if !snap.SavedUsable || snap.SavedID != "" || snap.SavedNote != "" {
		t.Errorf("没选过方法不该算坏选择：%+v", snap)
	}
	if len(snap.Notices) != 0 {
		t.Errorf("注册表健康时不该有提示：%v", snap.Notices)
	}
}

func TestGetStrategiesMarksTheSavedChoice(t *testing.T) {
	svc, _ := newStrategyService(t, strategyTable{})
	scopeID := scopesWithMethods(t, svc.productCatalog, 1)[0]
	chosen := strategies.ForScope(scopeID)[0].ID

	if _, err := svc.settings.Save(
		config.Patch{DefaultStrategyByScope: &map[string]string{scopeID: chosen}}, 0, "seed"); err != nil {
		t.Fatalf("预置已保存选择失败：%v", err)
	}

	snap, err := svc.GetStrategies(scopeID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.SavedID != chosen || !snap.SavedUsable || snap.SavedNote != "" {
		t.Errorf("已保存的选择没读回来：%+v", snap)
	}
	if snap.Revision != 1 {
		t.Errorf("快照里的配置版本 = %d，期望 1（下一次保存要带它）", snap.Revision)
	}
	var marked []string
	for _, opt := range snap.Options {
		if opt.Selected {
			marked = append(marked, opt.ID)
		}
	}
	if len(marked) != 1 || marked[0] != chosen {
		t.Errorf("已选标记 = %v，期望只有 %s", marked, chosen)
	}
}

func TestGetStrategiesReturnsEmptyArrayWhenScopeHasNoMethod(t *testing.T) {
	// 真表上这条不可达：装配层要求每个目录范围至少有一个可选方法。只有注入才测得到形状。
	scopeID := mustCatalog(t).Categories[0].ScopeID
	svc, _ := newStrategyService(t, fakeTable(t,
		map[string][]strategies.Strategy{}, map[string]strategies.Strategy{}))

	snap, err := svc.GetStrategies(scopeID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Options == nil {
		t.Fatal("零个方法时 options 是 nil，前端会撞上 null")
	}
	if len(snap.Options) != 0 {
		t.Errorf("这个范围不该有可选方法，实际 %v", optionIDs(snap))
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"options":[]`) || !strings.Contains(string(data), `"notices":[]`) {
		t.Errorf("JSON 里没有空数组形状：%s", data)
	}
	if strings.Contains(string(data), "null") {
		t.Errorf("JSON 里出现 null：%s", data)
	}
}

func TestGetStrategiesRefusesUnknownScope(t *testing.T) {
	svc, _ := newStrategyService(t, strategyTable{})
	for _, arg := range []string{"", "   ", "no-such-scope"} {
		snap, err := svc.GetStrategies(arg)
		if err == nil {
			t.Fatalf("范围 %q 不在目录里，却列出了方法", arg)
		}
		if code := publicCode(t, err); code != CodeUnknownScope {
			t.Errorf("范围 %q 的错误码 = %s，期望 %s", arg, code, CodeUnknownScope)
		}
		if snap.Options == nil || snap.Notices == nil {
			t.Errorf("失败路径的快照形状不该是 nil：%+v", snap)
		}
	}
}

func TestGetStrategiesReportsCatalogFailure(t *testing.T) {
	svc := &Service{productCatalogErr: fmt.Errorf("%w: 目录坏了", catalog.ErrInvalid)}
	snap, err := svc.GetStrategies("github")
	if publicCode(t, err) != CodeCatalogInvalid {
		t.Errorf("错误码不对，期望 %s", CodeCatalogInvalid)
	}
	if snap.Options == nil || snap.Notices == nil {
		t.Errorf("目录读不出来也该给空数组形状：%+v", snap)
	}
}

func TestGetStrategiesReportsSavedChoiceNoLongerRegistered(t *testing.T) {
	// 配置里存着一个 ID，但注册表里已经没有它了（方法被删名，或那份配置来自别的构建）。
	scopeID := mustCatalog(t).Categories[0].ScopeID
	live := fakeStrategy("live.one", "还在的方法", scopeID)
	svc, _ := newStrategyService(t, fakeTable(t,
		map[string][]strategies.Strategy{scopeID: {live}},
		map[string]strategies.Strategy{"live.one": live}), "gone.one")
	if _, err := svc.settings.Save(
		config.Patch{DefaultStrategyByScope: &map[string]string{scopeID: "gone.one"}}, 0, "seed"); err != nil {
		t.Fatal(err)
	}

	snap, err := svc.GetStrategies(scopeID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.SavedID != "gone.one" || snap.SavedUsable {
		t.Errorf("该报「不能用」：%+v", snap)
	}
	if !strings.Contains(snap.SavedNote, "不在注册表里") {
		t.Errorf("说明没落到「方法没了」这一种：%q", snap.SavedNote)
	}
	// 关键一条：不能用不等于可以替用户挑一个。
	for _, opt := range snap.Options {
		if opt.Selected {
			t.Errorf("已保存的选择不在列表里，却把 %s 标成已选——等于私自改了选择", opt.ID)
		}
	}
}

func TestGetStrategiesReportsSavedChoiceFromAnotherScope(t *testing.T) {
	// 方法在注册表里、配置服务的允许集合也收得下它，但它声明的适用范围里没有本范围。
	// 配置层只查两个集合，看不出这一层，所以「不能用」只能由命令层说。
	other := fakeStrategy("only.steam", "只给 Steam", "steam")
	live := fakeStrategy("live.one", "还在的方法", "github")
	svc, _ := newStrategyService(t, fakeTable(t,
		map[string][]strategies.Strategy{"github": {live}},
		map[string]strategies.Strategy{"only.steam": other, "live.one": live}), "only.steam")
	if _, err := svc.settings.Save(
		config.Patch{DefaultStrategyByScope: &map[string]string{"github": "only.steam"}}, 0, "seed"); err != nil {
		t.Fatalf("配置层收得下跨范围的方法（这是前提，不是结论）：%v", err)
	}

	snap, err := svc.GetStrategies("github")
	if err != nil {
		t.Fatal(err)
	}
	if snap.SavedUsable {
		t.Errorf("存的是 steam 的方法、看的是 github，却判成能用：%+v", snap)
	}
	if !strings.Contains(snap.SavedNote, "不适用") {
		t.Errorf("说明没落到「适用范围」这一条：%q", snap.SavedNote)
	}
	for _, opt := range snap.Options {
		if opt.Selected {
			t.Errorf("跨范围的选择不能顶着本范围的方法显示成已选：%+v", opt)
		}
	}
}

func TestGetStrategiesCarriesRegistryNotices(t *testing.T) {
	scopeID := mustCatalog(t).Categories[0].ScopeID
	table := fakeTable(t,
		map[string][]strategies.Strategy{},
		map[string]strategies.Strategy{},
		errors.New("加速方法 broken.one 没有实现"),
		errors.New("加速方法 broken.two 没有声明适用的加速范围"))

	svc, _ := newStrategyService(t, table)
	snap, err := svc.GetStrategies(scopeID)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"加速方法 broken.one 没有实现", "加速方法 broken.two 没有声明适用的加速范围"}
	if fmt.Sprint(snap.Notices) != fmt.Sprint(want) {
		t.Errorf("notices = %v，期望逐字带上注册表的失败原话 %v", snap.Notices, want)
	}
}

func TestSelectStrategySavesItsOwnScopeOnly(t *testing.T) {
	svc, _ := newStrategyService(t, strategyTable{})
	scopes := scopesWithMethods(t, svc.productCatalog, 2)
	first, second := scopes[0], scopes[1]
	firstID := strategies.ForScope(first)[0].ID
	secondID := strategies.ForScope(second)[0].ID

	snap, err := svc.SelectStrategy(first, firstID, 0, "m1")
	if err != nil {
		t.Fatalf("保存本范围的选择应成功：%v", err)
	}
	if snap.Revision != 1 || snap.SavedID != firstID || !snap.SavedUsable {
		t.Errorf("保存后的快照不对：%+v", snap)
	}
	if len(snap.Options) == 0 || !snap.Options[0].Selected {
		t.Errorf("保存后的列表没把新选择标成已选：%v", optionIDs(snap))
	}

	if _, err := svc.SelectStrategy(second, secondID, 1, "m2"); err != nil {
		t.Fatalf("第二个范围的保存应成功：%v", err)
	}
	cfg, err := svc.settings.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	// Patch 里这张表是整体替换：只改本范围那一个键，别的范围的选择不能被抹掉。
	got := cfg.Accelerator.DefaultStrategyByScope
	if len(got) != 2 || got[first] != firstID || got[second] != secondID {
		t.Errorf("两个范围的选择应各自保留，实际 %v", got)
	}
	if cfg.Revision != 2 {
		t.Errorf("两次保存应递增到 2，实际 %d", cfg.Revision)
	}
}

func TestSelectStrategyRefusesUnregisteredMethod(t *testing.T) {
	scopeID := mustCatalog(t).Categories[0].ScopeID
	live := fakeStrategy("live.one", "还在的方法", scopeID)
	svc, _ := newStrategyService(t, fakeTable(t,
		map[string][]strategies.Strategy{scopeID: {live}},
		map[string]strategies.Strategy{"live.one": live}), "gone.one")

	_, err := svc.SelectStrategy(scopeID, "gone.one", 0, "m1")
	if publicCode(t, err) != CodeStrategyUnavailable {
		t.Errorf("错误码不对，期望 %s", CodeStrategyUnavailable)
	}
	cfg, snapErr := svc.settings.Snapshot()
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	if cfg.Revision != 0 || len(cfg.Accelerator.DefaultStrategyByScope) != 0 {
		t.Errorf("被拒的保存动了配置：%v", cfg.Accelerator.DefaultStrategyByScope)
	}
	// 反证这道门只在命令层：同一个 ID 直接走配置服务是收得下的。
	if _, err := svc.settings.Save(
		config.Patch{DefaultStrategyByScope: &map[string]string{scopeID: "gone.one"}}, 0, "m-direct"); err != nil {
		t.Fatalf("配置服务本该收得下允许集合里的 ID，命令层这道门才有意义：%v", err)
	}
}

func TestSelectStrategyRefusesMethodThatDoesNotApply(t *testing.T) {
	other := fakeStrategy("only.steam", "只给 Steam", "steam")
	live := fakeStrategy("live.one", "还在的方法", "github")
	svc, _ := newStrategyService(t, fakeTable(t,
		map[string][]strategies.Strategy{"github": {live}},
		map[string]strategies.Strategy{"only.steam": other, "live.one": live}), "only.steam")

	_, err := svc.SelectStrategy("github", "only.steam", 0, "m1")
	var publicErr *Error
	if !errors.As(err, &publicErr) || publicErr.Code != CodeStrategyUnavailable {
		t.Fatalf("错误码 = %v，期望 %s", err, CodeStrategyUnavailable)
	}
	if !strings.Contains(publicErr.Message, "不适用") {
		t.Errorf("消息没落到「适用范围」这一条：%q", publicErr.Message)
	}
	cfg, snapErr := svc.settings.Snapshot()
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	if cfg.Revision != 0 || len(cfg.Accelerator.DefaultStrategyByScope) != 0 {
		t.Errorf("被拒的保存动了配置：%v", cfg.Accelerator.DefaultStrategyByScope)
	}
	// 反证：这个 ID 在注册表里、也在配置服务的允许集合里，配置层自己拦不住跨范围。
	if _, err := svc.settings.Save(
		config.Patch{DefaultStrategyByScope: &map[string]string{"github": "only.steam"}}, 0, "m-direct"); err != nil {
		t.Fatalf("配置服务收得下跨范围的方法，正是命令层要补的门：%v", err)
	}
}

func TestSelectStrategyKeepsSettingsConcurrencyRules(t *testing.T) {
	svc, _ := newStrategyService(t, strategyTable{})
	scopeID := scopesWithMethods(t, svc.productCatalog, 1)[0]
	chosen := strategies.ForScope(scopeID)[0].ID

	if _, err := svc.SelectStrategy(scopeID, chosen, 0, "m1"); err != nil {
		t.Fatal(err)
	}
	// 副本版本落后：别处已经改过，这次提交不能盖掉它。
	if publicCode(t, svcErr(svc.SelectStrategy(scopeID, chosen, 0, "m2"))) != CodeRevisionConflict {
		t.Errorf("版本落后应报 %s", CodeRevisionConflict)
	}
	// 没有幂等键：双击和重试会各写一次，一律拒。
	if publicCode(t, svcErr(svc.SelectStrategy(scopeID, chosen, 1, "   "))) != CodeInvalidSettings {
		t.Errorf("空幂等键应报 %s", CodeInvalidSettings)
	}
	// 同一个幂等键重放：不二次递增版本。
	snap, err := svc.SelectStrategy(scopeID, chosen, 1, "m3")
	if err != nil {
		t.Fatal(err)
	}
	if snap.Revision != 2 {
		t.Fatalf("首次保存应到 2，实际 %d", snap.Revision)
	}
	again, err := svc.SelectStrategy(scopeID, chosen, 1, "m3")
	if err != nil {
		t.Fatalf("同一个幂等键重放应成功：%v", err)
	}
	if again.Revision != 2 {
		t.Errorf("幂等重放二次递增了版本：%d", again.Revision)
	}
}

// svcErr 从 (快照, 错误) 里只取错误，好让 publicCode 直接接上。
func svcErr(_ StrategySnapshot, err error) error { return err }

func TestSelectStrategyRefusesBlankAndUnknownArguments(t *testing.T) {
	svc, _ := newStrategyService(t, strategyTable{})
	scopeID := scopesWithMethods(t, svc.productCatalog, 1)[0]

	for _, tc := range []struct{ scope, strategy, code string }{
		{"", "any", CodeUnknownScope},
		{"no-such-scope", "any", CodeUnknownScope},
		{scopeID, "", CodeStrategyUnavailable},
		{scopeID, "  \t ", CodeStrategyUnavailable},
	} {
		_, err := svc.SelectStrategy(tc.scope, tc.strategy, 0, "m-"+tc.code)
		if publicCode(t, err) != tc.code {
			t.Errorf("范围 %q、方法 %q 的错误码不对，期望 %s", tc.scope, tc.strategy, tc.code)
		}
		cfg, snapErr := svc.settings.Snapshot()
		if snapErr != nil {
			t.Fatal(snapErr)
		}
		if cfg.Revision != 0 || len(cfg.Accelerator.DefaultStrategyByScope) != 0 {
			t.Errorf("范围 %q、方法 %q 被拒后配置被改了：%v", tc.scope, tc.strategy, cfg.Accelerator.DefaultStrategyByScope)
		}
	}
}

func TestSelectStrategyReportsCatalogFailure(t *testing.T) {
	svc := &Service{productCatalogErr: fmt.Errorf("%w: 目录坏了", catalog.ErrInvalid)}
	if publicCode(t, svcErr(svc.SelectStrategy("github", "any", 0, "m1"))) != CodeCatalogInvalid {
		t.Errorf("目录读不出来时不该谈保存")
	}
}

// TestTwoStrategyCommandsAreBound 是形状门：这两条必须都在 Service 上、签名和契约一致，
// 前端才不会被一次悄悄的参数改动打歪（SelectStrategy 比契约原稿多两个设置并发参数）。
func TestTwoStrategyCommandsAreBound(t *testing.T) {
	svcType := reflect.TypeOf(&Service{})
	str := reflect.TypeOf("")
	i64 := reflect.TypeOf(int64(0))
	snapshot := reflect.TypeOf(StrategySnapshot{})
	errType := reflect.TypeOf((*error)(nil)).Elem()

	for _, want := range []struct {
		name string
		in   []reflect.Type
	}{
		{"GetStrategies", []reflect.Type{str}},
		{"SelectStrategy", []reflect.Type{str, str, i64, str}},
	} {
		method, ok := svcType.MethodByName(want.name)
		if !ok {
			t.Fatalf("少了绑给前端的命令 %s", want.name)
		}
		sig := method.Type
		// 方法值的第一个入参是接收者，所以期望参数表要加上它。
		if sig.NumIn() != len(want.in)+1 || sig.NumOut() != 2 {
			t.Errorf("%s 签名形状不对：入参 %d 出参 %d，期望 %d/2", want.name, sig.NumIn(), sig.NumOut(), len(want.in)+1)
			continue
		}
		for i, wantIn := range want.in {
			if got := sig.In(i + 1); got != wantIn {
				t.Errorf("%s 第 %d 个参数应为 %s，实际 %s", want.name, i, wantIn, got)
			}
		}
		if sig.Out(0) != snapshot || sig.Out(1) != errType {
			t.Errorf("%s 返回值应为 (%s, error)，实际 (%s, %s)", want.name, snapshot, sig.Out(0), sig.Out(1))
		}
	}
}

// TestStrategySnapshotJSONKeys 钉住对外字段名：改一个键等于同时改前端绑定和界面读取。
func TestStrategySnapshotJSONKeys(t *testing.T) {
	data, err := json.Marshal(StrategySnapshot{
		ScopeID: "github",
		Options: []StrategyOption{{ID: "a.one", Name: "甲", Description: "说明", ScopeIDs: []string{"github"}, CanLookup: true}},
		SavedID: "a.one", SavedUsable: true, Revision: 3, Notices: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonKeys(t, data); fmt.Sprint(got) != fmt.Sprint([]string{"notices", "options", "revision", "savedId", "savedUsable", "scopeId"}) {
		t.Errorf("快照字段名变了：%v", got)
	}
	option, err := json.Marshal(StrategyOption{ID: "a.one", Name: "甲", Description: "说明", ScopeIDs: []string{"github"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := jsonKeys(t, option); fmt.Sprint(got) != fmt.Sprint([]string{"canLookup", "description", "id", "keepsTlsHostname", "name", "scopeIds", "selected", "systemDnsOnly"}) {
		t.Errorf("方法行的字段名变了：%v", got)
	}
}

func jsonKeys(t *testing.T, data []byte) []string {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("不是 JSON 对象：%s", data)
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
