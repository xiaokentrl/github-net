package privilege

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
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
	// writeMu 串行化全部写事务（对照《原子性提交方案》并发写防护）：
	// 「还原全部」不走应用队列，没有这把锁时与在途应用并发 load→persist 会互相覆盖。
	writeMu sync.Mutex
}

func NewHostsApplier(hostsPath, dataDir string) *HostsApplier {
	return &HostsApplier{hostsPath: hostsPath, dataDir: dataDir, now: time.Now}
}

// lockFile 拿数据目录上的跨进程排它锁，返回解锁函数。
// socket 启动防抖（helper 侧）挡住绝大多数双实例，这把 flock 兜住最后一路：
// 防抖失效仍然双开时，两个进程的写事务在这里排队，不再 load→rename 互相覆盖丢更新。
func (a *HostsApplier) lockFile() (func(), error) {
	if err := os.MkdirAll(a.dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("%w：建数据目录失败：%v", ErrHostsWrite, err)
	}
	f, err := os.OpenFile(filepath.Join(a.dataDir, lockFileName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%w：开不了写互斥标记：%v", ErrHostsWrite, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%w：拿不到跨进程写锁：%v", ErrHostsWrite, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// 元数据与备份的固定文件名。备份最多一份当前有效恢复点：下一次「首次写入」会覆盖它。
const (
	metaFileName   = "recovery.json"
	backupDirName  = "backups"
	backupFileName = "hosts-original.bak"
	// lockFileName 是跨进程写互斥的标记文件：writeMu 只在单进程内有效（2026-11-13 审计中危）。
	lockFileName = "write.lock"
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
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	unlock, lerr := a.lockFile()
	if lerr != nil {
		return lerr
	}
	defer unlock()
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
	return a.persist(meta, content, newMeta, newContent)
}

// RestoreItem 把一个项目退回它应用前的映射。
func (a *HostsApplier) RestoreItem(itemID string) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	unlock, lerr := a.lockFile()
	if lerr != nil {
		return lerr
	}
	defer unlock()
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
	return a.persist(meta, content, newMeta, newContent)
}

// RestoreAll 把全部还由工具管理的项目退回应用前状态，返回实际还原的项目数。
// 全部退光后元数据和备份一起清掉——那个恢复点已经没有要还原的东西了。
func (a *HostsApplier) RestoreAll() (int, error) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	unlock, lerr := a.lockFile()
	if lerr != nil {
		return 0, lerr
	}
	defer unlock()
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
	if err := a.persist(meta, content, newMeta, newContent); err != nil {
		return 0, err
	}
	return len(restored), nil
}

// Preview 只读算还原前后的逐域名对比，不写任何文件。
// 恢复元数据在 root 数据目录里、界面进程读不到，所以这一步放到特权服务侧做。
// itemIDs 为空＝清除已写入的 Hosts（全量）；一个＝行级还原；多个＝「还原本菜单」按给定顺序一起还原。
// 没写过（账本不存在）时返回空预览，不是错误。
// 走 writeMu：写事务「先 rename Hosts、后 rename meta」分两步，不加锁的话
// 两步之间读到的 Hosts 与 meta 是两代账本，界面可能闪一次假冲突（2026-11-13 审计低危）。
func (a *HostsApplier) Preview(itemIDs []string) (hosts.RestorePreview, error) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	content, meta, err := a.load()
	if err != nil {
		return hosts.RestorePreview{}, err
	}
	switch len(itemIDs) {
	case 0:
		return hosts.PreviewRestore(meta, content)
	case 1:
		return hosts.PreviewRestoreItem(meta, content, itemIDs[0])
	default:
		return hosts.PreviewRestoreItems(meta, content, itemIDs)
	}
}

// State 只读恢复元数据，回答「最后一次成功写入是哪批映射」。
// 不改任何文件，也不读 Hosts 原文：界面拿它和现读的区段比对，判断已应用还是被改过。
// 元数据不存在就是「没写过」，返回零值而不是错误——这两件事不能混。
// 同 Preview 走 writeMu：别在写事务两步之间读走半代账本。
func (a *HostsApplier) State() (hosts.Written, error) {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	metaRaw, err := os.ReadFile(a.metaPath())
	if errors.Is(err, os.ErrNotExist) {
		return hosts.Written{}, nil
	}
	if err != nil {
		return hosts.Written{}, fmt.Errorf("%w：读恢复元数据失败：%v", ErrHostsRead, err)
	}
	meta, err := hosts.UnmarshalMetadata(metaRaw)
	if err != nil {
		return hosts.Written{}, fmt.Errorf("恢复元数据读不出来，没法判断现在生效的是哪批映射（%v）；删除 %s 可重置，但会失去还原能力", err, a.metaPath())
	}
	return meta.Written(), nil
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
// preTxContent 是事务前的 Hosts 全文：回滚写回它（而不是首次原文）——
// 非首次写入校验失败时，写回首次原文会把已应用的前几项映射一起抹掉，账实分裂、工具自锁。
func (a *HostsApplier) persist(before hosts.Recovery, preTxContent string, newMeta hosts.Recovery, newContent string) error {
	if err := a.backupOnce(before); err != nil {
		return err
	}
	if err := a.writeHosts(newContent); err != nil {
		return err
	}
	// 残余窗口（2026-11-13 审计存档）：writeHosts 成功后、saveMeta 之前进程崩溃/断电，
	// Hosts 是新区段而 meta 是旧摘要，下一笔会被 Check 判冲突自锁——saveMeta「写失败」
	// 已走下面的回滚出路，但「进程当场消失」挡不住，只能靠手工改 /etc/hosts 或删备份恢复。
	// 要根治得加「按 Hosts 实况重建账本」的白名单协议操作，成本大于收益，先存档。
	if err := a.verify(newMeta); err != nil {
		rollbackErr := a.rollback(before, preTxContent, newContent)
		if rollbackErr != nil {
			return fmt.Errorf("%w，而且回滚也失败了（%v）；系统 Hosts 可能停在中间状态，请尽快检查 %s", ErrVerifyFailed, rollbackErr, a.hostsPath)
		}
		// 回滚成功：盘上已回到事务前内容，但新内容可能已被 resolver 缓存过——也刷一次。
		flushDNSCache()
		return fmt.Errorf("%w：已回滚到写入前内容，这次修改没有生效", ErrVerifyFailed)
	}
	if !newMeta.Exists() {
		// 账本还是零值：这一笔没有记下任何东西（比如空账本上做全量还原），
		// 元数据不保存；若有陈旧文件一并清掉，别留一份对不上现状的账。
		_ = os.Remove(a.metaPath())
		return nil
	}
	if err := a.saveMeta(newMeta); err != nil {
		// 元数据写不进去（数据目录满/权限突变时真实可达）：不能停在「内容已写入、账本还是旧账」
		// ——下一笔任何操作都会因区段摘要对不上被拒，工具自锁（2026-11-13 审计中危）。
		// 与 verify 失败同一条出路：回滚到事务前内容，这笔事务整体算失败。
		rollbackErr := a.rollback(before, preTxContent, newContent)
		if rollbackErr != nil {
			return fmt.Errorf("%w：内容已写入但元数据没能保存（%v），而且回滚也失败了（%v）；系统 Hosts 可能停在中间状态，请尽快检查 %s", ErrHostsWrite, err, rollbackErr, a.hostsPath)
		}
		flushDNSCache()
		return fmt.Errorf("%w：元数据没能保存（%v），已回滚到写入前内容，这次修改没有生效", ErrHostsWrite, err)
	}
	// 全部退光就没有要还原的东西了：清掉恢复点，别留一份永远用不上的旧账本。
	if !newMeta.InUse() && newMeta.Exists() {
		_ = os.Remove(a.metaPath())
		_ = os.Remove(a.backupPath())
	}
	flushDNSCache()
	return nil
}

// flushDNSCache 尽力刷新系统级 DNS 缓存：systemd-resolved（resolvectl flush-caches）
// 和 nscd（nscd -i hosts）。两个都没装、没权限或失败都无所谓——Ubuntu 基线上的
// systemd-resolved 自己盯着 /etc/hosts，文件一变立刻生效，这一步是给装了
// nscd 的机器兜底（nscd 的 hosts 缓存默认能挂一小时）。helper 本来就是 root，
// 这两条在这里跑最合适，也不动协议。
func flushDNSCache() {
	_ = exec.Command("resolvectl", "flush-caches").Run()
	_ = exec.Command("nscd", "-i", "hosts").Run()
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
// preTxContent 为空（理论上只在首笔事务）时才写回 before.Original；
// 写回前先确认现文件仍是本次事务写入的 newContent——已被外部改动拒绝覆盖；
// 现文件读不出来也拒绝：fail-closed，宁可大声失败也不盲写盖掉未知内容（2026-11-13 审计低危）。
func (a *HostsApplier) rollback(before hosts.Recovery, preTxContent string, expectedContent string) error {
	raw, err := os.ReadFile(a.hostsPath)
	if err != nil {
		return fmt.Errorf("%w：写回前读不了系统 Hosts（%v），拒绝盲写覆盖", ErrVerifyFailed, err)
	}
	if string(raw) != expectedContent {
		return fmt.Errorf("%w：写回前发现系统 Hosts 已被外部改动（与本次事务写入的内容不一致），拒绝覆盖", ErrVerifyFailed)
	}
	target := preTxContent
	if !before.Exists() {
		target = before.Original
	}
	if err := a.writeHosts(target); err != nil {
		return err
	}
	if after, err := os.ReadFile(a.hostsPath); err != nil {
		return fmt.Errorf("回滚后重读失败：%v", err)
	} else if string(after) != target {
		return fmt.Errorf("%w：回滚后重读的内容与事务前不一致，请检查 %s", ErrVerifyFailed, a.hostsPath)
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
