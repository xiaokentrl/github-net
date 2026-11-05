package app

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// 状态配色表的门：后端声明的每一档状态，前端那套「状态 → 颜色档」都得逐字登记。
// 漏一档，那个状态在界面上就没有颜色——最要紧的是检测/应用失败不显红，
// 而类型检查抓不到这种漏；多一档说明前端还留着后端已经删掉的状态，一样算红。

var (
	goTaskStatus  = regexp.MustCompile(`^\s*Status[A-Za-z]+\s+Status\s*=\s*"([a-zA-Z]+)"`)
	goCandStatus  = regexp.MustCompile(`^\s*Status[A-Za-z]+\s+CandidateStatus\s*=\s*"([a-zA-Z]+)"`)
	goHostsStatus = regexp.MustCompile(`^\s*Hosts[A-Za-z]+\s+HostsStatus\s*=\s*"([a-zA-Z]+)"`)
	tsToneKey     = regexp.MustCompile(`^\s*'([a-zA-Z]+)'\s*:`)
)

func goStatusSet(t *testing.T, root, rel string, re *regexp.Regexp) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, line := range strings.Split(readAt(t, root, rel), "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			out[m[1]] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("没从 %s 抓到状态，门自己的正则失效了", rel)
	}
	return out
}

func tsToneTable(t *testing.T, src, name string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	in, found := false, false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !in {
			if strings.HasPrefix(trimmed, "export const "+name+":") {
				in, found = true, true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "}") {
			break
		}
		if m := tsToneKey.FindStringSubmatch(line); m != nil {
			out[m[1]] = true
		}
	}
	if !found || len(out) == 0 {
		t.Fatalf("没读到 %s 这张表，门读不出清单", name)
	}
	return out
}

func assertSameStatusSets(t *testing.T, label string, backend, frontend map[string]bool) {
	t.Helper()
	var missing, extra []string
	for k := range backend {
		if !frontend[k] {
			missing = append(missing, k)
		}
	}
	for k := range frontend {
		if !backend[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("%s 对不上：前端漏登记 %v，前端多登记 %v", label, missing, extra)
	}
}

func TestStatusToneTablesCoverEveryBackendStatus(t *testing.T) {
	root := repoRoot(t)
	src := readAt(t, root, filepath.Join("frontend", "src", "features", "tasks", "statusTone.ts"))

	assertSameStatusSets(t, "任务状态",
		goStatusSet(t, root, filepath.Join("internal", "task", "task.go"), goTaskStatus),
		tsToneTable(t, src, "TASK_TONE"))
	assertSameStatusSets(t, "候选状态",
		goStatusSet(t, root, filepath.Join("internal", "accelerator", "detect.go"), goCandStatus),
		tsToneTable(t, src, "CANDIDATE_TONE"))
	assertSameStatusSets(t, "Hosts 状态",
		goStatusSet(t, root, filepath.Join("internal", "app", "dto.go"), goHostsStatus),
		tsToneTable(t, src, "HOSTS_TONE"))
}
