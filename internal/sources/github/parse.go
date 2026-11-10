// Package github 是 GitHub 范围的公共 hosts 源：三个社区维护的源文件，
// 拉下来解析成「主机名 → 候选地址」。本包只管取数和整理，
// 不懂任务队列、不碰 Hosts；把它们接成加速方法、登记进注册表是 strategies 包的事。
//
// 拉源是对源站的普通 HTTPS GET（几十 KB 文本），不是 DNS 查询；这个流量开销已拍板接受。
package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
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
// 带 JSON 标签是因为落盘缓存（cache.go）把整份映射写成文件，读写两头共用这一个形状。
type Entry struct {
	IP   string `json:"ip"`
	IPv6 bool   `json:"ipv6,omitempty"`
}

// 拉源的三条界线：
//   - 一次连接最多 5 秒：检测一轮的期限也是 5 秒，拉得再久也会被上层掐掉，不如自己先收。
//   - 单份源最多 2 MiB：正常就是几十 KB 的文本，超限说明拿错了东西。
//   - 内存里 30 分钟：源一天更新几次；一轮检测几十个主机名，更不该拉几十遍。
//     再往外还有一层「每个源各记一天」的落盘缓存（见 cache.go），接上之后一天只下载一次。
const (
	FetchTimeout = 5 * time.Second
	MaxBodyBytes = 2 << 20
	CacheTTL     = 30 * time.Minute
)

// Fetcher 拉源器：同一个源在 TTL 内只拉一次，并发的调用合并成同一次请求。
// 接上 cache 之后，TTL 过了也先看当天那份落盘，只有跨天或被人清掉才真的下载。
type Fetcher struct {
	client *http.Client
	ttl    time.Duration
	cache  *FileCache // nil＝不落盘，只有内存这一层（测试和没接缓存的阶段）

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
//
// 代理策略（网络与离线弹性方案纪律，显式声明）：遵循环境变量代理（HTTP_PROXY/
// HTTPS_PROXY，Go 默认 ProxyFromEnvironment），不读桌面 GUI 代理设置；
// 证书校验不降级。源拉取是幂等 GET：传输层错误与 5xx 带退避抖动自动重试。
var Default = NewFetcher(&http.Client{Timeout: FetchTimeout}, CacheTTL)

func NewFetcher(client *http.Client, ttl time.Duration) *Fetcher {
	return &Fetcher{client: client, ttl: ttl, live: map[string]*liveEntry{}, loading: map[string]*loadShare{}}
}

// UseCache 给共用那份拉源器接上落盘缓存，由装配层在启动时调一次。
// 传 nil 就是关掉这一层（拿不到配置目录时的现状：只在内存里记 30 分钟）。
func (f *Fetcher) UseCache(cache *FileCache) {
	f.mu.Lock()
	f.cache = cache
	f.mu.Unlock()
}

// CacheEnabled 说这一层接上没接上。「清理已选缓存」用它分清两种情况：
// 没接上是这台机器上没有这条能力，接上了但一份都没删是今天还没下载过——两句话不一样。
func (f *Fetcher) CacheEnabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cache.Enabled()
}

// Hosts 给出这个源当前的内容：内存新鲜就直接回，过期了先看当天那份落盘，
// 都没有才下载，下载成功顺手写盘。
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
	cache := f.cache
	f.mu.Unlock()

	data, err := f.load(ctx, src, cache)
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

// load 是「当天落盘 → 下载」这一步，走在刚才那个合并窗口里：
// 同一份源被几十个主机名同时要，也只读一次文件、最多下载一次。
// 落盘读到了同样会被上面写回内存，所以一轮里后面那些主机名连文件都不用碰。
func (f *Fetcher) load(ctx context.Context, src Source, cache *FileCache) (map[string][]Entry, error) {
	if cache.Enabled() {
		if data, ok := cache.Load(src); ok {
			return data, nil
		}
	}
	data, err := f.download(ctx, src.URL)
	if err != nil {
		return nil, err
	}
	if cache.Enabled() {
		// 写盘失败不当错误抛：候选已经拿到了，顶多下一轮再拉一遍。
		_ = cache.Save(src, data)
	}
	return data, nil
}

// CachedToday 只读报出这些源里今天已经下过、落盘还在的源 ID。
// 预览用它标「点了立即清理会不会真删」：今天没下过的源本来就没文件，不在返回里。
// 判据复用 FileCache.Load（同样认当天日期和源 ID），不另写一套。
func (f *Fetcher) CachedToday(srcs []Source) []string {
	f.mu.Lock()
	cache := f.cache
	f.mu.Unlock()
	out := make([]string, 0, len(srcs))
	if !cache.Enabled() {
		return out
	}
	for _, src := range srcs {
		if _, ok := cache.Load(src); ok {
			out = append(out, src.ID)
		}
	}
	return out
}

// ClearCache 丢掉这些源的内存和落盘两份，下一轮检测就会重新下载（拍板：清理后才重新下载）。
// 返回真删掉落盘文件的源 ID——今天还没下载过的不在这个数里。
func (f *Fetcher) ClearCache(srcs []Source) ([]string, error) {
	f.mu.Lock()
	for _, src := range srcs {
		delete(f.live, src.URL)
	}
	cache := f.cache
	f.mu.Unlock()
	if !cache.Enabled() {
		return nil, nil
	}
	return cache.Clear(srcs)
}

// PruneCache 启动时清一次落盘缓存里超过保留期限的文件（拍板 2026-11-05：任何缓存最多存 3 天）。
// 内存那一份 TTL 只有 30 分钟，不存在超期的东西，所以只动落盘这一层；没接落盘缓存就什么都不做。
func (f *Fetcher) PruneCache() error {
	f.mu.Lock()
	cache := f.cache
	f.mu.Unlock()
	if !cache.Enabled() {
		return nil
	}
	return cache.PruneExpired()
}

// fetchAttempts 是一次源拉取的总尝试次数（原始 + 2 次重试）：源拉取是幂等 GET，
// 传输层错误与 5xx 自动重试；4xx（源挪走/不存在）重试无意义，直接返回。
// 退避 400ms→800ms 带抖动；每次尝试都在同一个 ctx 里，上层掐 deadline 时立即停。
// fetchBackoffBase 是包级变量：测试置 0 让退避瞬时走完，不拖慢用例。
const fetchAttempts = 3

var fetchBackoffBase = 400 * time.Millisecond

// retryableError 标记「值得重试」的失败（5xx）：重试分类用 errors.As 判定，
// 不靠错误文案的字符串匹配——改一句文案就静默改变重试语义的坑就是这么来的。
type retryableError struct{ err error }

func (e retryableError) Error() string { return e.err.Error() }
func (e retryableError) Unwrap() error { return e.err }

func (f *Fetcher) download(ctx context.Context, url string) (map[string][]Entry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("源地址不对: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt < fetchAttempts; attempt++ {
		if attempt > 0 {
			// 退避翻倍（400ms→800ms）+ 抖动 ±90ms；ctx 取消立即停，不硬睡。
			backoff := fetchBackoffBase << (attempt - 1)
			// 抖动 ±90ms（毫秒级；time.Duration 整数是纳秒，之前单位错成了纳秒）
			jitter := time.Duration(rand.Intn(181)-90) * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff + jitter):
			}
		}
		resp, err := f.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("拉取失败: %w", err)
			continue // 传输层错误：退避后重试
		}
		body, readErr := func() (map[string][]Entry, error) {
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				if resp.StatusCode >= 500 {
					// 5xx：服务端抖动，值得重试——包上哨兵错误供外层 errors.As 识别
					return nil, retryableError{fmt.Errorf("源返回了 %s", resp.Status)}
				}
				return nil, fmt.Errorf("源返回了 %s", resp.Status) // 4xx：重试无意义
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBodyBytes+1))
			if err != nil {
				return nil, fmt.Errorf("源内容读不完: %w", err)
			}
			if len(body) > MaxBodyBytes {
				return nil, fmt.Errorf("源文件超过 %d MiB 上限", MaxBodyBytes>>20)
			}
			return Parse(string(body)), nil
		}()
		if readErr == nil {
			return body, nil
		}
		// 非 5xx（4xx、内容读不完）重试无意义：内容问题重试也是一样的结果
		var re retryableError
		if !errors.As(readErr, &re) {
			return nil, readErr
		}
		lastErr = readErr
	}
	return nil, lastErr
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
