package privilege

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/xiaokentrl/github-net/internal/hosts"
)

// 客户端三类失败，前端按码分支：
//
//	ErrHelperUnavailable —— 端点连不上：服务没装、没起来或已退出。修复方式是装/起服务，重试没用。
//	ErrHelperRefused     —— 服务明确拒绝了这笔操作（白名单校验或业务失败），Message 可展示。
//	ErrProtocol          —— 帧坏了：两边的协议版本对不上，重装其中一个才能修。
var (
	ErrHelperUnavailable = errors.New("特权辅助服务不可用")
	ErrHelperRefused     = errors.New("特权辅助服务拒绝了这次操作")
)

// Caller 是主程序侧对辅助服务的窄接口；app 层只认这个，测试换成假实现。
type Caller interface {
	Ping() error
	Apply(itemID string, mappings []hosts.Mapping) error
	RestoreItem(itemID string) error
	RestoreAll() (restored int, err error)
}

// Client 是 Caller 的真实现：连固定端点、一问一答、答完即关。
type Client struct {
	// dialTimeout 覆盖连接建立；callTimeout 覆盖整笔操作（含服务侧读写 Hosts）。
	dialTimeout time.Duration
	callTimeout time.Duration
	// endpoint 空即生产端点；测试用 NewClientAt 指到临时 socket。
	endpoint string
}

func NewClient() *Client {
	return &Client{dialTimeout: 2 * time.Second, callTimeout: 20 * time.Second}
}

// NewClientAt 建一个连指定端点的客户端，只有测试用：生产代码一律 NewClient。
func NewClientAt(endpoint string) *Client {
	c := NewClient()
	c.endpoint = endpoint
	return c
}

// Endpoint 返回要连的端点。root 态进程（比如测试里直接起服务）连同一条，
// 这样「服务在不在」只有一个真相。
func (c *Client) Endpoint() string {
	if c.endpoint != "" {
		return c.endpoint
	}
	return RootEndpointDir + "/" + EndpointName
}

func (c *Client) Ping() error {
	_, err := c.call(Request{Op: OpPing})
	return err
}

func (c *Client) Apply(itemID string, mappings []hosts.Mapping) error {
	_, err := c.call(Request{Op: OpApply, ItemID: itemID, Mappings: mappings})
	return err
}

func (c *Client) RestoreItem(itemID string) error {
	_, err := c.call(Request{Op: OpRestoreItem, ItemID: itemID})
	return err
}

func (c *Client) RestoreAll() (int, error) {
	resp, err := c.call(Request{Op: OpRestoreAll})
	if err != nil {
		return 0, err
	}
	return resp.Restored, nil
}

// call 发一帧、收一帧；把网络层错误归类成上面三类失败，业务拒绝带服务侧的可展示消息。
func (c *Client) call(req Request) (*Response, error) {
	conn, err := net.DialTimeout("unix", c.Endpoint(), c.dialTimeout)
	if err != nil {
		return nil, fmt.Errorf("%w：%v（服务没装或没在跑）", ErrHelperUnavailable, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.callTimeout))

	if err := writeFrame(conn.Write, req); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	body, err := readFrame(conn.Read)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	var resp Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("%w: 应答不是合法 JSON: %v", ErrProtocol, err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("%w: %s", ErrHelperRefused, resp.Message)
	}
	return &resp, nil
}
