package config

import (
	"os"
	"path/filepath"
	"testing"
)

// 旧档搬家：新位置没有而旧位置有 → 整体复制，原件保留；新位置已有 → 不动。
func TestMigrateLegacy(t *testing.T) {
	newPath := filepath.Join(t.TempDir(), "cfg", "config.json")

	// 旧档存在：搬过来，内容一致，原件还在。
	legacy := filepath.Join(t.TempDir(), "data", "github-net", "config", "config.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"schemaVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Dir(filepath.Dir(filepath.Dir(legacy))))
	MigrateLegacy(newPath)
	raw, err := os.ReadFile(newPath)
	if err != nil || string(raw) != `{"schemaVersion":1}` {
		t.Fatalf("旧档该被搬过来（err=%v，内容=%q）", err, raw)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("原件该保留当备份: %v", err)
	}

	// 新位置已有：不再覆盖。
	if err := os.WriteFile(newPath, []byte(`{"schemaVersion":1,"kept":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"schemaVersion":1,"old":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	MigrateLegacy(newPath)
	raw, _ = os.ReadFile(newPath)
	if string(raw) != `{"schemaVersion":1,"kept":true}` {
		t.Fatalf("新位置已有配置时不该被覆盖，实际 %q", raw)
	}

	// 旧档不存在：什么都不发生，也不报错。
	MigrateLegacy(filepath.Join(t.TempDir(), "another", "config.json"))
}
