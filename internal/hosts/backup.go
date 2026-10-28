package hosts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

// ErrBadMetadata 是恢复元数据的形状不对（版本不认识、条目重复、IP 不是规范写法、所有者没排序……）。
// 按冻结的写入契约，这等同于「元数据损坏」：当成冲突停住，不猜着补，也不因为读不懂就当作没写过。
var ErrBadMetadata = errors.New("恢复元数据不可用")

// MetadataSchemaVersion 是这份元数据的格式版本。版本不认识就整体拒，不降级读、不迁移。
const MetadataSchemaVersion = 1

// Mapping 是一条应用请求：把这些主机名指到这个 IP。它来自检测/方法那一层，不带行号，
// 也不假定自己是区段里的全部内容——事务层会把它并进当前条目集。
type Mapping struct {
	IP        string
	Hostnames []string
}

// Entry 是元数据里当前由工具维护的一条映射：主机名、正在生效的 IP、认领它的项目集合。
//
// Owners 为空是有意允许的一种状态：某个项目还原后把主机名退回它「应用前」的旧映射，
// 这条映射还是工具写出去的，但已经没有任何活动项目认领它，所以单项还原删不掉它，
// 只能由下一次应用接手。
type Entry struct {
	Hostname string
	IP       string
	Owners   []string
}

// Ownership 是「这个项目动手之前，这个主机名在区段里是什么样子」。IP 为空有两种情况：
// 应用前它确实没有映射，还原时把映射整条撤掉；或者当时它被别的项目认领着，那条记录的存亡归对方管，
// 本项目还原时不该去恢复它。IP 非空表示应用前它有本工具写的映射（本项目自己的，或无主的旧映射），
// 还原时退回那个 IP。
type Ownership struct {
	Hostname string
	IP       string
}

// ProjectState 是一个活动项目的应用前快照。项目还原成功后，这条就从元数据里消失。
type ProjectState struct {
	ItemID string
	Before []Ownership
}

// Recovery 是 backups/hosts/ 里那一份、也是唯一一份恢复元数据。
//
// 三个字段各自管一件事，别混：
//   - Original：首次写入前的 Hosts 原文，只用于事务失败回滚，且更新时不许覆盖它。
//     它不是一整份文件的长期备份——契约要求「不另存重复的 Hosts 明文副本」，
//     所以这里只有这一份，而且回滚前必须确认目标文件仍匹配本次事务的预期版本。
//   - Digest：最后一次成功写入的区段摘要，用来判断「现在区段里的是不是我们写的那批映射」。
//   - Entries / Projects：当前工具映射与所有权，以及每个活动项目的应用前快照。
type Recovery struct {
	SchemaVersion int
	Original      string
	Digest        string
	Entries       []Entry
	Projects      []ProjectState
}

// Exists 表示手里这份是真实读回来的元数据，而不是「还没写过」的零值。
// 只有它为 false 时，本次写入才有资格把当前 Hosts 记成首次写入前的原始快照。
func (r Recovery) Exists() bool { return r.SchemaVersion != 0 }

// InUse 表示这份元数据还认账：还有工具映射，或者还有活动项目。
// 两者都空就等于「什么都没写过」，这时文件里若还挂着区段标记，就是对不上。
func (r Recovery) InUse() bool {
	return len(r.Entries) > 0 || len(r.Projects) > 0
}

// ActiveItemIDs 返回还认领着映射的项目 ID（含只留下旧映射的项目不算）。按 ID 排序，
// 还原顺序、测试断言都取这个顺序，不依赖 map 遍历顺序。
func (r Recovery) ActiveItemIDs() []string {
	ids := make([]string, 0, len(r.Projects))
	for _, p := range r.Projects {
		ids = append(ids, p.ItemID)
	}
	sort.Strings(ids)
	return ids
}

// AllReleased 表示所有工具管理记录都已还原：没有活动项目，也没有残留的工具映射。
// 落盘那层据此决定能不能删掉这份元数据——删之前还要重新读取并校验，不是这里能替它做的。
func (r Recovery) AllReleased() bool {
	return !r.InUse()
}

// Digest 算「区段里现在是哪批映射」的摘要，走渲染那一套同一个规范化（IP 规范写法、
// 行内与行间排序、去重）。所以它比的是映射集合，不是字节：行序变了、IP 换了一种等价写法、
// 行内主机名重复，都不算用户改了东西——下一次写入本来就会把它们规范成同一个形状，
// 拿它当冲突只会把用户挡在自己的工具外面。真改了映射（换 IP、加删主机名、写进注释或非法行）
// 一定会变，注释和非法行另有判读路径（Block.Anomaly），不靠摘要碰运气。
//
// 错误只来自调用方自己拼出来的 Block：本包 Parse 的结果不会走到这条（Parse 已经把非法行判成
// Anomaly），但 Digest 是对外函数，不吞这个错。
func Digest(b Block) (string, error) {
	if !b.Usable() {
		return "", fmt.Errorf("%w：区段判读不了，%s", ErrConflict, b.Reason)
	}
	lines, err := managedLines(b.Records)
	if err != nil {
		return "", err
	}
	preimage := fmt.Sprintf("present=%t\n%s", b.Present, strings.Join(lines, "\n"))
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:]), nil
}

// CheckStatus 是「当前 Hosts 与恢复元数据对不对得上」的三种结论。
type CheckStatus string

const (
	// CheckClean：两边都没有工具区段，可以干净起步。
	CheckClean CheckStatus = "clean"
	// CheckMatched：区段就是元数据记录的那批映射，可以继续写入或还原。
	CheckMatched CheckStatus = "matched"
	// CheckConflict：对不上，或者区段判读不了。必须停手，不覆盖未知内容，也不猜着修。
	CheckConflict CheckStatus = "conflict"
)

// Check 在做任何写入之前问一次：现在文件里的区段，跟元数据说的对不对得上。
// 契约要求「区段与已保存摘要不一致」「有工具标记但没有有效恢复点」都算冲突，
// 并且重新检测不许静默抹掉用户改动——这句话落到代码里就是这个函数的返回值。
func Check(content string, meta Recovery) (CheckStatus, string) {
	block := Parse(content)
	if block.Present && !block.Usable() {
		return CheckConflict, "区段结构判读不了：" + block.Reason
	}
	if !meta.InUse() {
		if block.Present {
			return CheckConflict, "文件里有本工具的区段标记，恢复元数据里却没有任何记录，" +
				"没法确认这段是谁写的，先停在这里，请人工处理。"
		}
		return CheckClean, ""
	}
	if !block.Present {
		return CheckConflict, "元数据说写过区段，文件里已经没有了，可能被手工删掉，先停在这里，请人工处理。"
	}
	got, err := Digest(block)
	if err != nil {
		return CheckConflict, err.Error()
	}
	if got != meta.Digest {
		return CheckConflict, "区段内容与最后一次成功写入的摘要对不上，说明被外部改过，先停在这里，请人工处理。"
	}
	return CheckMatched, ""
}

// metadataJSON 是元数据的落盘形状。字段与 Recovery 一一对应，单独一层是为了让「版本不认识就整体拒」
// 有个明确的地方：未知字段直接报错，不静默忽略——静默忽略等于把用户改过的文件读成合法元数据。
type metadataJSON struct {
	SchemaVersion int                `json:"schemaVersion"`
	Original      string             `json:"original"`
	Digest        string             `json:"digest"`
	Entries       []entryJSON        `json:"entries"`
	Projects      []projectStateJSON `json:"projects"`
}

type entryJSON struct {
	Hostname string   `json:"hostname"`
	IP       string   `json:"ip"`
	Owners   []string `json:"owners"`
}

type ownershipJSON struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
}

type projectStateJSON struct {
	ItemID string          `json:"itemId"`
	Before []ownershipJSON `json:"before"`
}

// Marshal 把元数据编成字节。写文件、权限、原子替换属特权服务那半边，这里只管形状。
func (r Recovery) Marshal() ([]byte, error) {
	if err := r.validate(); err != nil {
		return nil, err
	}
	out := metadataJSON{
		SchemaVersion: r.SchemaVersion,
		Original:      r.Original,
		Digest:        r.Digest,
	}
	for _, e := range r.Entries {
		out.Entries = append(out.Entries, entryJSON{Hostname: e.Hostname, IP: e.IP, Owners: e.Owners})
	}
	for _, p := range r.Projects {
		var before []ownershipJSON
		for _, o := range p.Before {
			before = append(before, ownershipJSON{Hostname: o.Hostname, IP: o.IP})
		}
		out.Projects = append(out.Projects, projectStateJSON{ItemID: p.ItemID, Before: before})
	}
	data, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("%w：编不出来，%w", ErrBadMetadata, err)
	}
	return data, nil
}

// UnmarshalMetadata 读回元数据并逐条验形状。任何一条不对就返回 ErrBadMetadata：
// 上层据此按「元数据损坏」处理，绝不返回一个半对的 Recovery 让写入继续。
func UnmarshalMetadata(data []byte) (Recovery, error) {
	if len(data) == 0 {
		return Recovery{}, fmt.Errorf("%w：内容是空的", ErrBadMetadata)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw metadataJSON
	if err := dec.Decode(&raw); err != nil {
		return Recovery{}, fmt.Errorf("%w：读不出来，%v", ErrBadMetadata, err)
	}
	if raw.SchemaVersion != MetadataSchemaVersion {
		return Recovery{}, fmt.Errorf("%w：格式版本是 %d，本程序只认 %d",
			ErrBadMetadata, raw.SchemaVersion, MetadataSchemaVersion)
	}
	r := Recovery{
		SchemaVersion: raw.SchemaVersion,
		Original:      raw.Original,
		Digest:        raw.Digest,
	}
	for _, e := range raw.Entries {
		r.Entries = append(r.Entries, Entry{Hostname: e.Hostname, IP: e.IP, Owners: e.Owners})
	}
	for _, p := range raw.Projects {
		var before []Ownership
		for _, o := range p.Before {
			before = append(before, Ownership{Hostname: o.Hostname, IP: o.IP})
		}
		r.Projects = append(r.Projects, ProjectState{ItemID: p.ItemID, Before: before})
	}
	if err := r.validate(); err != nil {
		return Recovery{}, err
	}
	return r, nil
}

// validate 是元数据的形状门。每一项都对应一条「如果错了会怎样」：
// 排序不对＝同一批数据能编出两个字节串；重复主机名＝两行都写进区段，谁赢看运气；
// IP 不是规范写法＝摘要跟着漂；所有者里空 ID＝还原时找不着主人。
func (r Recovery) validate() error {
	if r.SchemaVersion != MetadataSchemaVersion {
		return fmt.Errorf("%w：格式版本是 %d，本程序只认 %d", ErrBadMetadata, r.SchemaVersion, MetadataSchemaVersion)
	}
	if len(r.Digest) != sha256.Size*2 {
		return fmt.Errorf("%w：区段摘要 %q 不是 64 位十六进制", ErrBadMetadata, r.Digest)
	}
	rawDigest, err := hex.DecodeString(r.Digest)
	if err != nil || hex.EncodeToString(rawDigest) != r.Digest {
		return fmt.Errorf("%w：区段摘要 %q 不是小写十六进制", ErrBadMetadata, r.Digest)
	}
	seen := map[string]bool{}
	for i, e := range r.Entries {
		if e.Hostname != NormalizeHostname(e.Hostname) || !validHostname(e.Hostname) {
			return fmt.Errorf("%w：第 %d 条映射的主机名 %q 不是规范写法", ErrBadMetadata, i+1, e.Hostname)
		}
		if seen[e.Hostname] {
			return fmt.Errorf("%w：主机名 %s 在元数据里出现两次", ErrBadMetadata, e.Hostname)
		}
		seen[e.Hostname] = true
		if err := checkCanonicalIP(e.IP); err != nil {
			return fmt.Errorf("%w：主机名 %s 的 %w", ErrBadMetadata, e.Hostname, err)
		}
		if i > 0 && r.Entries[i-1].Hostname > e.Hostname {
			return fmt.Errorf("%w：映射条目没按主机名排序", ErrBadMetadata)
		}
		if err := checkOwners(e.Owners); err != nil {
			return fmt.Errorf("%w：主机名 %s 的 %w", ErrBadMetadata, e.Hostname, err)
		}
	}
	ids := map[string]bool{}
	for i, p := range r.Projects {
		if strings.TrimSpace(p.ItemID) == "" {
			return fmt.Errorf("%w：第 %d 个项目的 ID 是空的", ErrBadMetadata, i+1)
		}
		if ids[p.ItemID] {
			return fmt.Errorf("%w：项目 %s 在元数据里出现两次", ErrBadMetadata, p.ItemID)
		}
		ids[p.ItemID] = true
		if i > 0 && r.Projects[i-1].ItemID > p.ItemID {
			return fmt.Errorf("%w：项目快照没按 ID 排序", ErrBadMetadata)
		}
		hostSeen := map[string]bool{}
		for j, o := range p.Before {
			if o.Hostname != NormalizeHostname(o.Hostname) || !validHostname(o.Hostname) {
				return fmt.Errorf("%w：项目 %s 第 %d 条应用前记录的主机名 %q 不是规范写法",
					ErrBadMetadata, p.ItemID, j+1, o.Hostname)
			}
			if hostSeen[o.Hostname] {
				return fmt.Errorf("%w：项目 %s 的主机名 %s 在应用前快照里出现两次", ErrBadMetadata, p.ItemID, o.Hostname)
			}
			hostSeen[o.Hostname] = true
			if o.IP != "" {
				if err := checkCanonicalIP(o.IP); err != nil {
					return fmt.Errorf("%w：项目 %s 主机名 %s 的 %w", ErrBadMetadata, p.ItemID, o.Hostname, err)
				}
			}
			if j > 0 && p.Before[j-1].Hostname > o.Hostname {
				return fmt.Errorf("%w：项目 %s 的应用前快照没按主机名排序", ErrBadMetadata, p.ItemID)
			}
		}
	}
	// 认领关系必须两头都对得上：每条映射的每个认领项目，应用前快照里都得记着这个主机名。
	// 反过来说就是「项目认领了一条自己没有快照的映射」＝这条线断了，还原时不知道该把它放回哪个地址。
	recorded := make(map[string]map[string]bool, len(r.Projects))
	for _, p := range r.Projects {
		hosts := make(map[string]bool, len(p.Before))
		for _, o := range p.Before {
			hosts[o.Hostname] = true
		}
		recorded[p.ItemID] = hosts
	}
	for _, e := range r.Entries {
		for _, owner := range e.Owners {
			if !recorded[owner][e.Hostname] {
				return fmt.Errorf("%w：主机名 %s 的认领项目 %s 没有它的应用前快照", ErrBadMetadata, e.Hostname, owner)
			}
		}
	}
	return nil
}

func checkCanonicalIP(ip string) error {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return fmt.Errorf("%w：IP %q 不是合法地址", ErrBadMetadata, ip)
	}
	if parsed.String() != ip {
		return fmt.Errorf("%w：IP %q 不是规范写法（应为 %s）", ErrBadMetadata, ip, parsed.String())
	}
	return nil
}

func checkOwners(owners []string) error {
	for i, owner := range owners {
		if strings.TrimSpace(owner) == "" {
			return fmt.Errorf("%w：认领项目的 ID 是空的", ErrBadMetadata)
		}
		if i > 0 && owners[i-1] >= owner {
			return fmt.Errorf("%w：认领项目没排序去重（%s 跟在 %s 后面）", ErrBadMetadata, owner, owners[i-1])
		}
	}
	return nil
}
