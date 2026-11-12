package event

import (
	"sync"
	"testing"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
)

// 收集投出去的事件；emit 在 flush 里放锁后调用，用互斥锁保护切片。
type collector struct {
	mu  sync.Mutex
	got []accelerator.Progress
}

func (c *collector) emit(name string, payload any) {
	if name != DetectProgressName {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, payload.(accelerator.Progress))
}

func (c *collector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.got)
}

func p(id, item string) accelerator.Progress {
	return accelerator.Progress{ItemID: item, CandidateID: id, Hostname: "api.github.com"}
}

// 同键后到覆盖：一拍内同一条候选换几次状态，只发最新一份；不同候选各发一份。
func TestPublishCoalescesSameCandidate(t *testing.T) {
	var c collector
	e := NewDetectEmitter(c.emit)
	e.Publish(p("c1", "api"))
	e.Publish(p("c1", "api"))
	e.Publish(p("c1", "api"))
	e.Publish(p("c2", "api"))
	waitFlushed(t, e, &c, 2)
	if c.got[0].CandidateID != "c1" || c.got[1].CandidateID != "c2" {
		t.Errorf("该按首次转入的顺序发 c1、c2，实际 %s、%s", c.got[0].CandidateID, c.got[1].CandidateID)
	}
}

// 尾部兜底：探测完最后一批后没有新事件触发下一拍，定时器也要把攒着的发出去——
// 否则最后一行的终态会卡在待发清单里，界面一直显示「检测中」。
func TestPublishFlushesTrailingEvents(t *testing.T) {
	var c collector
	e := NewDetectEmitter(c.emit)
	e.Publish(p("c9", "api"))
	if got := c.count(); got != 0 {
		t.Fatalf("一拍之内不该直发，实际发了 %d 条", got)
	}
	waitFlushed(t, e, &c, 1)
}

// 单批上限：一次攒出远超上限的量，每拍只发 progressFlushMax 条，分拍发完，总量不丢。
func TestPublishCapsBatchSize(t *testing.T) {
	var c collector
	e := NewDetectEmitter(c.emit)
	const total = progressFlushMax*3 + 7
	for i := 0; i < total; i++ {
		e.Publish(p(string(rune('a'+i%26))+string(rune('a'+i/26)), "api"))
	}
	// 等到全部发完：每拍最多 progressFlushMax 条，攒了 3 拍多，留足等待余量。
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if c.count() >= total {
			break
		}
		time.Sleep(progressFlushInterval)
	}
	if got := c.count(); got != total {
		t.Fatalf("分拍发完该一条不丢，实际 %d / %d", got, total)
	}
}

func waitFlushed(t *testing.T, e *DetectEmitter, c *collector, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		pending := len(e.pending)
		e.mu.Unlock()
		if pending == 0 && c.count() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等了 1 秒还没发完：待发 %d、已发 %d、想要 %d", len(e.pending), c.count(), want)
}
