package app

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaokentrl/github-net/internal/identity"
	"github.com/xiaokentrl/github-net/internal/retention"
)

// PruneCaches 是启动时那一次本机落盘缓存清理（拍板 2026-11-05：任何缓存最多只保存 3 天，超过就自动清理）。
// 它是包级函数而不是 Service 的方法，理由和 ShutdownTaskQueue 一样：Service 的每个导出方法都会被 Wails
// 绑成前端能调的命令，而「把本机缓存删一删」只该是进程起来那一刻自己做的事，不该成为界面上一个能点的按钮
// （命令清单也因此还是 28 条，没多出这一条）。
//
// 管三样：候选底账（SQLite，轮次 + 每轮候选 + 按 IP 去重的地址池）、公共源的落盘缓存文件、
// 下载下来的升级包与配套签名。三样都能由下一次检测或下一次下载重新生成，删了不丢任何结论。
//
// 不算缓存、不碰的：用户配置 config.json（含每个范围选的加速方法）、系统 Hosts 里已生效的区段、
// 恢复依据 recovery.json 与 hosts-original.bak、自启动条目——删掉那些是改功能，不是清缓存。
//
// 只在启动时跑这一次（拍板 4A）；清失败不吭声也不拦启动（拍板 5A）：少删几条不影响任何功能，
// 为「清理」这种锦上添花的事弹一条提示反而是打扰。库没开成、落盘缓存没接上、目录不存在，都当没东西可清。
func PruneCaches(s *Service) {
	if s.candidateStore != nil {
		_ = s.candidateStore.Prune()
	}
	_ = s.sources().PruneCache()
	if dir, err := s.updatesDir(); err == nil {
		_, _ = pruneExpiredFiles(dir)
	}
}

// pruneExpiredFiles 删掉 dir 这一层里超过保留期限的普通文件，两道门缺一不可：
//  1. 名字得是本应用写下的（见 isOurUpdateFile）——这个落点（配置目录的上一级里的 updates/）不是本应用独占的，
//     别人家的文件年龄再老也不归我们删；
//  2. 年龄超过保留期限。
//
// 目录一律不碰，也不往子目录里递归，理由同上。年龄看文件的修改时间：升级包和配套签名都是本应用下载时写下的，
// 写下那一刻就是它最后一次被保存的时刻。删掉的个数只给用例核对，界面不显示这个数。
func pruneExpiredFiles(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	cutoff := time.Now().Add(-retention.MaxAge)
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !isOurUpdateFile(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

// isOurUpdateFile 是删除那一刻的归属门：只认下载器会写下的那两类名字。
// 安装包与配套签名都以包名（identity.PackageName，就是发布资产名的开头）打头，
// 下载中途崩了留下的临时文件是 `.<包名>...` 那种点开头名字（见 internal/updater/download.go 的 CreateTemp 模板）。
// 校验清单不算：FetchChecksums 读完就当场删掉，不会在这里躺着。
// 名字对不上的一律当别人的东西——年龄再老也不动。
func isOurUpdateFile(name string) bool {
	return strings.HasPrefix(name, identity.PackageName) || strings.HasPrefix(name, "."+identity.PackageName)
}
