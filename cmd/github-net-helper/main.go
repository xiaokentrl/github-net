// github-net-helper 是受限特权辅助服务：唯一被允许写系统 Hosts 的那一个进程。
//
// 权限模型见 internal/privilege 的包注释：root 常驻、固定端点 /run/github-net/helper.sock、
// socket 0666、白名单操作。它不读用户配置、不知道前端存在、不执行命令——
// 只做「把客户端给的映射安全地写进 Hosts，或把它们还原」这一件事。
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"github.com/xiaokentrl/github-net/internal/privilege"
)

func main() {
	// 默认值是生产形状；单元测试用 -hosts 和 -data 指到临时文件，不走真实系统。
	hostsPath := flag.String("hosts", "/etc/hosts", "系统 Hosts 路径（仅测试覆盖）")
	dataDir := flag.String("data", "/var/lib/github-net", "恢复元数据与备份目录（仅测试覆盖）")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.LUTC)

	if err := run(*hostsPath, *dataDir); err != nil {
		log.Fatalf("辅助服务退出：%v", err)
	}
}

func run(hostsPath, dataDir string) error {
	if err := os.MkdirAll(privilege.RootEndpointDir, 0o755); err != nil {
		return fmt.Errorf("建端点目录 %s: %w", privilege.RootEndpointDir, err)
	}
	socket := privilege.RootEndpointDir + "/" + privilege.EndpointName
	// 删旧 socket 前先试连一下：连得上＝已有活服务在跑（手动/脚本误拉起第二个实例），
	// 退出而不是抢走它的 socket——盲删会让两个 root 进程并发写 /etc/hosts 丢更新
	// （进程内的 writeMu 拦不住跨进程，2026-11-13 审计中危；数据目录 flock 兜最后一路）。
	if c, dialErr := net.DialTimeout("unix", socket, time.Second); dialErr == nil {
		_ = c.Close()
		return fmt.Errorf("已有辅助服务在监听 %s，本实例退出", socket)
	}
	// 旧 socket 是上一次异常退出留下的：删掉重建，bind 才不会撞「地址已在使用」。
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", socket, err)
	}
	// 普通权限的主界面要连得进来：0666 是权限模型拍板的那一档（见 internal/privilege 包注释）。
	if err := os.Chmod(socket, 0o666); err != nil {
		return fmt.Errorf("设置 socket 权限: %w", err)
	}

	applier := privilege.NewHostsApplier(hostsPath, dataDir)
	log.Printf("辅助服务就绪：hosts=%s data=%s endpoint=%s", hostsPath, dataDir, socket)

	// Serve 只在 listener 出错时返回；systemd 的 Restart=on-failure 负责把它拉起来。
	serveErr := privilege.NewServer(applier).Serve(ln)
	// 正常/异常退出都把 socket 痕迹扫掉；systemd 的 ExecStopPost 再兜一层运行时目录。
	_ = os.Remove(socket)
	return serveErr
}
