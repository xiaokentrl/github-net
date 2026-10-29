//go:build unix

package privilege

import (
	"net"

	"golang.org/x/sys/unix"
)

// checkPeer 取一次对端凭据，证明确实是本机 socket 对端。
// 权限模型〔沿用 github-hlep，2026-10-25 用户拍板〕：本机来的连接一律放行——
// 白名单操作本身不泄密、不破坏（应用/还原都只动本工具标记的区段），
// 真正的边界是「只开这几个操作」而不是「哪个用户可以连」。
// 取失败不拒绝：拿不到凭据通常是非 Linux 的 unix socket 或实现差异，协议白名单还在兜底。
func checkPeer(conn net.Conn) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return
	}
	_ = raw.Control(func(fd uintptr) {
		_, _ = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	})
}
