package app

import (
	"path/filepath"
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
			store := config.NewStore(filepath.Join(t.TempDir(), "config.json"), cat.ScopeIDs(), strategies.IDs())
			if _, err := store.Save(config.Patch{
				DefaultStrategyByScope: &map[string]string{scopeID: method.ID},
			}, 0, "m-"+scopeID+"-"+method.ID); err != nil {
				t.Errorf("范围 %s 选方法 %s 应被接受：%v", scopeID, method.ID, err)
			}
		}
	}
}
