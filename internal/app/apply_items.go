package app

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/xiaokentrl/github-net/internal/privilege"
	"github.com/xiaokentrl/github-net/internal/task"
)

// 应用与还原的入口层。分工和检测那条一样：入口验形状（项目在目录、有检测结果、
// 服务可达与否不在这验——队列任务里会验并给出可见失败），执行器验内容并干活。
// 这一批命令都不自己写 Hosts：写系统的只有特权辅助服务（internal/privilege）。

// ApplyItems 把指定项目排进应用队列：把该项目最近一轮检测的可用候选写进系统 Hosts。
// 没检测过的项目在入口就被拒——「先检测、再应用」这条顺序不靠用户自觉。
func (s *Service) ApplyItems(itemIDs []string) ([]task.Task, error) {
	ids, err := s.uniqueKnownItems(itemIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		view, ok := s.detectResults.view(id)
		if !ok {
			return nil, NewError(CodeNoDetectResult, "这个项目还没有检测结果，先检测再应用", nil)
		}
		if !hasAvailable(view) {
			return nil, NewError(CodeNoDetectResult, "最近一轮检测没有可用的候选地址，先重新检测", nil)
		}
	}
	return s.enqueueAll(task.KindApply, ids, applyRoundName(len(ids)))
}

// RestoreItems 把指定项目排进还原队列：退回它应用前的映射。
// 项目在不在目录里要验；它有没有被应用过不在这验——还原没应用过的项目在服务侧是无害的空操作，
// 让队列里那条以「还原完成」收场即可，不给用户造一个必须先理解的错误。
func (s *Service) RestoreItems(itemIDs []string) ([]task.Task, error) {
	ids, err := s.uniqueKnownItems(itemIDs)
	if err != nil {
		return nil, err
	}
	singleName := ""
	if len(ids) == 1 {
		singleName, _, _ = s.findItem(ids[0])
	}
	return s.enqueueAll(task.KindRestore, ids, restoreRoundName(singleName, len(ids)))
}

// RestoreAllHosts 把全部还由工具管理的项目一次退回应用前状态，返回实际还原的项目数。
// 这是服务侧一笔原子操作、没有进度可言，所以不进队列：单独一条命令直接完成，
// 失败原样上抛给界面（错误码区分「服务没在跑」和「服务拒了」）。
func (s *Service) RestoreAllHosts() (int, error) {
	if s.priv == nil {
		return 0, NewError(CodeHelperUnavailable, "特权辅助服务没在运行，无法还原", nil)
	}
	n, err := s.priv.RestoreAll()
	if err != nil {
		return 0, privilegePublicError(err)
	}
	return n, nil
}

// OpenHosts 请系统用默认编辑器打开 Hosts 文件。打开动作由后端执行，
// 前端不碰进程；拿不到可展示失败，不静默。
func (s *Service) OpenHosts() error {
	path := s.hostsPath
	if path == "" {
		return NewError(CodeHostsFailed, "这个平台拿不到 Hosts 文件路径", nil)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		// start 是 cmd 的内建命令；路径是固定常量不是用户输入，没有注入面。
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		// Linux 桌面环境里 xdg-open 走默认关联；服务态没有会话，这命令只从界面进程发。
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return NewError(CodeHostsFailed, "没能打开 Hosts 文件："+err.Error(), err)
	}
	// 不等编辑器退出：打开成功就算完成，进程交给系统。
	go func() { _ = cmd.Wait() }()
	return nil
}

// uniqueKnownItems 是 Apply/Restore 两个入口共用的第一道门：
// 去掉空白与重复、整批非空、每个 ID 都在目录里。任何一项不合格整批拒绝。
func (s *Service) uniqueKnownItems(itemIDs []string) ([]string, error) {
	if s.productCatalogErr != nil {
		return nil, ToPublicError(s.productCatalogErr)
	}
	ids := make([]string, 0, len(itemIDs))
	seen := make(map[string]bool, len(itemIDs))
	for _, raw := range itemIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, NewError(CodeInvalidItemID, "没有要操作的项目", nil)
	}
	for _, id := range ids {
		if _, _, ok := s.findItem(id); !ok {
			return nil, NewError(CodeInvalidItemID, "产品目录里没有这个项目，刷新界面后再试", nil)
		}
	}
	return ids, nil
}

// hasAvailable 报告一轮结果里有没有「可用」的候选。
func hasAvailable(view DetectResultView) bool {
	for _, h := range view.Hosts {
		for _, c := range h.Candidates {
			if c.Status == "available" {
				return true
			}
		}
	}
	return false
}

// enqueueAll 把一批已通过校验的项目按给定类型一起排进队列，归进同一个新的轮次，
// 返回任务快照。轮名由调用方按原型规则给（界面根节点显示它），范围只在整批同属一个范围时记下。
// 排到一半失败只剩「应用正在退出」，前面已排进去的有效任务会随队列关闭被取消。
func (s *Service) enqueueAll(kind task.Kind, ids []string, roundName string) ([]task.Task, error) {
	scopeID := ""
	for _, id := range ids {
		_, sc, ok := s.findItem(id)
		if !ok {
			continue
		}
		if scopeID == "" {
			scopeID = sc
		} else if sc != scopeID {
			scopeID = ""
			break
		}
	}
	roundID := s.tasks.BeginRound(kind, scopeID, roundName)
	tasks := make([]task.Task, 0, len(ids))
	for _, id := range ids {
		name, itemScope, _ := s.findItem(id)
		t, err := s.tasks.Enqueue(task.NewRequest{Kind: kind, ItemID: id, Name: name, ScopeID: itemScope, RoundID: roundID})
		if err != nil {
			return nil, ToPublicError(err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// applyRoundName / restoreRoundName 是原型定下的两条轮名规则，写在一处免得界面自己拼第二套：
// 应用一批是「应用队列 · N 项」，恢复单个是「恢复 · 项目名」，恢复多个和恢复同类成一批「恢复队列 · N 项」。
func applyRoundName(count int) string {
	return "应用队列 · " + strconv.Itoa(count) + " 项"
}

func restoreRoundName(itemName string, count int) string {
	if count == 1 {
		return "恢复 · " + itemName
	}
	return "恢复队列 · " + strconv.Itoa(count) + " 项"
}

// privilegePublicError 把特权层失败翻成带稳定错误码的公开错误（只用于不走队列的直接命令）。
func privilegePublicError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case isHelperUnavailable(err):
		return NewError(CodeHelperUnavailable, "特权辅助服务没在运行，无法操作系统 Hosts；重新安装本应用可以把它带起来", err)
	case isHelperRefused(err):
		return NewError(CodeHostsFailed, "特权辅助服务拒绝了这次操作："+unwrapMessage(err), err)
	default:
		return NewError(CodeHostsFailed, "修改系统 Hosts 失败："+unwrapMessage(err), err)
	}
}

func isHelperUnavailable(err error) bool { return errors.Is(err, privilege.ErrHelperUnavailable) }
func isHelperRefused(err error) bool     { return errors.Is(err, privilege.ErrHelperRefused) }
