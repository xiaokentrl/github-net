// Package github 是 GitHub 范围的公共 hosts 源：三个社区维护的源文件，
// 拉下来解析成「主机名 → 候选地址」。本包只管取数和整理，
// 不懂任务队列、不碰 Hosts；把它们接成加速方法、登记进注册表是 strategies 包的事。
//
// 拉源是对源站的普通 HTTPS GET（几十 KB 文本），不是 DNS 查询；这个流量开销已拍板接受。
package github

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xiaokentrl/github-net/internal/hosts"
)

// Source 是一份公共源的定义：它是谁、从哪取。ID 写进配置和注册表，定了就不许改。
type Source struct {
	ID   string
	Name string
	URL  string
}

// Entry 是源文件里拆出来的一条候选地址。IP 是规范化后的写法，可以直接进 Hosts 管理区段。
type Entry struct {
	IP   string
	IPv6 bool
}

// 拉源的三条界线：
//   - 一次连接最多 5 秒：检测一轮的期限也是 5 秒，拉得再久也会被上层掐掉，不如自己先收。
//   - 单份源最多 2 MiB：正常就是几十 KB 的文本，超限说明拿错了东西。
//   - 缓存 30 分钟：源一天更新几次；一轮检测几十个主机名，更不该拉几十遍。
const (
	FetchTimeout = 5 * time.Second
	MaxBodyBytes = 2 << 20
	CacheTTL     = 30 * time.Minute
)

// Fetcher 拉源器：同一个源在 TTL 内只拉一次，并发的调用合并成同一次请求。
type Fetcher struct {
	client *http.Client
	ttl    time.Duration

	mu      sync.Mutex
	live    map[string]*liveEntry // URL → 已缓存的映射
	loading map[string]*loadShare // URL → 正在进行的那次拉取
}

type liveEntry struct {
	at   time.Time
	data map[string][]Entry
}

type loadShare struct {
	ready chan struct{}
	data  map[string][]Entry
	err   error
}

// Default 是线上共用的拉源器。测试可以整个换掉自己那份，用完换回来。
var Default = NewFetcher(&http.Client{Timeout: FetchTimeout}, CacheTTL)

func NewFetcher(client *http.Client, ttl time.Duration) *Fetcher {
	return &Fetcher{client: client, ttl: ttl, live: map[string]*liveEntry{}, loading: map[string]*loadShare{}}
}

// Hosts 给出这个源当前的内容：缓存新鲜就直接回，过期或没有就拉一份。
// 拉失败不缓存：下一台主机名来了重试，源站抖一下不至于让整轮全空。
func (f *Fetcher) Hosts(ctx context.Context, src Source) (map[string][]Entry, error) {
	f.mu.Lock()
	if e, ok := f.live[src.URL]; ok && time.Since(e.at) < f.ttl {
		f.mu.Unlock()
		return e.data, nil
	}
	if e, ok := f.loading[src.URL]; ok {
		// 别的主机名已经在拉同一个源：等它收尾，拿到同一份结果（或同一个错误）。
		f.mu.Unlock()
		select {
		case <-e.ready:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return e.data, e.err
	}
	e := &loadShare{ready: make(chan struct{})}
	f.loading[src.URL] = e
	f.mu.Unlock()

	data, err := f.download(ctx, src.URL)
	f.mu.Lock()
	delete(f.loading, src.URL)
	if err == nil {
		f.live[src.URL] = &liveEntry{at: time.Now(), data: data}
	}
	f.mu.Unlock()
	e.data, e.err = data, err
	close(e.ready)
	return data, err
}

func (f *Fetcher) download(ctx context.Context, url string) (map[string][]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("源地址不对: %w", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("源返回了 %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("源内容读不完: %w", err)
	}
	if len(body) > MaxBodyBytes {
		return nil, fmt.Errorf("源文件超过 %d MiB 上限", MaxBodyBytes>>20)
	}
	return Parse(string(body)), nil
}

// Parse 把一份源文本拆成「主机名 → 候选地址」。只认标准 hosts 行：
// 首列是合法 IP，后面跟主机名。注释、空行、首列不是 IP 的脏行（实测源里有字面的 timeout）
// 一律跳过。同一主机名按文件里的出现顺序排，同一条地址只留最早那次。
func Parse(text string) map[string][]Entry {
	out := map[string][]Entry{}
	for _, line := range strings.Split(text, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ip := net.ParseIP(fields[0])
		if ip == nil {
			continue
		}
		var entry Entry
		if v4 := ip.To4(); v4 != nil {
			entry = Entry{IP: v4.String()}
		} else {
			entry = Entry{IP: ip.String(), IPv6: true}
		}
		for _, raw := range fields[1:] {
			h := hosts.NormalizeHostname(raw)
			if h == "" {
				continue
			}
			if !containsEntry(out[h], entry) {
				out[h] = append(out[h], entry)
			}
		}
	}
	return out
}

func containsEntry(rows []Entry, e Entry) bool {
	for _, r := range rows {
		if r.IP == e.IP {
			return true
		}
	}
	return false
}
