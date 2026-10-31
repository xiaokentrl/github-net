package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/accelerator/strategies"
	"github.com/xiaokentrl/github-net/internal/catalog"
	"github.com/xiaokentrl/github-net/internal/config"
)

// 策略文件是靠 init 登记进表的，那一刻没有调用方能接住错误。
// 装配层既然开始用这张表当配置校验集合，就必须顺手核对它的形状：
// 注册没报错、声明的范围都在目录里、每个目录范围至少有一个方法可选。
// 这三条任何一条不成立的运行时表现都是静默的：要么一个方法都选不到，
// 要么选了存不进去，而界面上只会显示「保存失败」这种没头没尾的话。
func TestRegistryAssemblyShapeIsSound(t *testing.T) {
	if errs := strategies.Errors(); len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("加速方法注册失败：%v", err)
		}
	}
	cat, err := catalog.Load()
	if err != nil {
		t.Fatalf("产品目录读不出来：%v", err)
	}
	scopeIDs := cat.ScopeIDs()
	if unknown := strategies.UnknownScopes(scopeIDs); len(unknown) != 0 {
		for _, err := range unknown {
			t.Errorf("%v", err)
		}
	}
	if len(strategies.IDs()) == 0 {
		t.Fatal("注册表是空的：任何默认加速方法都存不进去")
	}
	for scopeID := range scopeIDs {
		if len(strategies.ForScope(scopeID)) == 0 {
			t.Errorf("范围 %s 没有任何可选方法，选择区会是空的", scopeID)
		}
	}
}

// 配置服务拿到的校验集合必须真的等于注册表，不能是抄来的第二份名单。
func TestSettingsStoreValidatesAgainstTheRegistry(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, scopeID := range []string{"github", "steam"} {
		if !cat.ScopeIDs()[scopeID] {
			t.Fatalf("目录里没有范围 %s，这条用例的前提没了", scopeID)
		}
		for _, method := range strategies.ForScope(scopeID) {
			store := config.NewStore(filepath.Join(t.TempDir(), "config.json"), cat.ScopeIDs(), strategies.IDs(), cat.CategoryIDs())
			if _, err := store.Save(config.Patch{
				DefaultStrategyByScope: &map[string]string{scopeID: method.ID},
			}, 0, "m-"+scopeID+"-"+method.ID); err != nil {
				t.Errorf("范围 %s 选方法 %s 应被接受：%v", scopeID, method.ID, err)
			}
		}
	}
}

// 目录里的每个主菜单都必须真能存进「启动后自动检测」清单。
// 这条钉的是 CategoryIDs 给的值确实是菜单 ID：装配传错集合时，
// 界面上的表现是「勾了主菜单但保存失败」，看不出根因，所以在这一层就红。
func TestEveryCatalogMenuIsSavableAsAutoDetectMenu(t *testing.T) {
	cat, err := catalog.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range cat.Categories {
		list := []string{category.ID}
		store := config.NewStore(filepath.Join(t.TempDir(), "config.json"),
			cat.ScopeIDs(), strategies.IDs(), cat.CategoryIDs())
		got, err := store.Save(config.Patch{AutoDetectMenus: &list}, 0, "m-"+category.ID)
		if err != nil {
			t.Errorf("主菜单 %s 存不进自动检测清单：%v", category.ID, err)
			continue
		}
		if len(got.Startup.AutoDetectMenus) != 1 || got.Startup.AutoDetectMenus[0] != category.ID {
			t.Errorf("主菜单 %s 存进去变了样：%+v", category.ID, got.Startup.AutoDetectMenus)
		}
	}
	// 目录之外的 ID 必须拒：集合给宽了就是「什么字符串都收」。
	store := config.NewStore(filepath.Join(t.TempDir(), "config.json"),
		cat.ScopeIDs(), strategies.IDs(), cat.CategoryIDs())
	unknown := []string{"no-such-menu"}
	if _, err := store.Save(config.Patch{AutoDetectMenus: &unknown}, 0, "m-unknown"); err == nil {
		t.Error("目录之外的主菜单应被拒绝")
	}
}

// 装配层必须把目录里的主菜单集合真的交给配置服务。
// 判据取那一行的整行形状再做相等比较，不用「包含」：
// 包含会被「少传一个集合、但那几个字还留在注释里」骗过去。
func TestSettingsStoreAssemblyPassesThreeValidationSets(t *testing.T) {
	src := readAt(t, repoRoot(t), "internal/app/app.go")
	line := ""
	for _, candidate := range strings.Split(src, "\n") {
		if strings.Contains(candidate, "config.NewStore(path,") {
			line = strings.TrimSpace(candidate)
		}
	}
	if line == "" {
		t.Fatal("app.go 里找不到装配配置服务那一行，这条门的前提没了")
	}
	const want = "return config.NewStore(path, c.ScopeIDs(), strategies.IDs(), c.CategoryIDs()), nil"
	if line != want {
		t.Errorf("配置服务的校验集合不是「目录范围 + 注册表方法 + 目录主菜单」这三份：\n got: %s\nwant: %s", line, want)
	}
}
