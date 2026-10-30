package privilege

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Server 是特权服务侧的连接循环：收一帧请求、白名单校验、交给 Handler、回一帧应答。
// 它只管协议和对端校验，hosts 怎么写归 Handler（cmd 里那层）。
type Server struct {
	handler Handler
}

func NewServer(handler Handler) *Server {
	return &Server{handler: handler}
}

// Serve 逐个处理连接，每连接一个现场；listener 关闭或出错即返回。
// 返回的 error 只用于服务退出日志，单个连接的失败不中断服务。
func (s *Server) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return fmt.Errorf("接受连接: %w", err)
		}
		go s.handleConn(conn)
	}
}

// handleConn 处理一条连接：目前协议是一问一答，答完即关。
// 以后要复用连接（一次连上发多笔）再改成循环，现在的操作频率用不上。
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	checkPeer(conn)

	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		// 设不了期限的连接不值得信任，直接关。
		return
	}
	var req Request
	body, err := readFrame(conn.Read)
	if err == nil {
		err = unmarshalInto(body, &req)
	}
	if err == nil {
		err = s.dispatch(req, conn)
	}
	if err != nil {
		// 协议层失败（帧坏了）没法回一帧像样的应答，直接断；
		// 业务失败在 dispatch 里已经回过应答，到这里说明回应答也失败了，同样只能断。
		_ = err
	}
}

// dispatch 校验白名单并调用 Handler，把结果（成功或业务失败）作为一帧应答回去。
func (s *Server) dispatch(req Request, conn net.Conn) error {
	resp := func() *Response {
		if bad := ValidateRequest(req); bad != nil {
			return bad
		}
		return s.callHandler(req)
	}()
	if resp == nil {
		resp = &Response{OK: true}
	}
	return writeFrame(conn.Write, resp)
}

func (s *Server) callHandler(req Request) (resp *Response) {
	// Handler 的实现里有文件操作；任何 panic 都不能带崩 root 服务，兜成业务失败应答。
	defer func() {
		if r := recover(); r != nil {
			resp = &Response{Code: CodeApplyFailed, Message: "服务内部错误，操作没有完成"}
		}
	}()
	switch req.Op {
	case OpPing:
		return nil
	case OpApply:
		if err := s.handler.Apply(req.ItemID, req.Mappings); err != nil {
			return &Response{Code: CodeApplyFailed, Message: err.Error()}
		}
		return nil
	case OpRestoreItem:
		if err := s.handler.RestoreItem(req.ItemID); err != nil {
			return &Response{Code: CodeApplyFailed, Message: err.Error()}
		}
		return nil
	case OpRestoreAll:
		n, err := s.handler.RestoreAll()
		if err != nil {
			return &Response{Code: CodeApplyFailed, Message: err.Error()}
		}
		return &Response{OK: true, Restored: n}
	case OpHostsState:
		written, err := s.handler.State()
		if err != nil {
			return &Response{Code: CodeApplyFailed, Message: err.Error()}
		}
		return &Response{OK: true, Written: &written}
	}
	return &Response{Code: CodeBadOp, Message: "不认识的操作"}
}

// unmarshalInto 单独包一层，让 JSON 错误也带上协议语义。
func unmarshalInto(body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%w: 请求不是合法 JSON: %v", ErrProtocol, err)
	}
	return nil
}
