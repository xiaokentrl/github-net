package updater

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// 下载安全上限：正常安装包十几 MB，512MB 是防失控的闸，不是预期用量。
const maxDownloadSize = 512 << 20

// PickAssets 从发布资产里挑出「当前平台的安装包 + 它的签名 + 校验和清单」。
// 命名约定（发布时照此挂资产）：github-net_<版本>_<arch>.deb / .rpm / .exe / .dmg，
// 签名是同名加 .sig，摘要清单是 checksums.txt。挑不到安装包给明确错误，
// 不挑「长得像」的替代品——装错包比不装更糟。
func PickAssets(info Info) (installer, sig, checksums UpdateAsset, err error) {
	suffix := installerSuffix(runtime.GOOS)
	arch := runtime.GOARCH
	for _, a := range info.Assets {
		if strings.HasSuffix(a.Name, ".sig") || strings.HasSuffix(a.Name, ".txt") {
			continue
		}
		if strings.HasSuffix(a.Name, suffix) && strings.Contains(a.Name, arch) {
			installer = a
			break
		}
	}
	if installer.Name == "" {
		return installer, sig, checksums, fmt.Errorf("这次发布没有 %s/%s 的安装包资产", runtime.GOOS, arch)
	}
	for _, a := range info.Assets {
		if a.Name == installer.Name+".sig" {
			sig = a
		}
		if a.Name == "checksums.txt" {
			checksums = a
		}
	}
	if sig.Name == "" {
		return installer, sig, checksums, fmt.Errorf("安装包 %s 没有配套的 .sig 签名，拒绝下载", installer.Name)
	}
	return installer, sig, checksums, nil
}

func installerSuffix(goos string) string {
	switch goos {
	case "windows":
		return ".exe"
	case "darwin":
		return ".dmg"
	default:
		return ".deb"
	}
}

// Download 把一个资产拉到 destDir，返回落盘路径。
// 大小以资产声明的 Size 为预检、以上限为闸；写临时文件再改名，不留半截文件。
func (c *Client) Download(ctx context.Context, asset UpdateAsset, destDir string) (string, error) {
	if asset.Size > maxDownloadSize {
		return "", fmt.Errorf("资产 %s 声明大小 %d 字节，超过下载上限", asset.Name, maxDownloadSize)
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", fmt.Errorf("建下载目录失败：%w", err)
	}
	// 资产来源白名单（安全边界方案：更新信任根）：只接受白名单里的协议+域。
	// 上游应答被污染或账号被攻破时，下载可指向任意域——验签挡「装坏包」，白名单挡「下载哪」。
	// 生产白名单全是 https 的 GitHub 域，所以「必须是 https」由白名单本身兜住，
	// 不再单独判 scheme（判了会把测试假服务器的 http 资产一并拦死）。
	u, err := url.Parse(asset.URL)
	if err != nil {
		return "", fmt.Errorf("%w: 资产地址解析不了：%q", ErrBadData, asset.URL)
	}
	if !assetOriginAllowed(u, c.allowedAssetOrigins) {
		return "", fmt.Errorf("%w: 资产来源 %q 不在白名单", ErrBadData, u.Scheme+"://"+u.Host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", fmt.Errorf("%w：%v", ErrNetwork, err)
	}
	resp, err := c.download.Do(req)
	if err != nil {
		// 双 %w 保住错误链：重定向被白名单拦下的那个 ErrBadData 藏在 url.Error 里，
		// 不保链的话上层 errors.Is 只认得到 ErrNetwork，认不出真实原因。
		return "", fmt.Errorf("%w：%w", ErrNetwork, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w：下载 %s 得到 HTTP %d", ErrNetwork, asset.Name, resp.StatusCode)
	}

	tmp, err := os.CreateTemp(destDir, "."+asset.Name+".*")
	if err != nil {
		return "", fmt.Errorf("临时文件建不出来：%w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	written, err := io.Copy(tmp, io.LimitReader(resp.Body, maxDownloadSize))
	if err != nil {
		return "", fmt.Errorf("%w：下载 %s 中断：%v", ErrNetwork, asset.Name, err)
	}
	// 实收和声明对不上：要么传输断了，要么资产被人换过。两种都拒收。
	if asset.Size > 0 && written != asset.Size {
		return "", fmt.Errorf("资产 %s 实收 %d 字节，声明 %d 字节，拒收", asset.Name, written, asset.Size)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("临时文件关不上：%w", err)
	}
	dest := filepath.Join(destDir, asset.Name)
	if err := os.Rename(tmpName, dest); err != nil {
		return "", fmt.Errorf("落盘失败：%w", err)
	}
	return dest, nil
}

// FetchChecksums 把 checksums.txt 拉回来取某一行，返回该文件的期望摘要；没有清单就返回空串。
// destDir 由调用方给，用的是升级包那个目录：清单跟着包走，落在同一个目录里才谈得上「超过 3 天的缓存统一清」——
// 以前丢在系统临时目录，那是全机共享的地方，按保留期限去扫它会误删别人家的同名文件。
func (c *Client) FetchChecksums(ctx context.Context, asset UpdateAsset, name, destDir string) (string, error) {
	if asset.Name == "" {
		return "", nil
	}
	path, err := c.Download(ctx, asset, destDir)
	if err != nil {
		return "", err
	}
	defer os.Remove(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && fields[1] == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksums.txt 里没有 %s 这一行", name)
}

// assetOriginAllowed 判断一个 URL 是否落在白名单里（按协议+域比对）。
// 首跳资产地址与重定向的每一跳都用它；白名单项坏了按「不允许」算，不按「放行」算。
func assetOriginAllowed(u *url.URL, origins []string) bool {
	for _, o := range origins {
		parsed, err := url.Parse(o)
		if err != nil {
			continue
		}
		if parsed.Scheme == u.Scheme && parsed.Host == u.Host {
			return true
		}
	}
	return false
}
