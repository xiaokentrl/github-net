package config

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// 候选预算的边界和档位数字，前后端各写了一份：后端用它判配置合法，前端用它挡输入框。
// 抄错一位的后果是看得见的：界面填得进去、点了却被后端拒，回读旧值，用户以为「改不动」；
// 反过来前端写大了，就是非法值一路发到后端。所以这里钉成相等。
//
// 130 和 1040 这两个数不是拍出来的，是目录里去重主机名数乘档位的每主机名候选上限，
// 那层推导关系由 internal/app/budget_parity_test.go 钉住（那边才同时看得见目录和配置）。

const networkPaneFile = "frontend/src/features/settings/NetworkPane.tsx"
const profileOptionsFile = "frontend/src/features/settings/profileOptions.ts"

func frontendNumber(t *testing.T, src, expr, what string) int {
	t.Helper()
	got := matchLines(t, src, expr, what)
	if len(got) != 1 {
		t.Fatalf("%s 应当只出现一次，实际 %d 次：%v", what, len(got), got)
	}
	value, err := strconv.Atoi(got[0])
	if err != nil {
		t.Fatalf("%s 不是整数：%q", what, got[0])
	}
	return value
}

func TestFrontendCandidateBoundsEqualBackendRange(t *testing.T) {
	src := readSource(t, repoRoot(t), networkPaneFile)
	if got := frontendNumber(t, src, `^const CANDIDATE_MIN = (\d+)$`, "前端候选下限"); got != MinCandidateProbes {
		t.Errorf("前端下限 %d 与后端 %d 不等", got, MinCandidateProbes)
	}
	if got := frontendNumber(t, src, `^const CANDIDATE_MAX = (\d+)$`, "前端候选上限"); got != MaxCandidateProbesLimit {
		t.Errorf("前端上限 %d 与后端 %d 不等", got, MaxCandidateProbesLimit)
	}
}

// 档位数字漂开的后果不是报错，是界面那句「一轮全量需要 N 次、还差 M 次」算错，
// 用户照着错话去调预算。所以三档的每主机名候选上限和并发数也要两边相等：
// 候选上限管「一共试多少个」，并发数管「同一时刻试几个」，检测执行编排真的按后者开工作协程，
// 前端只写一个更大的数就是「界面说有 8 路、实际只有 2 路」。
func TestFrontendProfileCandidateLimitsEqualBackend(t *testing.T) {
	root := repoRoot(t)
	src := readSource(t, root, profileOptionsFile)

	idsLine := matchLines(t, src, `^export const PROFILE_IDS: ProfileID\[\] = \[([^\]]+)\]$`, "前端档位清单")
	if len(idsLine) != 1 {
		t.Fatalf("档位清单应当只声明一次，实际 %d 次", len(idsLine))
	}
	ids := strings.Split(strings.ReplaceAll(idsLine[0], "'", ""), ",")
	bodies := matchLines(t, src, `^\s*(?:low|standard|full): \{ ([^}]+) \},$`, "前端档位数字条目")
	if len(bodies) != len(ids) {
		t.Fatalf("前端档位数字条目 %d 条，档位清单 %d 条，对不上", len(bodies), len(ids))
	}
	for i, id := range ids {
		id = strings.TrimSpace(id)
		wantCandidates, ok := ProfileCandidateLimit(id)
		if !ok {
			t.Fatalf("前端档位 %q 后端不认", id)
		}
		wantConcurrency, ok := ProfileConcurrency(id)
		if !ok {
			t.Fatalf("前端档位 %q 后端不认（并发数）", id)
		}
		gotCandidates := frontendSpec(t, bodies[i], `candidates: (\d+)`, id, "候选上限")
		gotConcurrency := frontendSpec(t, bodies[i], `concurrency: (\d+)`, id, "并发数")
		if gotCandidates != wantCandidates {
			t.Errorf("档位 %s 的每主机名候选上限：前端 %d、后端 %d", id, gotCandidates, wantCandidates)
		}
		if gotConcurrency != wantConcurrency {
			t.Errorf("档位 %s 的并发数：前端 %d、后端 %d", id, gotConcurrency, wantConcurrency)
		}
	}
}

// frontendSpec 从一档的条目正文里取一个数字。取不到或取到多个都算红——
// 少一个数字＝这一档的这条约束等于没在门里。
func frontendSpec(t *testing.T, body, expr, profileID, what string) int {
	t.Helper()
	m := regexp.MustCompile(expr).FindAllStringSubmatch(body, -1)
	if len(m) != 1 {
		t.Fatalf("档位 %s 的%s应当出现一次，实际 %d 次（条目：%q）", profileID, what, len(m), body)
	}
	value, err := strconv.Atoi(m[0][1])
	if err != nil {
		t.Fatalf("档位 %s 的%s不是整数：%q", profileID, what, m[0][1])
	}
	return value
}
