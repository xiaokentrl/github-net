package hosts

import (
	"errors"
	"io/fs"
	"os"
	"time"
)

// Snapshot 是「读了一次 Hosts」的完整结果。
// 读失败不 panic、也不把 error 抛给上层，而是把原因留在 ReadErr：
// 界面上「读不到」是一种要显示出来的真实状态，不是程序出错。
type Snapshot struct {
	Path    string
	ReadAt  time.Time
	Block   Block
	ReadErr string
}

// OK 表示文件读到了；区段本身有没有毛病要看 Block。
func (s Snapshot) OK() bool { return s.ReadErr == "" }

// Read 读 path 指向的 Hosts 文件并解析管理区段，只读打开、不写一个字节。
// 路径为空、文件不存在、没有读权限都归进 ReadErr，各给一句能直接显示的话。
func Read(path string) Snapshot {
	snap := Snapshot{Path: path, ReadAt: time.Now()}
	if path == "" {
		snap.ReadErr = "拿不到系统 Hosts 的路径，无法判断状态。"
		return snap
	}
	data, err := os.ReadFile(path)
	if err != nil {
		snap.ReadErr = readFailureText(path, err)
		return snap
	}
	snap.Block = Parse(string(data))
	return snap
}

func readFailureText(path string, err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "文件不存在：" + path
	case errors.Is(err, fs.ErrPermission):
		return "没有读权限：" + path
	default:
		return "读不出来：" + err.Error()
	}
}
