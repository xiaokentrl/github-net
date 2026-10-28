// Package hosts 判读并渲染系统 Hosts 文件里属于本工具的管理区段。
// 本包一个字节都不写盘：写盘、备份、事务和还原属后面的特权服务阶段。
package hosts

import (
	"net"
	"regexp"
	"strconv"
	"strings"
)

// ManagedVersion 是区段标记里的版本号。标记行必须整行等于下面两行之一。
const ManagedVersion = 1

var (
	beginMarker = markerFor("BEGIN", ManagedVersion)
	endMarker   = markerFor("END", ManagedVersion)

	beginLine = regexp.MustCompile(`^# BEGIN github-net managed hosts v([0-9]+)$`)
	endLine   = regexp.MustCompile(`^# END github-net managed hosts v([0-9]+)$`)
)

// MarkerBegin / MarkerEnd 给出标记原文，写入阶段和测试都用它们，不留第二份字符串。
func MarkerBegin() string { return beginMarker }
func MarkerEnd() string   { return endMarker }

func markerFor(kind string, version int) string {
	return "# " + kind + " github-net managed hosts v" + strconv.Itoa(version)
}

// Anomaly 是区段结构上的毛病。空值表示区段能判读（或者根本没有区段）。
type Anomaly string

const (
	AnomalyNone           Anomaly = ""
	AnomalyDuplicated     Anomaly = "duplicated"     // 标记重复、套娃或顺序颠倒
	AnomalyMissingEnd     Anomaly = "missingEnd"     // 有头没尾
	AnomalyMissingBegin   Anomaly = "missingBegin"   // 有尾没头
	AnomalyUnknownVersion Anomaly = "unknownVersion" // 版本号不是本程序认识的
	AnomalyBadRecord      Anomaly = "badRecord"      // 区段里出现了不是「IP + 主机名」的行
)

// Record 是区段内的一条映射。Line 是 1 起的行号，只在说明问题时用。
type Record struct {
	IP        string
	Hostnames []string
	Line      int
}

// Block 是管理区段的读取结果。Present 说有没有区段，Anomaly 说区段能不能判读，
// Reason 是能直接显示给用户的一句话；Records 只在结构可用时填写。
type Block struct {
	Present bool
	Anomaly Anomaly
	Reason  string
	Version int
	Records []Record

	// beginAt / endAt 是两枚标记行在 splitLines 结果里的下标，渲染阶段靠它们原地替换区段，
	// 免得第二处再扫一遍标记、两边判读口径漂开。只有 Parse 的成功路径会填。
	beginAt int
	endAt   int
}

// Usable 表示区段结构没问题、Records 可信。没有区段也算可用，那时就是零条记录。
func (b Block) Usable() bool { return b.Anomaly == AnomalyNone }

// HasRecord 判断某个主机名在区段里有没有本工具写的映射。
// 只按位置判断，不判断 IP 对不对——期望 IP 要等写入阶段的摘要才知道。
func (b Block) HasRecord(hostname string) bool {
	want := NormalizeHostname(hostname)
	if want == "" {
		return false
	}
	for _, rec := range b.Records {
		for _, host := range rec.Hostnames {
			if host == want {
				return true
			}
		}
	}
	return false
}

// NormalizeHostname 把主机名收成比较用的形状：小写、去首尾空格、去尾随点。
// 目录里存的已经是这个形状，Hosts 文件里的写法五花八门，比较前都要过这里。
func NormalizeHostname(hostname string) string {
	host := strings.ToLower(strings.TrimSpace(hostname))
	return strings.TrimSuffix(host, ".")
}

// validHostname 是主机名的唯一判定：规范化后非空、不含路径/端口这类字符、本身不是 IP。
// 读区段和渲染区段共用这一条，两边不会出现「读得进但写不出」的口径差。
func validHostname(host string) bool {
	if host == "" || strings.ContainsAny(host, "/@:*") {
		return false
	}
	return net.ParseIP(host) == nil
}

// Parse 从 Hosts 全文里找出本工具的管理区段并读出的记录。
// 它对任何输入都不报错：结构问题走 Anomaly，由调用方按冲突档显示，绝不猜测修复。
func Parse(content string) Block {
	lines := splitLines(content)

	var begins, ends []markerHit
	for i, line := range lines {
		if m := beginLine.FindStringSubmatch(line); m != nil {
			begins = append(begins, markerHit{version: m[1], index: i})
		}
		if m := endLine.FindStringSubmatch(line); m != nil {
			ends = append(ends, markerHit{version: m[1], index: i})
		}
	}
	if len(begins) == 0 && len(ends) == 0 {
		return Block{Present: false}
	}
	if len(begins) == 0 {
		return Block{Present: true, Anomaly: AnomalyMissingBegin,
			Reason: "只有结束标记、没有开始标记，区段没法判读。"}
	}
	if len(ends) == 0 {
		return Block{Present: true, Anomaly: AnomalyMissingEnd,
			Reason: "只有开始标记、没有结束标记，区段没法判读。"}
	}
	if len(begins) > 1 || len(ends) > 1 || ends[0].index < begins[0].index {
		return Block{Present: true, Anomaly: AnomalyDuplicated,
			Reason: "区段标记重复、套娃或顺序不对，区段没法判读。"}
	}

	begin, end := begins[0], ends[0]
	if begin.version != strconv.Itoa(ManagedVersion) {
		return Block{Present: true, Anomaly: AnomalyUnknownVersion,
			Reason: "区段版本是 v" + begin.version + "，本程序只认识 v" + strconv.Itoa(ManagedVersion) + "。"}
	}
	if end.version != begin.version {
		return Block{Present: true, Anomaly: AnomalyUnknownVersion,
			Reason: "开始标记和结束标记的版本不一致，区段没法判读。"}
	}

	records, anomaly, reason := readRecords(lines[begin.index+1:end.index], begin.index+2)
	block := Block{Present: true, Version: ManagedVersion, Records: records, beginAt: begin.index, endAt: end.index}
	if anomaly != AnomalyNone {
		block.Anomaly = anomaly
		block.Reason = reason
	}
	return block
}

type markerHit struct {
	version string
	index   int
}

// readRecords 读区段内部的行：空行跳过，其余必须是「IP 后跟至少一个主机名」。
// 带 # 的行（整行注释或行尾注释）都算外部改动——本工具写出去的区段里不会有注释。
// 看到残缺行也一样报 badRecord，绝不猜测修复。
func readRecords(lines []string, firstLine int) ([]Record, Anomaly, string) {
	records := make([]Record, 0, len(lines))
	for offset, raw := range lines {
		lineNumber := firstLine + offset
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		if strings.ContainsRune(text, '#') {
			return nil, AnomalyBadRecord, badLineText(lineNumber)
		}
		fields := strings.Fields(text)
		if len(fields) < 2 || net.ParseIP(fields[0]) == nil {
			return nil, AnomalyBadRecord, badLineText(lineNumber)
		}
		hostnames := make([]string, 0, len(fields)-1)
		for _, field := range fields[1:] {
			host := NormalizeHostname(field)
			if !validHostname(host) {
				return nil, AnomalyBadRecord, badLineText(lineNumber)
			}
			hostnames = append(hostnames, host)
		}
		records = append(records, Record{IP: fields[0], Hostnames: hostnames, Line: lineNumber})
	}
	return records, AnomalyNone, ""
}

func badLineText(lineNumber int) string {
	return "区段里第 " + strconv.Itoa(lineNumber) + " 行不是本工具写过的「IP + 主机名」映射，区段被外部改过。"
}

// splitLines 按行切开并去掉行尾的 \r；结尾空行不算一行内容。
func splitLines(content string) []string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	return lines
}
