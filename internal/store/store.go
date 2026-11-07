// Package store 把每一轮检测的候选结论落进 SQLite（纯 Go 驱动 modernc.org/sqlite，
// 不引 cgo，跨平台构建不添变数）。库放在应用的配置目录旁边，进程重启后数据还在；
// 它只是「发生过什么的底账」：界面看最近一轮仍然走内存里的 detectResultStore，
// 这里不参与实时路径，写失败也不影响检测。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaokentrl/github-net/internal/accelerator"
	"github.com/xiaokentrl/github-net/internal/retention"
	_ "modernc.org/sqlite"
)

// keepRounds 是底账的条数兜底（按轮记，一轮一行）：只留最近这么多轮，轮下的候选随外键级联一起删。
// 真正的主上限是时间——见 Prune 里的 retention.MaxAge（任何缓存最多存 3 天）。条数这道留着是为了
// 极端高频：万一 3 天里跑了几千轮，也不让库无限涨下去。
const keepRounds = 500

// Store 是打开着的候选底账。所有方法并发安全（database/sql 自带连接池串行化）。
type Store struct {
	db *sql.DB
	// now 是清理时取「现在」的时钟，测试注入假时钟来验保留期限两侧的行为。
	now func() time.Time
}

// RoundRow 是底账里的一轮：谁、用哪个方法、什么时候、结论数字是多少。
type RoundRow struct {
	ID          int64  // 底账里的行号，读某轮的候选用它
	TaskRound   string // 队列那一侧的轮 ID；单发检测没有批量轮就是空串
	ItemID      string
	ScopeID     string
	StrategyID  string
	StartedAt   string
	FinishedAt  string
	Available   int
	ProbesUsed  int
	DNSQueries  int
	Truncated   bool
	DeadlineHit bool
	Cancelled   bool
}

// CandidateRow 是底账里的一条候选，字段和 accelerator.Candidate 一一对应
// （布尔在库里是 0/1，读出来还原成 bool）。
type CandidateRow struct {
	Hostname    string
	CandidateID string
	IP          string
	IPv6        bool
	Status      string
	LatencyMS   int64
	TLSMS       int64
	Reason      string
	Applied     bool
}

// Open 打开（或新建）path 指向的底账库，建好两张表。父目录不存在就一并创建。
// 外键和忙等在 DSN 里开：级联删候选靠外键，busy_timeout 兜住偶发的锁竞争。
func Open(path string) (*Store, error) {
	if filepath.Dir(path) != "." {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("store: 建不了数据库目录: %w", err)
		}
	}
	// 单连接就够：底账写入只在每轮收尾那一刻发生，量也就几十行，
	// 限一避免多连接下的锁竞争，pragma 也只要设一次。
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: 打不开候选底账: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, now: time.Now}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// useClock 把清理用的时钟换成测试给的那一份（只影响「保留期限怎么算」，不改写进去的时间）。
func (s *Store) useClock(now func() time.Time) {
	s.now = now
}

func (s *Store) migrate() error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS detect_rounds (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			task_round   TEXT NOT NULL DEFAULT '',
			item_id      TEXT NOT NULL,
			scope_id     TEXT NOT NULL,
			strategy_id  TEXT NOT NULL,
			started_at   TEXT NOT NULL,
			finished_at  TEXT NOT NULL,
			available    INTEGER NOT NULL,
			probes_used  INTEGER NOT NULL,
			dns_queries  INTEGER NOT NULL,
			truncated    INTEGER NOT NULL,
			deadline_hit INTEGER NOT NULL,
			cancelled    INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_rounds_item ON detect_rounds(item_id)`,
		`CREATE TABLE IF NOT EXISTS detect_candidates (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			round_row    INTEGER NOT NULL REFERENCES detect_rounds(id) ON DELETE CASCADE,
			hostname     TEXT NOT NULL,
			candidate_id TEXT NOT NULL,
			ip           TEXT NOT NULL,
			ipv6         INTEGER NOT NULL,
			status       TEXT NOT NULL,
			latency_ms   INTEGER NOT NULL,
			tls_ms       INTEGER NOT NULL,
			reason       TEXT NOT NULL DEFAULT '',
			applied      INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_candidates_round ON detect_candidates(round_row)`,
		// candidate_ips 是按 IP 去重的候选池：每个「主机名 + IP」只有一行，
		// 重复写入只更新最新状态——它回答「这个域名现在有哪些已知地址、最后一次测是什么结果」。
		// 轮底账（上面两张表）管「每轮发生了什么」，两份各管各的。
		`CREATE TABLE IF NOT EXISTS candidate_ips (
			hostname   TEXT NOT NULL,
			ip         TEXT NOT NULL,
			ipv6       INTEGER NOT NULL,
			status     TEXT NOT NULL,
			latency_ms INTEGER NOT NULL,
			source_id  TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (hostname, ip)
		)`,
	} {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("store: 建表失败: %w", err)
		}
	}
	return nil
}

// Close 关掉底层库。
func (s *Store) Close() error { return s.db.Close() }

// SaveRound 把一轮的结论整笔写进底账：先写轮行，再把这轮的候选逐条挂上去。
// 候选为空也照写轮行——「这一轮没查出东西」本身是发生过的事实。
func (s *Store) SaveRound(taskRound, scopeID, strategyID string, r *accelerator.Result) error {
	if r == nil {
		return fmt.Errorf("store: 空结果不落盘")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: 开不了事务: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(
		`INSERT INTO detect_rounds
			(task_round, item_id, scope_id, strategy_id, started_at, finished_at,
			 available, probes_used, dns_queries, truncated, deadline_hit, cancelled)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		taskRound, r.ItemID, scopeID, strategyID,
		r.StartedAt.UTC().Format(timeFormat), r.FinishedAt.UTC().Format(timeFormat),
		r.Available, r.ProbesUsed, r.DNSQueries, b2i(r.Truncated), b2i(r.DeadlineHit), b2i(r.Cancelled),
	)
	if err != nil {
		return fmt.Errorf("store: 轮行写不进去: %w", err)
	}
	roundRow, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("store: 拿不到轮行号: %w", err)
	}
	// 缓存语义（拍板 2026-11-03 重写）：检测只是采集，数据只增不减。
	// 只有「这一轮查出了可用地址」的主机名才整块替换（旧的最优被新的最优顶掉）；
	// 本轮没有可用候选的主机名，池里的旧可用地址原样保留——它仍是最优已知，
	// 一轮空结果不构成把它丢掉的理由（切源后新源没覆盖的域名就靠这条活下来）。
	covered := make(map[string]bool)
	for _, c := range r.Candidates {
		if c.Status == accelerator.StatusAvailable {
			covered[c.Hostname] = true
		}
	}
	if len(covered) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(covered)), ",")
		args := make([]any, 0, len(covered))
		for hostname := range covered {
			args = append(args, hostname)
		}
		if _, err := tx.Exec(`DELETE FROM candidate_ips WHERE hostname IN (`+ph+`)`, args...); err != nil {
			return fmt.Errorf("store: 清旧缓存失败: %w", err)
		}
	}
	for _, c := range r.Candidates {
		if _, err := tx.Exec(
			`INSERT INTO detect_candidates
				(round_row, hostname, candidate_id, ip, ipv6, status, latency_ms, tls_ms, reason, applied)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			roundRow, c.Hostname, c.ID, c.IP, b2i(c.IPv6), string(c.Status),
			c.LatencyMS, c.TLSMS, c.Reason, b2i(c.Applied),
		); err != nil {
			return fmt.Errorf("store: 候选行写不进去: %w", err)
		}
		// 同主机名同 IP 已存在就更新成这次的结果，不另起一行（去重）。
		// 只写 covered 的主机名：没覆盖的域名连更新都不做，旧缓存一字不动。
		if !covered[c.Hostname] {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO candidate_ips
				(hostname, ip, ipv6, status, latency_ms, source_id, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT (hostname, ip) DO UPDATE SET
				ipv6 = excluded.ipv6, status = excluded.status, latency_ms = excluded.latency_ms,
				source_id = excluded.source_id, updated_at = excluded.updated_at`,
			c.Hostname, c.IP, b2i(c.IPv6), string(c.Status), c.LatencyMS,
			strategyID, r.FinishedAt.UTC().Format(timeFormat),
		); err != nil {
			return fmt.Errorf("store: 候选池写不进去: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: 落盘提交失败: %w", err)
	}
	return s.Prune()
}

// Prune 把不该再留的底账删掉（拍板 2026-11-05：任何缓存最多存 3 天，超期自动清理）。三刀：
//  1. 超过保留期限的轮整轮删掉，轮下的候选随外键级联一起走，不留孤儿行；
//  2. 超过保留期限没再测过的地址行删掉——某个域名只要不再检测，它的存量地址就会一直躺着，
//     原来没有任何东西管它；
//  3. 最近 keepRounds 轮之外的旧轮删掉（条数兜底，防极端高频把库撑大）。
//
// 期限用全局那一个数字 retention.MaxAge，这里不再自己写天数。这几处时间只由本包按
// UTC 的 RFC3339 定宽写法写入（见 timeFormat 和三处 .UTC().Format(timeFormat)），
// 同一写法下字符串比大小就是时间比先后，所以直接拿来当删除界限。
func (s *Store) Prune() error {
	cutoff := s.now().UTC().Add(-retention.MaxAge).Format(timeFormat)
	if _, err := s.db.Exec(`DELETE FROM detect_rounds WHERE finished_at < ?`, cutoff); err != nil {
		return fmt.Errorf("store: 清理超期轮失败: %w", err)
	}
	if _, err := s.db.Exec(`DELETE FROM candidate_ips WHERE updated_at < ?`, cutoff); err != nil {
		return fmt.Errorf("store: 清理超期地址失败: %w", err)
	}
	if _, err := s.db.Exec(
		`DELETE FROM detect_rounds WHERE id NOT IN
			(SELECT id FROM detect_rounds ORDER BY id DESC LIMIT ?)`, keepRounds,
	); err != nil {
		return fmt.Errorf("store: 清理旧轮失败: %w", err)
	}
	return nil
}

// ClearAll 把候选底账整份清空：逐轮候选、轮底账、按 IP 去重的地址池一起删。
// 一次事务走完，任何一步失败都整体回滚，不留「轮没了、地址池还在」的半截状态。
// 返回真删掉的轮数与地址行数，界面按这两个数字报回执，不自己猜清了多少。
func (s *Store) ClearAll() (int64, int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, fmt.Errorf("store: 开始清空底账失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM detect_candidates`); err != nil {
		return 0, 0, fmt.Errorf("store: 清空逐轮候选失败: %w", err)
	}
	roundRes, err := tx.Exec(`DELETE FROM detect_rounds`)
	if err != nil {
		return 0, 0, fmt.Errorf("store: 清空轮底账失败: %w", err)
	}
	ipRes, err := tx.Exec(`DELETE FROM candidate_ips`)
	if err != nil {
		return 0, 0, fmt.Errorf("store: 清空地址池失败: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("store: 提交清空底账失败: %w", err)
	}
	rounds, _ := roundRes.RowsAffected()
	addresses, _ := ipRes.RowsAffected()
	return rounds, addresses, nil
}

// CountAll 只数不清：返回候选底账里的轮数（detect_rounds）和地址池行数（candidate_ips）。
// 预览「全部 IP 缓存」要看数量，照着这个数就行；库没接上时调用方已按 nil 挡过。
func (s *Store) CountAll() (int64, int64, error) {
	var rounds, addresses int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM detect_rounds`).Scan(&rounds); err != nil {
		return 0, 0, fmt.Errorf("store: 数轮数失败: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM candidate_ips`).Scan(&addresses); err != nil {
		return 0, 0, fmt.Errorf("store: 数地址数失败: %w", err)
	}
	return rounds, addresses, nil
}

// CountCandidateRows 数逐轮候选记录数（detect_candidates）。
// 「清理全部 IP 缓存」会整表删掉它，预览要如实报出来，不能只报轮数和地址数。
func (s *Store) CountCandidateRows() (int64, error) {
	var rows int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM detect_candidates`).Scan(&rows); err != nil {
		return 0, fmt.Errorf("store: 数逐轮候选失败: %w", err)
	}
	return rows, nil
}

// Rounds 按时间倒序取最近 limit 轮。limit 不大于 0 就取上限以内全部。
func (s *Store) Rounds(limit int) ([]RoundRow, error) {
	q := `SELECT id, task_round, item_id, scope_id, strategy_id, started_at, finished_at,
		available, probes_used, dns_queries, truncated, deadline_hit, cancelled
		FROM detect_rounds ORDER BY id DESC`
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.db.Query(q)
	if err != nil {
		return nil, fmt.Errorf("store: 读轮列表失败: %w", err)
	}
	defer rows.Close()
	var out []RoundRow
	for rows.Next() {
		var r RoundRow
		var truncated, deadlineHit, cancelled int
		if err := rows.Scan(&r.ID, &r.TaskRound, &r.ItemID, &r.ScopeID, &r.StrategyID,
			&r.StartedAt, &r.FinishedAt, &r.Available, &r.ProbesUsed, &r.DNSQueries,
			&truncated, &deadlineHit, &cancelled); err != nil {
			return nil, fmt.Errorf("store: 轮行读不出来: %w", err)
		}
		r.Truncated, r.DeadlineHit, r.Cancelled = truncated != 0, deadlineHit != 0, cancelled != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// Candidates 取某一轮底账里的全部候选行，按写入顺序（也就是探测定格顺序）返回。
func (s *Store) Candidates(roundRow int64) ([]CandidateRow, error) {
	rows, err := s.db.Query(
		`SELECT hostname, candidate_id, ip, ipv6, status, latency_ms, tls_ms, reason, applied
		 FROM detect_candidates WHERE round_row = ? ORDER BY id`, roundRow)
	if err != nil {
		return nil, fmt.Errorf("store: 读候选失败: %w", err)
	}
	defer rows.Close()
	var out []CandidateRow
	for rows.Next() {
		var c CandidateRow
		var ipv6, applied int
		if err := rows.Scan(&c.Hostname, &c.CandidateID, &c.IP, &ipv6, &c.Status,
			&c.LatencyMS, &c.TLSMS, &c.Reason, &applied); err != nil {
			return nil, fmt.Errorf("store: 候选行读不出来: %w", err)
		}
		c.IPv6, c.Applied = ipv6 != 0, applied != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// PoolRow 是候选池里的一行：一个「主机名 + IP」的最新已知状态。
type PoolRow struct {
	Hostname  string
	IP        string
	IPv6      bool
	Status    string
	LatencyMS int64
	SourceID  string
	UpdatedAt string
}

// Pool 按主机名分组读出整份候选池（组内按 IP 排序）。hostname 不为空就只看这个主机名。
func (s *Store) Pool(hostname string) ([]PoolRow, error) {
	q := `SELECT hostname, ip, ipv6, status, latency_ms, source_id, updated_at FROM candidate_ips`
	var args []any
	if hostname != "" {
		q += ` WHERE hostname = ?`
		args = append(args, hostname)
	}
	q += ` ORDER BY hostname, ip`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: 读候选池失败: %w", err)
	}
	defer rows.Close()
	var out []PoolRow
	for rows.Next() {
		var p PoolRow
		var ipv6 int
		if err := rows.Scan(&p.Hostname, &p.IP, &ipv6, &p.Status, &p.LatencyMS, &p.SourceID, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: 候选池行读不出来: %w", err)
		}
		p.IPv6 = ipv6 != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// timeFormat 是底账里时间的统一写法：UTC 的 RFC3339，和内存结果视图一致。
const timeFormat = time.RFC3339

// MarkPoolProbe 把应用前复验的新结论刷回候选池那一行：只改状态、延迟、测量时间三列，
// 来源和 IPv6 标记一个字不动。行不存在就当无事发生——内存那一轮来的候选本来就不必在池里有行，
// 那部分的事实由内存结果和 SaveRound 管。
func (s *Store) MarkPoolProbe(hostname, ip, status string, latencyMS int64, at time.Time) error {
	if _, err := s.db.Exec(
		`UPDATE candidate_ips SET status = ?, latency_ms = ?, updated_at = ?
		 WHERE hostname = ? AND ip = ?`,
		status, latencyMS, at.UTC().Format(timeFormat), hostname, ip,
	); err != nil {
		return fmt.Errorf("store: 候选池写不回复验结论: %w", err)
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
