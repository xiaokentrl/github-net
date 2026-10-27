// Command identity 把产品身份的值输出给构建配置和 shell 脚本，
// 让 internal/identity 那份 Go 常量成为唯一手写源。
//
//	# 脚本里只用这一次，之后一律读 IDENTITY_* 变量，不许再逐键调用
//	eval "$(go run ./cmd/identity --shell)"
//
//	IDENTITY_APP_ID='io.github.xiaokentrl.githubnet'
//	IDENTITY_SLUG='github-net'
//	...
//
//	go run ./cmd/identity --key HELPER_PATH     # 单独取一个值，给 Taskfile 一类只需要一项的场合
//
// 取值失败（没有 Go 工具链、模块编不过）时退出码非 0 且不输出内容，
// 调用方必须当场中断—— scripts/lib/identity-guard.sh 负责那道空值门。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/xiaokentrl/github-net/internal/identity"
)

// keyPrefix 让脚本里的变量一眼看得出来源，也避免和调用方已有变量撞名。
const keyPrefix = "IDENTITY_"

func main() {
	key := flag.String("key", "", "只输出这个键的值（不带 "+keyPrefix+" 前缀）")
	shell := flag.Bool("shell", false, "输出可 eval 的一行一个 IDENTITY_键=值")
	list := flag.Bool("list", false, "列出全部键名")
	flag.Parse()

	switch {
	case *list:
		for _, name := range identity.Keys() {
			fmt.Println(name)
		}
	case *key != "":
		value, ok := identity.Value(*key)
		if !ok {
			fmt.Fprintf(os.Stderr, "未知的键：%s（可选：%s）\n", *key, strings.Join(identity.Keys(), " "))
			os.Exit(2)
		}
		fmt.Println(value)
	case *shell:
		for _, e := range identity.Entries() {
			fmt.Printf("%s%s=%s\n", keyPrefix, e.Name, shellQuote(e.Value))
		}
	default:
		fmt.Fprintln(os.Stderr, "用法：identity --shell | --key NAME | --list")
		os.Exit(2)
	}
}

// shellQuote 用单引号包住，值里的单引号按 POSIX 办法接续，
// 保证 eval 之后拿到的就是常量本身，不会被空格、`$` 或反引号二次解释。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
