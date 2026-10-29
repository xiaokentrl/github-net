package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xiaokentrl/github-net/internal/updater"
)

// 更新检查的稳定错误码：前端按码分支给不同的话，不解析中文。
const (
	CodeUpdateNoRelease = "update_no_release"
	CodeUpdateLimited   = "update_rate_limited"
	CodeUpdateNetwork   = "update_network"
	CodeUpdateBadData   = "update_bad_data"
	// update_verify_failed：下载完成但摘要或签名校验没过，包已作废；绝不退回「不验签就装」。
	CodeUpdateVerifyFailed = "update_verify_failed"
	// update_handoff_failed：包已下载并验签，但没能交给系统安装流程（多半是没有桌面会话）；
	// 消息里带安装包路径，用户可以手动装。
	CodeUpdateHandoffFailed = "update_handoff_failed"
)

// CheckForUpdates 查 GitHub 最新稳定版并与当前版本比较。
// 只比较和显示；下载、校验、安装属安全更新流程，这里一步都不做。
func (s *Service) CheckForUpdates() (updater.Info, error) {
	info, err := s.updater.Check(context.Background(), s.version)
	if err != nil {
		return updater.Info{}, updatePublicError(err)
	}
	return info, nil
}

// UpdateDownloadResult 是「下载并安装」一次成功的结果：落盘路径交给桌面安装器后回给界面展示。
type UpdateDownloadResult struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// updatesDir 是升级包的落点：应用数据目录下的 updates/。交给桌面安装器之后文件还要留在原地
// 让用户完成安装，所以这里不即时删除；每次新的下载会先清掉上一轮的旧包。
func (s *Service) updatesDir() (string, error) {
	if s.settings != nil {
		return filepath.Join(filepath.Dir(filepath.Dir(s.settings.Path())), "updates"), nil
	}
	return filepath.Join(os.TempDir(), "github-net-updates"), nil
}

// DownloadUpdate 执行「下载并安装」的前半段：下载当前平台安装包 + 签名 + 校验清单，
// 双校验（SHA256 摘要 + Ed25519 验签）通过后交给系统桌面安装流程（Linux 上唤起软件安装器）。
// 签名校验用的公钥随包分发；应用里没有公钥就拒绝一切升级，绝不退回「不验签」。
func (s *Service) DownloadUpdate(info updater.Info) (UpdateDownloadResult, error) {
	if !info.UpdateAvailable {
		return UpdateDownloadResult{}, NewError(CodeUpdateBadData, "这次检查的结论是「没有更新」，没有包可下", nil)
	}
	installer, sig, checksums, err := updater.PickAssets(info)
	if err != nil {
		return UpdateDownloadResult{}, NewError(CodeUpdateBadData, err.Error(), err)
	}
	destDir, err := s.updatesDir()
	if err != nil {
		return UpdateDownloadResult{}, NewError(CodeInternal, "拿不到升级包存放目录", err)
	}
	// 上一轮留下的旧包先清掉：落点永远只有当前这一次下载的东西。
	if entries, _ := os.ReadDir(destDir); len(entries) > 0 {
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(destDir, e.Name()))
		}
	}

	installerPath, err := s.updater.Download(context.Background(), installer, destDir)
	if err != nil {
		return UpdateDownloadResult{}, updatePublicError(err)
	}
	sigPath, err := s.updater.Download(context.Background(), sig, destDir)
	if err != nil {
		return UpdateDownloadResult{}, updatePublicError(err)
	}
	sigRaw, err := os.ReadFile(sigPath)
	if err != nil {
		return UpdateDownloadResult{}, NewError(CodeUpdateVerifyFailed, "读签名文件失败", err)
	}

	got, err := updater.SHA256File(installerPath)
	if err != nil {
		return UpdateDownloadResult{}, NewError(CodeUpdateVerifyFailed, "算安装包摘要失败", err)
	}
	// 摘要比对（清单在才有得比）+ 验签，双闸都要过；任何一闸不过就整包作废。
	if want, err := s.updater.FetchChecksums(context.Background(), checksums, installer.Name); err != nil {
		return UpdateDownloadResult{}, NewError(CodeUpdateVerifyFailed, "校验清单读不出来，拒绝安装", err)
	} else if want != "" {
		if err := updater.VerifyChecksum(got, want); err != nil {
			return UpdateDownloadResult{}, NewError(CodeUpdateVerifyFailed, "安装包摘要和发布清单对不上，安装包已作废", err)
		}
	}
	if err := updater.VerifySignature(s.pubKey, got, strings.TrimSpace(string(sigRaw))); err != nil {
		return UpdateDownloadResult{}, NewError(CodeUpdateVerifyFailed, "安装包签名校验失败，安装包已作废", err)
	}

	if err := s.openWithSystem(installerPath); err != nil {
		// 下载和校验都成功了，包是好的；只是这一步环境里没有桌面安装流程可交接。
		// 把路径亮出来，用户可以手动安装——如实说两件事，不把整笔下载说成失败。
		return UpdateDownloadResult{}, NewError(CodeUpdateHandoffFailed,
			"安装包已下载并通过校验（"+installerPath+"），但没能交给系统安装流程："+err.Error(), err)
	}
	return UpdateDownloadResult{Path: installerPath, Size: installer.Size}, nil
}

// openWithSystem 请系统打开一个本地文件（安装包交给桌面安装器、Hosts 交给编辑器，同一动作）。
func (s *Service) openWithSystem(path string) error {
	if s.openPath != nil {
		return s.openPath(path)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// updatePublicError 把 updater 的四类失败翻成带稳定码的公开错误，原因留在 cause 里供日志。
func updatePublicError(err error) *Error {
	switch {
	case errors.Is(err, updater.ErrNoStableRelease):
		return NewError(CodeUpdateNoRelease, "这个仓库还没有已发布的稳定版", err)
	case errors.Is(err, updater.ErrRateLimited):
		return NewError(CodeUpdateLimited, "GitHub 限流了这次检查，稍后再试", err)
	case errors.Is(err, updater.ErrNetwork):
		return NewError(CodeUpdateNetwork, "网络检查失败，确认能访问 GitHub 后重试", err)
	case errors.Is(err, updater.ErrBadData):
		return NewError(CodeUpdateBadData, "GitHub 返回的数据不认识，可能需要升级本应用", err)
	default:
		return NewError(CodeInternal, "更新检查失败，请稍后重试", err)
	}
}
