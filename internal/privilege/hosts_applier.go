package privilege

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaokentrl/github-net/internal/hosts"
)

// HostsApplier 是白名单操作在服务侧的实现：备份、渲染、原子替换、重读验证、失败回滚。
// 它只认构造时给定的那个 Hosts 路径和数据目录，客户端传不来别的路径——
// 「不接受任意路径」就是在这里落地的。
type HostsApplier struct {
	hostsPath string
	// dataDir 存恢复元数据（recovery.json）和首次写入前的原文备份（backups/）。
	// root 服务用 /var/lib/github-net；测试传临时目录。
	dataDir string
	now     func() time.Time
}

func NewHostsApplier(hostsPath, dataDir string) *HostsApplier {
	return &HostsApplier{hostsPath: hostsPath, dataDir: dataDir, now: time.Now}
}

// 元数据与备份的固定文件名。备份最多一份当前有效恢复点：下一次「首次写入」会覆盖它。
const (
	metaFileName   = "recovery.json"
	backupDirName  = "backups"
	backupFileName = "hosts-original.bak"
)

var (
	// ErrHostsRead / ErrHostsWrite 是服务侧可展示的失败分类；原始原因留在 %w 里给日志。
	ErrHostsRead  = errors.New("读不到系统 Hosts")
	ErrHostsWrite = errors.New("写入系统 Hosts 失败")
	// ErrVerifyFailed 表示写完重读对不上：回滚已尝试，结果见错误说明。
	ErrVerifyFailed = errors.New("写入后校验没通过")
)

// Apply 把一个项目的一批映射写进 Hosts：算出新区段、备份（仅首次）、原子替换、重读校验、失败回滚。
// 返回的 error 都带可直接展示的中文说明；原始错误串在 %w 链里，不外发。
func (a *HostsApplier) Apply(itemID string, mappings []hosts.Mapping) error {
	content, meta, err := a.load()
	if err != nil {
		return err
	}
	if !meta.Exists() {
		// 第一次动笔：原文由这里记进账本（事务层只算不改原文），备份与还原都靠它。
		meta.Original = content
	}
	newMeta, newContent, err := hosts.Apply(meta, content, itemID, mappings)
	if err != nil {
		return fmt.Errorf("这一批映射没能合并进管理区段：%v", err)
	}
	return a.persist(meta, newMeta, newContent)
}

// RestoreItem 把一个项目退回它应用前的映射。
func (a *HostsApplier) RestoreItem(itemID string) error {
	content, meta, err := a.load()
	if err != nil {
		return err
	}
	if !meta.Exists() {
		meta.Original = content
	}
	newMeta, newContent, err := hosts.RestoreItem(meta, content, itemID)
	if err != nil {
		return fmt.Errorf("这个项目没能还原：%v", err)
	}
	return a.persist(meta, newMeta, newContent)
}

// RestoreAll 把全部还由工具管理的项目退回应用前状态，返回实际还原的项目数。
// 全部退光后元数据和备份一起清掉——那个恢复点已经没有要还原的东西了。
func (a *HostsApplier) RestoreAll() (int, error) {
	content, meta, err := a.load()
	if err != nil {
		return 0, err
	}
	if !meta.Exists() {
		meta.Original = content
	}
	newMeta, newContent, restored, err := hosts.RestoreAll(meta, content)
	if err != nil {
		return 0, fmt.Errorf("没能还原：%v", err)
	}
	if err := a.persist(meta, newMeta, newContent); err != nil {
		return 0, err
	}
	return len(restored), nil
}

// load 读 Hosts 原文和恢复元数据。元数据损坏时明确拒绝：不能拿半对的账本去动系统文件。
func (a *HostsApplier) load() (string, hosts.Recovery, error) {
	raw, err := os.ReadFile(a.hostsPath)
	if err != nil {
		return "", hosts.Recovery{}, fmt.Errorf("%w（%s）：%v", ErrHostsRead, a.hostsPath, err)
	}
	metaRaw, err := os.ReadFile(a.metaPath())
	if errors.Is(err, os.ErrNotExist) {
		return string(raw), hosts.Recovery{}, nil
	}
	if err != nil {
		return "", hosts.Recovery{}, fmt.Errorf("%w：读恢复元数据失败：%v", ErrHostsRead, err)
	}
	meta, err := hosts.UnmarshalMetadata(metaRaw)
	if err != nil {
		return "", hosts.Recovery{}, fmt.Errorf("恢复元数据读不出来，拒绝动 Hosts（%v）；删除 %s 可重置，但会失去还原能力", err, a.metaPath())
	}
	return string(raw), meta, nil
}

// persist 完成一次成功计算后的落盘三步：备份（仅首次）、原子替换、重读校验，失败即回滚。
// before 是事务前的元数据：回滚时元数据也要退回去，账实两边一致。
func (a *HostsApplier) persist(before, newMeta hosts.Recovery, newContent string) error {
	if err := a.backupOnce(before); err != nil {
		return err
	}
	if err := a.writeHosts(newContent); err != nil {
		return err
	}
	if err := a.verify(newMeta); err != nil {
		rollbackErr := a.rollback(before)
		if rollbackErr != nil {
			return fmt.Errorf("%w，而且回滚也失败了（%v）；系统 Hosts 可能停在中间状态，请尽快检查 %s", ErrVerifyFailed, rollbackErr, a.hostsPath)
		}
		return fmt.Errorf("%w：已回滚到写入前内容，这次修改没有生效", ErrVerifyFailed)
	}
	if !newMeta.Exists() {
		// 账本还是零值：这一笔没有记下任何东西（比如空账本上做全量还原），
		// 元数据不保存；若有陈旧文件一并清掉，别留一份对不上现状的账。
		_ = os.Remove(a.metaPath())
		return nil
	}
	if err := a.saveMeta(newMeta); err != nil {
		return fmt.Errorf("%w：内容已写入但元数据没能保存，还原能力受损：%v", ErrHostsWrite, err)
	}
	// 全部退光就没有要还原的东西了：清掉恢复点，别留一份永远用不上的旧账本。
	if !newMeta.InUse() && newMeta.Exists() {
		_ = os.Remove(a.metaPath())
		_ = os.Remove(a.backupPath())
	}
	return nil
}

// backupOnce 只在第一次写入时把原文留底；之后的写入原地更新元数据，不再动这份备份。
func (a *HostsApplier) backupOnce(before hosts.Recovery) error {
	if before.Exists() {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(a.dataDir, backupDirName), 0o700); err != nil {
		return fmt.Errorf("%w：建备份目录失败：%v", ErrHostsWrite, err)
	}
	if err := os.WriteFile(a.backupPath(), []byte(before.Original), 0o600); err != nil {
		return fmt.Errorf("%w：备份写不进去：%v", ErrHostsWrite, err)
	}
	return nil
}

// writeHosts 用「同目录临时文件 + rename」替换 Hosts，并保留原文件的权限位。
// rename 在同一文件系统内是原子的，读的一方要么看到旧文件、要么看到新文件，不会看到半截。
func (a *HostsApplier) writeHosts(content string) error {
	info, err := os.Stat(a.hostsPath)
	mode := os.FileMode(0o644)
	if err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(a.hostsPath), ".github-net-hosts-*")
	if err != nil {
		return fmt.Errorf("%w：临时文件建不出来：%v", ErrHostsWrite, err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.WriteString(content); err != nil {
		return fmt.Errorf("%w：临时文件写不进去：%v", ErrHostsWrite, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("%w：临时文件权限改不了：%v", ErrHostsWrite, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("%w：临时文件落不了盘：%v", ErrHostsWrite, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w：临时文件关不上：%v", ErrHostsWrite, err)
	}
	if err := os.Rename(tmpName, a.hostsPath); err != nil {
		return fmt.Errorf("%w：替换失败：%v", ErrHostsWrite, err)
	}
	return nil
}

// verify 重读落盘后的 Hosts，核对管理区段就是元数据记录的那批映射。
// 判读不了区段和「区段不是我们刚写的那批」都算失败。
func (a *HostsApplier) verify(meta hosts.Recovery) error {
	raw, err := os.ReadFile(a.hostsPath)
	if err != nil {
		return fmt.Errorf("%w：重读失败：%v", ErrVerifyFailed, err)
	}
	status, reason := hosts.Check(string(raw), meta)
	if status != hosts.CheckMatched && !(status == hosts.CheckClean && !meta.InUse()) {
		return fmt.Errorf("%w：%s", ErrVerifyFailed, reason)
	}
	return nil
}

// rollback 把 Hosts 写回事务前的内容，元数据一并退回。任何一步失败都如实上抛：
// 回滚失败必须大声说，不能装作「没事了」。
func (a *HostsApplier) rollback(before hosts.Recovery) error {
	if err := a.writeHosts(before.Original); err != nil {
		return err
	}
	if _, err := os.ReadFile(a.hostsPath); err != nil {
		return fmt.Errorf("回滚后重读失败：%v", err)
	}
	if before.Exists() {
		return a.saveMeta(before)
	}
	_ = os.Remove(a.metaPath())
	return nil
}

func (a *HostsApplier) metaPath() string { return filepath.Join(a.dataDir, metaFileName) }
func (a *HostsApplier) backupPath() string {
	return filepath.Join(a.dataDir, backupDirName, backupFileName)
}

// saveMeta 原子保存恢复元数据：同目录临时文件 + rename，和 Hosts 写入同一纪律。
func (a *HostsApplier) saveMeta(meta hosts.Recovery) error {
	if err := os.MkdirAll(a.dataDir, 0o700); err != nil {
		return fmt.Errorf("建数据目录失败：%v", err)
	}
	raw, err := meta.Marshal()
	if err != nil {
		return fmt.Errorf("元数据编不出来：%v", err)
	}
	tmp, err := os.CreateTemp(a.dataDir, ".recovery-*")
	if err != nil {
		return fmt.Errorf("元数据临时文件建不出来：%v", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(raw); err != nil {
		return fmt.Errorf("元数据写不进去：%v", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("元数据权限改不了：%v", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("元数据文件关不上：%v", err)
	}
	if err := os.Rename(tmpName, a.metaPath()); err != nil {
		return fmt.Errorf("元数据替换失败：%v", err)
	}
	return nil
}
