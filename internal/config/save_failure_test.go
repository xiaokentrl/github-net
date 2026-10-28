package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 这一档管的是「磁盘出问题的时候，程序绝不能把用户的配置写坏」。
// 四条判据：写失败要报 ErrSaveFailed；内存里的版本不许动；磁盘上的既有内容不许动；
// 目录里不许留下半成品临时文件。
//
// 注入故障的手法一律不用「把目录改成只读」：CI 的容器里跑的是 root，
// root 无视目录权限，那样的用例在 CI 上是假绿。下面用的是路径本身不可用
// （同名条目挡住），root 也一样失败。

// residue 列目录，返回除了已知条目之外的东西；临时文件残留会在这里露出来。
func residue(t *testing.T, dir string, known ...string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var leftovers []string
	for _, e := range entries {
		if !contains(known, e.Name()) {
			leftovers = append(leftovers, e.Name())
		}
	}
	return leftovers
}

// TestSaveFailsWhenConfigDirIsReplacedByFile 对应总纲的「权限与磁盘错误」：
// 配置目录位置被一个普通文件占了（权限改坏、被外部程序动过都会这样），
// 保存必须失败，而且除了报错之外什么都不改变。
func TestSaveFailsWhenConfigDirIsReplacedByFile(t *testing.T) {
	path := newPath(t)
	dir := filepath.Dir(path)
	// 上一版留在被占的位置上，用来验证失败不会把它冲掉。
	const marker = "挡在这里的旧东西"
	if err := os.WriteFile(dir, []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewStore(path, nil, nil)
	if _, err := s.Save(Patch{Theme: str("forest")}, 0, "m1"); err == nil {
		t.Fatal("写不进去还报成功，界面上就会出现「已保存」的假象")
	} else if !errors.Is(err, ErrSaveFailed) {
		t.Errorf("错误应为 ErrSaveFailed，得到 %v", err)
	}

	after, err := os.ReadFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != marker {
		t.Errorf("占位的旧内容被改动了: %q", after)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("失败的保存却把配置文件写出来了")
	}
	got, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 0 || got.Appearance.Theme != "light" {
		t.Errorf("失败的保存在内存里生效了: revision=%d %+v", got.Revision, got.Appearance)
	}
}

// TestFailedSaveDoesNotConsumeMutationID 接着上一条：幂等键只在写成功那一刻记账。
// 否则磁盘恢复后重试同一个键，会被当成「已经保存过」而静默丢掉这次改动。
func TestFailedSaveDoesNotConsumeMutationID(t *testing.T) {
	path := newPath(t)
	dir := filepath.Dir(path)
	if err := os.WriteFile(dir, []byte("占位"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path, nil, nil)
	if _, err := s.Save(Patch{Theme: str("forest")}, 0, "m1"); !errors.Is(err, ErrSaveFailed) {
		t.Fatalf("路径不可用应报写盘失败，得到 %v", err)
	}
	// 把占位的文件换成真目录，等价于磁盘问题解决之后重试。
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	saved, err := s.Save(Patch{Theme: str("forest")}, 0, "m1")
	if err != nil {
		t.Fatalf("磁盘恢复后同一幂等键应能重试成功: %v", err)
	}
	if saved.Revision != 1 || saved.Appearance.Theme != "forest" {
		t.Errorf("重试没有真正落下去: revision=%d %+v", saved.Revision, saved.Appearance)
	}
	// 再拿同一个键提交别的内容，必须被幂等拦下来，不能二次写入。
	again, err := s.Save(Patch{Theme: str("midnight")}, 1, "m1")
	if err != nil {
		t.Fatal(err)
	}
	if again.Appearance.Theme != "forest" || again.Revision != 1 {
		t.Errorf("成功之后同一幂等键又写了一次: %+v", again.Appearance)
	}
}

// TestAtomicReplaceInterruptedKeepsPreviousVersion 对应「原子替换中断」：
// 临时文件已经写完、最后改名这一步被挡住（等价于断电或被杀进程落在
// fsync 与 rename 之间）时，占着目标的东西必须还在，
// 而且临时文件不能残留成第二个配置。
func TestAtomicReplaceInterruptedKeepsPreviousVersion(t *testing.T) {
	path := newPath(t)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeAtomic(path, Default()); err == nil {
		t.Fatal("替换目标是个目录，改名应该失败")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Errorf("挡路的目录被替换掉了，原子性没保住: %v", info.Mode())
	}
	if leftovers := residue(t, dir, "config.json", "keep.txt"); len(leftovers) != 0 {
		t.Errorf("替换失败后临时文件没清掉: %v", leftovers)
	}
}

// TestFailedQuarantineKeepsOriginalFile 补齐坏配置的另一条退路：原件读不出可用内容、
// 隔离副本又写不进（目标位置被占）时，程序不能报错卡死，要拿默认值继续跑、
// 说明情况，并且绝不删掉原件——那是用户唯一的原始现场。
func TestFailedQuarantineKeepsOriginalFile(t *testing.T) {
	path := newPath(t)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const broken = "{ 这不是 JSON"
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	// 隔离副本的位置 <path>.bad 被一个目录占着：root 也写不进去。
	if err := os.MkdirAll(BackupPath(path), 0o700); err != nil {
		t.Fatal(err)
	}

	s := NewStore(path, nil, nil)
	got, err := s.Snapshot()
	if err != nil {
		t.Fatalf("备份失败不该让程序起不来: %v", err)
	}
	if got.Revision != 0 || got.Network.Profile != ProfileLow {
		t.Errorf("没回到默认值: %+v", got.Network)
	}
	notice := s.Notice()
	if notice == "" {
		t.Fatal("必须告诉用户配置不可用")
	}
	if !strings.Contains(notice, "默认设置") {
		t.Errorf("说明里没讲清接下来按什么跑: %q", notice)
	}
	if !strings.Contains(notice, "备份") {
		t.Errorf("备份也失败了，说明里不能只讲备份成功: %q", notice)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("原件被删掉了，用户没有任何现场可查: %v", err)
	}
	if string(raw) != broken {
		t.Errorf("原件内容被动过: %q", raw)
	}
	// 保存同样不能把原件写坏，也不能在目录里留下第二个配置。
	if _, err := s.Save(Patch{Theme: str("forest")}, 0, "m1"); err != nil {
		t.Fatalf("坏配置回退默认值后应能正常保存: %v", err)
	}
	if leftovers := residue(t, dir, "config.json", "config.json.bad"); len(leftovers) != 0 {
		t.Errorf("保存后目录里留下半成品: %v", leftovers)
	}
}

// TestQuarantineBackupIsUserOnly 对应清单里的「敏感信息」一项：隔离副本和配置文件
// 一样只能当前用户读写，权限不能因为换个文件名就放宽。
func TestQuarantineBackupIsUserOnly(t *testing.T) {
	path := newPath(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ 这不是 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path, nil, nil)
	if _, err := s.Snapshot(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(BackupPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("隔离副本权限 = %o，期望 600", perm)
	}
	if s.Notice() == "" {
		t.Error("隔离成功后仍要说明原件去了哪里")
	}
}

// TestWriteCreatesUserOnlyDirectory 补齐另一侧：目录是程序自己建的时候，
// 也只能当前用户可进入，不能顺手给同组和其他人开权限。
func TestWriteCreatesUserOnlyDirectory(t *testing.T) {
	path := newPath(t)
	s := NewStore(path, nil, nil)
	if _, err := s.Save(Patch{Theme: str("oled")}, 0, "m1"); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("配置目录权限 = %o，期望 700", perm)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("配置文件权限 = %o，期望 600", perm)
	}
}
