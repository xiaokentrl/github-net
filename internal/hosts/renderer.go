package hosts

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// 两个哨兵错误：上层按错误码回给界面，靠 errors.Is 判，不靠句子匹配。
var (
	// ErrConflict 是现有区段不能判读（标记残缺、重复、嵌套、顺序错、版本未知、区段里有外部改动）。
	// 这时候绝不猜着修，也不覆盖未知内容。
	ErrConflict = errors.New("现有区段与预期不一致")
	// ErrBadRecord 是准备写进去的映射本身不合法（IP 解析不出来、主机名为空或带端口、同一主机名给了两个 IP）。
	ErrBadRecord = errors.New("映射记录不合法")
)

// Render 生成「把这些映射写进管理区段之后」的 Hosts 全文。它只算字符串，一个字节都不写盘，
// 更不碰系统文件；落盘、备份和验证属后面的特权服务阶段。
//
// 三条固定行为（口径来自技术规划第 6 节与结构规划的 Hosts 写入契约）：
//  1. 区段外的每一行原样保留：顺序、大小写、注释、行尾风格都不动。
//  2. 原文件没有标记时在末尾追加区段，先空一行与原有内容分隔（空行/文件为空时不重复加）。
//  3. 现有区段判读不了就整体拒绝（ErrConflict），不做「先清掉再写」这种猜测式修复。
func Render(content string, desired []Record) (string, error) {
	block := Parse(content)
	if block.Present && !block.Usable() {
		return "", fmt.Errorf("%w：%s", ErrConflict, block.Reason)
	}
	lines, err := managedLines(desired)
	if err != nil {
		return "", err
	}
	if !block.Present && len(lines) == 0 {
		// 没有区段、又不要任何映射：原文一个字节都不动，连行尾风格都不改。
		return content, nil
	}

	eol := "\n"
	if strings.Contains(content, "\r\n") {
		eol = "\r\n"
	}
	src := splitLines(content)
	// 先空一行再接区段（拍板 2026-10-31）：区段和用户自己写的行之间留一道空白，
	// 一眼能看出哪里是工具的地盘。只在「新追加区段」时加：已有区段的原位更新不动周围空行。
	separate := !block.Present && len(src) > 0 && strings.TrimSpace(src[len(src)-1]) != ""
	out := make([]string, 0, len(src)+len(lines)+1)
	if block.Present {
		end := block.beginAt
		// 整段撤掉时，把当初插入的那道空行一并收走：我们只在「非空行」后面加过空行，
		// 所以「空行紧贴区段、它上面还是非空行」的那一道就是工具加的；用户自己的连续空行不动。
		if len(lines) == 0 && end > 1 && strings.TrimSpace(src[end-1]) == "" && strings.TrimSpace(src[end-2]) != "" {
			end--
		}
		out = append(out, src[:end]...)
		out = append(out, lines...)
		out = append(out, src[block.endAt+1:]...)
	} else {
		out = append(out, src...)
		if separate {
			out = append(out, "")
		}
		out = append(out, lines...)
	}
	if len(out) == 0 {
		return "", nil
	}
	// 结尾换行跟随原文；唯一例外是往末尾追加区段时，必须先把原来那条没写完的最后一行补成整行。
	endsWithNewline := content == "" || strings.HasSuffix(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if endsWithNewline || !block.Present {
		return strings.Join(out, eol) + eol, nil
	}
	return strings.Join(out, eol), nil
}

// managedLines 把映射收成区段里的行：规范化、去重、稳定排序，非法记录当场拒。
// 顺序规则是「按每行最小的主机名排」，行内主机名也按字典序——同一批映射在任何机器、
// 任何顺序进来都出同一个字节串，后面算区段摘要才可比。
func managedLines(desired []Record) ([]string, error) {
	ipByHost := make(map[string]string, len(desired))
	hostsByIP := make(map[string][]string)
	for _, rec := range desired {
		ip := net.ParseIP(strings.TrimSpace(rec.IP))
		if ip == nil {
			return nil, fmt.Errorf("%w：IP %q 不是合法地址", ErrBadRecord, rec.IP)
		}
		canonicalIP := ip.String()
		if len(rec.Hostnames) == 0 {
			return nil, fmt.Errorf("%w：%s 后面没有主机名", ErrBadRecord, canonicalIP)
		}
		for _, raw := range rec.Hostnames {
			host := NormalizeHostname(raw)
			if !validHostname(host) {
				return nil, fmt.Errorf("%w：主机名 %q 不能写进区段", ErrBadRecord, raw)
			}
			// 同一个 IP 的两种写法（::1 与 0:0:0:0:0:0:0:1）算同一条，不算冲突。
			if known, seen := ipByHost[host]; seen {
				if known != canonicalIP {
					return nil, fmt.Errorf("%w：主机名 %s 同时被指到 %s 和 %s", ErrBadRecord, host, known, canonicalIP)
				}
				continue
			}
			ipByHost[host] = canonicalIP
			hostsByIP[canonicalIP] = append(hostsByIP[canonicalIP], host)
		}
	}

	type row struct {
		ip        string
		hostnames []string
	}
	// 每个主机名只会归到一个 IP 上（上面那道门保证），所以 hostsByIP 里天然没有重复，
	// 「行内最小主机名」在行与行之间也不会并列——用它当排序键就是全序，
	// 输出与 map 遍历顺序无关。
	rows := make([]row, 0, len(hostsByIP))
	for ip, hosts := range hostsByIP {
		sort.Strings(hosts)
		rows = append(rows, row{ip: ip, hostnames: hosts})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].hostnames[0] < rows[j].hostnames[0] })
	if len(rows) == 0 {
		// 一条映射都不剩：整段撤掉，连标记一起走。留着空标记对会让下一次判读
		// 把「空区段」当成已应用状态，而且文件里永久挂着两行没用的工具痕迹。
		return nil, nil
	}

	lines := make([]string, 0, len(rows)+2)
	lines = append(lines, MarkerBegin())
	for _, r := range rows {
		lines = append(lines, r.ip+" "+strings.Join(r.hostnames, " "))
	}
	lines = append(lines, MarkerEnd())
	return lines, nil
}
