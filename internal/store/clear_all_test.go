package store

import "testing"

// ClearAll 是用户主动点的「全清」：轮底账、逐轮候选、地址池三张表要一行不剩，
// 回执数字必须等于真删掉的行数。重复调用也不能报出已经不存在的行数。
func TestClearAllEmptiesEveryCacheTable(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveRound("r1", "github", "dns", sampleResult()); err != nil {
		t.Fatalf("准备一轮底账失败: %v", err)
	}
	tables := []string{"detect_rounds", "detect_candidates", "candidate_ips"}
	for _, table := range tables {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("读 %s 失败: %v", table, err)
		}
		if n == 0 {
			t.Fatalf("准备数据没写进 %s，这条用例验不了清空", table)
		}
	}

	rounds, addresses, err := s.ClearAll()
	if err != nil {
		t.Fatalf("清空失败: %v", err)
	}
	if rounds != 1 {
		t.Errorf("删掉的轮数应是 1，实际 %d", rounds)
	}
	if addresses == 0 {
		t.Error("地址池里有行，却报清掉 0 条")
	}
	for _, table := range tables {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("清空后读 %s 失败: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s 清空后还剩 %d 行", table, n)
		}
	}

	rounds2, addresses2, err := s.ClearAll()
	if err != nil || rounds2 != 0 || addresses2 != 0 {
		t.Errorf("再清一次应是 0/0 且不报错，实际 %d/%d err=%v", rounds2, addresses2, err)
	}
}
