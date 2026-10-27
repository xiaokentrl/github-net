package app

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
)

// HostsAccess 是「当前进程对系统 Hosts 的写权限」探测结果。
// 这里只探测、不写入：Hosts 写入仍属后面的特权服务阶段。
type HostsAccess string

const (
	// HostsAccessWritable 表示能以当前身份打开 Hosts 文件写（不截断、不写一个字节）。
	HostsAccessWritable HostsAccess = "writable"
	// HostsAccessReadOnly 表示因权限不足打不开，需要提权或辅助服务。
	HostsAccessReadOnly HostsAccess = "readOnly"
	// HostsAccessUnknown 表示探测本身失败（文件不存在、平台不支持等），不能当成任何一种结论。
	HostsAccessUnknown HostsAccess = "unknown"
)

// HostsPath 返回当前平台的系统 Hosts 路径；拿不到就是空串，由探测方报 unknown。
func HostsPath() string {
	if runtime.GOOS != "windows" {
		return "/etc/hosts"
	}
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = os.Getenv("windir")
	}
	if root == "" {
		return ""
	}
	return root + `\System32\drivers\etc\hosts`
}

// ProbeHostsAccess 用只打开不截断的方式试探写权限，成功后立即关闭。
// 权限不足和「其它错误」分开报，因为界面上「需要提权」和「读不到」是两回事。
func ProbeHostsAccess(path string) HostsAccess {
	if path == "" {
		return HostsAccessUnknown
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		_ = f.Close()
		return HostsAccessWritable
	}
	if errors.Is(err, fs.ErrPermission) {
		return HostsAccessReadOnly
	}
	return HostsAccessUnknown
}
