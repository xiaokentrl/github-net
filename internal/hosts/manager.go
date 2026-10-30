package hosts

import (
	"errors"
	"io/fs"
	"os"
	"strings"
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
	// Shadowed 是「区段之前的手写行里出现过的主机名」集合（已规范化）。
	// Hosts 按行序首条生效：这些行会挡住管理区段里同名的映射，界面据此提示。
	// 只在区段可用且有重名时非空；读失败或没区段时为空。
	Shadowed map[string]bool
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
	snap.Shadowed = shadowedOutside(string(data), snap.Block)
	return snap
}

// shadowedOutside 找出「区段之前的手写行」里出现过的主机名（与区段内记录同名的才算）。
// Hosts 按行序首条生效：区段之前的手写行会挡住区段里同名的映射；区段之后的不挡。
func shadowedOutside(content string, block Block) map[string]bool {
	out := make(map[string]bool)
	if !block.Present || !block.Usable() {
		return out
	}
	lines := splitLines(content)
	for i := 0; i < block.beginAt && i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, h := range fields[1:] {
			out[NormalizeHostname(h)] = true
		}
	}
	// 只留和区段内记录同名的：集合本身就是给「挡住区段映射」这件事用的。
	blockNames := make(map[string]bool, len(block.Records))
	for _, rec := range block.Records {
		for _, h := range rec.Hostnames {
			blockNames[h] = true
		}
	}
	result := make(map[string]bool)
	for name := range out {
		if blockNames[name] {
			result[name] = true
		}
	}
	return result
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
