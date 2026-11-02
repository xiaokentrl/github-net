package github

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileCache 是「每个源各记一天」的落盘缓存（拍板 2026-11-02）：
// 一天只下载一次，跨到第二天才重新拉；只有「清理已选缓存」之后当天会再下一遍。
// 内存里那份 30 分钟的缓存（见 parse.go 的 CacheTTL）管的是「一轮检测别拉几十遍」，
// 这一层管的是「今天已经拉过了，重开程序也别再拉」。
//
// 每个源一个文件、各记各的天：某一份源下载失败不拖累另外两份，
// 也不会有「三份共用一个日期戳、清一份等于清三份」这种对不上的粒度。
type FileCache struct {
	dir string
	// now 是取「今天」的时钟，测试注入假时钟来验跨天必须重下。
	now func() time.Time
}

// DayLayout 是日历日的写法；换判据（比如改成按小时）只动这一处。
const DayLayout = "2006-01-02"

// cacheFile 是落盘的那份内容。判新旧只看 Day；FetchedAt 是给人看的（出问题时想知道哪天几点拉的）。
type cacheFile struct {
	Source    string             `json:"source"`
	Day       string             `json:"day"`
	FetchedAt string             `json:"fetchedAt"`
	Hostnames map[string][]Entry `json:"hostnames"`
}

func NewFileCache(dir string) *FileCache {
	return &FileCache{dir: dir, now: time.Now}
}

// useClock 把时钟换成测试给的那一份（只影响「今天」怎么算）。
func (c *FileCache) useClock(now func() time.Time) {
	c.now = now
}

func (c *FileCache) file(src Source) string {
	return filepath.Join(c.dir, src.ID+".json")
}

func (c *FileCache) today() string {
	return c.now().Format(DayLayout)
}

// Enabled 说这一层到底接没上（没给目录就是没接）。
// 清理命令靠它分清「今天还没有可清的缓存」和「这台机器上根本没这条能力」，要说的话不一样。
func (c *FileCache) Enabled() bool {
	return c != nil && c.dir != ""
}

// Load 取这个源当天的内容。文件不在、读不动、解析坏、日期不是今天、
// 或者内容其实是另一份源写的（拿错文件），一律当没有：由调用方去下载。
func (c *FileCache) Load(src Source) (map[string][]Entry, bool) {
	raw, err := os.ReadFile(c.file(src))
	if err != nil {
		return nil, false
	}
	var f cacheFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, false
	}
	if f.Source != src.ID || f.Day != c.today() || f.Hostnames == nil {
		return nil, false
	}
	return f.Hostnames, true
}

// Save 写下当天这一份。落盘失败不拦检测：这一层是省流量的加法，
// 写不进去只是回头还得再拉一遍，不该让候选拿不到。
func (c *FileCache) Save(src Source, data map[string][]Entry) error {
	if data == nil {
		return fmt.Errorf("源 %s 的内容是空的，不落盘", src.ID)
	}
	now := c.now()
	raw, err := json.Marshal(cacheFile{
		Source:    src.ID,
		Day:       now.Format(DayLayout),
		FetchedAt: now.Format(time.RFC3339),
		Hostnames: data,
	})
	if err != nil {
		return err
	}
	return writeCacheFile(c.file(src), raw)
}

// Clear 删掉这些源的落盘。返回真正删掉文件的源 ID：
// 本来就没有文件的（今天还没下载过）不混进这个数，界面要按它说话。
func (c *FileCache) Clear(srcs []Source) ([]string, error) {
	removed := make([]string, 0, len(srcs))
	var problems []string
	for _, src := range srcs {
		switch err := os.Remove(c.file(src)); {
		case err == nil:
			removed = append(removed, src.ID)
		case os.IsNotExist(err):
			// 今天还没拉过这一份，没有要删的东西——这是正常情况，不是失败。
		default:
			problems = append(problems, fmt.Sprintf("%s（%v）", src.ID, err))
		}
	}
	if len(problems) > 0 {
		return removed, fmt.Errorf("这几份源缓存没能删掉：%s", strings.Join(problems, "；"))
	}
	return removed, nil
}

// writeCacheFile 在同目录建唯一临时文件、同步落盘再改名替换；目录 0700、文件 0600。
// 和配置那一份同规矩：崩在半路也不会留下写了一半的缓存，读它的人只会当作没有。
func writeCacheFile(path string, raw []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".source-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
