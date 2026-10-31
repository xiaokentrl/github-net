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
	_ "modernc.org/sqlite"
)

// keepRounds 是底账的保留上限（按轮记，一轮一行）。候选底账只用来回看近期发生过什么，
// 不是要长期积累的数据集：超过上限就删最老的轮，轮下的候选随外键级联一起删。
const keepRounds = 500

// Store 是打开着的候选底账。所有方法并发安全（database/sql 自带连接池串行化）。
type Store struct {
	db *sql.DB
}

// RoundRow 是底账里的一轮：谁、用哪个方法、什么时候、结论数字是多少。
type RoundRow struct {
	ID         int64  // 底账里的行号，读某轮的候选用它
	TaskRound  string // 队列那一侧的轮 ID；单发检测没有批量轮就是空串
	ItemID     string
	ScopeID    string
	StrategyID string
	StartedAt  string
	FinishedAt string
	Available  int
	ProbesUsed int
	DNSQueries int
	Truncated  bool
	DeadlineHit bool
	Cancelled  bool
}

// CandidateRow 是底账里的一条候选，字段和 accelerator.Candidate 一一对应
// （布尔在库里是 0/1，读出来还原成 bool）。
type CandidateRow struct {
	Hostname     string
	CandidateID  string
	IP           string
	IPv6         bool
	Status       string
	LatencyMS    int64
	TLSMS        int64
	Reason       string
	Applied      bool
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
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
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
	// 缓存语义（拍板 2026-10-31）：这一轮覆盖到的主机名，池里的旧地址先清掉再写新的——
	// 池子回答的是「最近一轮这些域名查到了什么」，不是历史累积；检测过但没查到的域名旧地址也清，
	// 免得界面上留一份「上上轮的老底」冒充现状。
	if len(r.Hostnames) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(r.Hostnames)), ",")
		args := make([]any, 0, len(r.Hostnames))
		for _, h := range r.Hostnames {
			args = append(args, h.Hostname)
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
		// 同一笔进候选池：同主机名同 IP 已存在就更新成这次的结果，不另起一行（去重）。
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
	return s.prune()
}

// prune 把上限之外的旧轮删掉，轮下的候选随外键级联一起走。
func (s *Store) prune() error {
	if _, err := s.db.Exec(
		`DELETE FROM detect_rounds WHERE id NOT IN
			(SELECT id FROM detect_rounds ORDER BY id DESC LIMIT ?)`, keepRounds,
	); err != nil {
		return fmt.Errorf("store: 清理旧轮失败: %w", err)
	}
	return nil
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

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
