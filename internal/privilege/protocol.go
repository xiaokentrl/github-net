// Package privilege 是普通权限主程序与受限特权辅助服务之间的固定 IPC 层。
//
// 权限模型沿用 github-hlep 那套（2026-10-25 用户拍板，同领域已验证）：
//
//	辅助服务以 root 常驻（写 /etc/hosts 本来就要这一档），主界面始终普通用户权限；
//	端点固定在 /run/github-net（/run 只有 root 建得出这一层目录，别人占不了名）；
//	root 态 socket 0666（普通权限的界面连得进来），本机来的连接一律放行
//	（SO_PEERCRED 仍取一次，证明确实是本机 socket 对端）；
//	服务只开放白名单操作：应用映射、单项还原、全部还原、状态询问、读写入记录。
//	不接受任意路径、任意命令或任意文件内容——新 Hosts 的内容由服务自己按映射渲染，
//	客户端只传「哪个项目、哪些主机名指到哪个 IP」这个用户决定本身。
package privilege

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/xiaokentrl/github-net/internal/hosts"
)

// RootEndpointDir 是 root 态服务的固定端点目录，socket 名固定。
// 改这两个值等于换户口：旧服务和新客户端会对不上，改动前先过 identity 那把金标准的心智。
const (
	RootEndpointDir = "/run/github-net"
	EndpointName    = "helper.sock"
)

// 操作白名单：协议里只认这五个。别的 op 一律拒绝，服务侧不解释、不透传。
// hostsState 是只读的：只回「最后一次成功写入的摘要和当前映射」，不改任何文件；
// 界面靠它判断「已应用/部分应用」，而恢复元数据在 root 目录里，界面进程自己读不到。
const (
	OpPing        = "ping"
	OpApply       = "apply"
	OpRestoreItem = "restoreItem"
	OpRestoreAll  = "restoreAll"
	OpHostsState  = "hostsState"
)

// 单帧上限：请求最大也就几十个映射，1 MiB 绰绰有余；超限当协议错误拒掉，
// 防止有人往 socket 里灌大东西把服务拖垮。
const MaxFrameSize = 1 << 20

// Request 是客户端发来的一个操作。所有字段都是服务侧要白名单校验的输入：
// op 必须在白名单里；itemID 必须非空（restoreAll 除外）；mappings 只在 apply 时有意义。
type Request struct {
	Op       string          `json:"op"`
	ItemID   string          `json:"itemId,omitempty"`
	Mappings []hosts.Mapping `json:"mappings,omitempty"`
}

// Response 是服务的一次应答。OK 为 false 时 Code/Message 一定非空；
// Restored 是 restoreAll 实际还原的项目数，其他操作不带；
// Written 只在 hostsState 时带上「最后一次成功写入」的读侧视图。
type Response struct {
	OK       bool           `json:"ok"`
	Code     string         `json:"code,omitempty"`
	Message  string         `json:"message,omitempty"`
	Restored int            `json:"restored,omitempty"`
	Written  *hosts.Written `json:"written,omitempty"`
}

// 服务侧拒绝请求时用的稳定码；客户端和日志都按码分支，不解析中文。
const (
	CodeBadFrame     = "bad_frame"
	CodeBadOp        = "bad_op"
	CodeBadItem      = "bad_item"
	CodeBadMapping   = "bad_mapping"
	CodeApplyFailed  = "apply_failed"
	CodeWriteFailed  = "write_failed"
	CodeVerifyFailed = "verify_failed"
)

// Handler 是服务对白名单操作的实现。返回 error 表示这一笔没做成（事务层保证失败时原文件不动）。
// State 是只读的，不改任何文件。
type Handler interface {
	Apply(itemID string, mappings []hosts.Mapping) error
	RestoreItem(itemID string) error
	RestoreAll() (restored int, err error)
	State() (hosts.Written, error)
}

// ValidateRequest 是服务侧的入口校验：op 在白名单、字段与操作匹配。
// 放在包里而不是 cmd 里，是为了让契约测试不依赖那个二进制就能钉死协议边界。
func ValidateRequest(req Request) *Response {
	switch req.Op {
	case OpPing:
		return nil
	case OpApply:
		if req.ItemID == "" {
			return &Response{Code: CodeBadItem, Message: "apply 请求缺项目 ID"}
		}
		if len(req.Mappings) == 0 {
			return &Response{Code: CodeBadMapping, Message: "apply 请求没有携带任何映射"}
		}
		for _, m := range req.Mappings {
			if m.IP == "" || len(m.Hostnames) == 0 {
				return &Response{Code: CodeBadMapping, Message: "映射里 IP 或主机名是空的"}
			}
		}
		return nil
	case OpRestoreItem:
		if req.ItemID == "" {
			return &Response{Code: CodeBadItem, Message: "restoreItem 请求缺项目 ID"}
		}
		return nil
	case OpRestoreAll:
		return nil
	case OpHostsState:
		return nil
	default:
		return &Response{Code: CodeBadOp, Message: fmt.Sprintf("不认识的操作 %q，服务只开白名单那五个", req.Op)}
	}
}

// 写一帧：4 字节大端长度 + JSON。
func writeFrame(send func([]byte) (int, error), v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("编码请求: %w", err)
	}
	if len(raw) > MaxFrameSize {
		return fmt.Errorf("帧大小 %d 超上限 %d", len(raw), MaxFrameSize)
	}
	head := []byte{byte(len(raw) >> 24), byte(len(raw) >> 16), byte(len(raw) >> 8), byte(len(raw))}
	if _, err := send(head); err != nil {
		return fmt.Errorf("写帧头: %w", err)
	}
	if _, err := send(raw); err != nil {
		return fmt.Errorf("写帧体: %w", err)
	}
	return nil
}

// readFrame 读一帧，长度超限即协议错误。
func readFrame(receive func([]byte) (int, error)) ([]byte, error) {
	head := make([]byte, 4)
	if _, err := ioReadFull(receive, head); err != nil {
		return nil, err
	}
	size := int(head[0])<<24 | int(head[1])<<16 | int(head[2])<<8 | int(head[3])
	if size <= 0 || size > MaxFrameSize {
		return nil, fmt.Errorf("%w: 帧长度 %d 不在 (0, %d] 内", ErrProtocol, size, MaxFrameSize)
	}
	body := make([]byte, size)
	if _, err := ioReadFull(receive, body); err != nil {
		return nil, err
	}
	return body, nil
}

// ErrProtocol 表示帧边界坏了：重试也没用，客户端把它归成「服务不可信」直接报错。
var ErrProtocol = errors.New("IPC 协议错误")

// ioReadFull 把 read 全填满；EOF 在读到 0 字节时按「连接被关」报，读一半按协议错误报。
func ioReadFull(receive func([]byte) (int, error), buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := receive(buf[total:])
		if n > 0 {
			total += n
		}
		if err != nil {
			if total == 0 && n == 0 {
				return total, fmt.Errorf("%w: 对端关了连接: %v", ErrProtocol, err)
			}
			return total, fmt.Errorf("%w: 读了一半连接断了: %v", ErrProtocol, err)
		}
		if n == 0 {
			return total, fmt.Errorf("%w: 对端不再发送", ErrProtocol)
		}
	}
	return total, nil
}
