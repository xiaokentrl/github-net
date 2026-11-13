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
	"strconv"
	"strings"
	"time"
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
// 大小以资产声明的 Size 为预检、以上限为闸；写 .part 文件再改名，不留半截成品。
// 支持断点续传（2026-11-14 升级实测把这条从三四档升为必须）：.part 带着已收字节，
// 下一次带 Range 从断点续；服务器不支持 Range（回 200 全量）就整段重来。
// onProgress 非空时按节拍汇报（received/total，节流约 200ms 一条，完成时补一条终值），
// 网络中断时保留 .part 供续传；摘要对不上（服务器给的段不对）才删掉重来。
func (c *Client) Download(ctx context.Context, asset UpdateAsset, destDir string, onProgress func(received, total int64)) (string, error) {
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
	// 已有同名且等长的成品：不重下（重复点击、交接后重试不再整包重来）。
	// 内容对不对由调用方的验签闸把关——验不过会被删掉，下一次自然走正常下载。
	dest := filepath.Join(destDir, asset.Name)
	if st, err := os.Stat(dest); err == nil && asset.Size > 0 && st.Size() == asset.Size {
		return dest, nil
	}
	// 断点续传：.part 是固定名（隐藏前缀，PruneCaches 的归属门认它），有已收字节就带 Range 续。
	partPath := filepath.Join(destDir, "."+asset.Name+".part")
	resumeFrom := int64(0)
	if st, err := os.Stat(partPath); err == nil && st.Size() > 0 {
		resumeFrom = st.Size()
		if asset.Size > 0 && resumeFrom == asset.Size {
			// 残片已经收满声明长度（收满后进程在改名前被杀是常见动作）：
			// 直接改名成成品——再发 Range 会越界吃 416，重试永远失败成死锁。
			if err := os.Rename(partPath, filepath.Join(destDir, asset.Name)); err == nil {
				return filepath.Join(destDir, asset.Name), nil
			}
		}
		if asset.Size > 0 && resumeFrom > asset.Size {
			// 残片比声明还大：内容不可信，整段重来。
			_ = os.Remove(partPath)
			resumeFrom = 0
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", fmt.Errorf("%w：%v", ErrNetwork, err)
	}
	if resumeFrom > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
	}
	resp, err := c.download.Do(req)
	if err != nil {
		// 双 %w 保住错误链：重定向被白名单拦下的那个 ErrBadData 藏在 url.Error 里，
		// 不保链的话上层 errors.Is 只认得到 ErrNetwork，认不出真实原因。
		return "", fmt.Errorf("%w：%w", ErrNetwork, err)
	}
	defer resp.Body.Close()
	// 206＝服务器答应续传——但起点要对：解析 Content-Range 的 start，与 resumeFrom
	// 不一致（CDN/反代异常）就当整段重来，盲 O_APPEND 会拼出坏文件。
	// 416＝残片越界（起点已被服务器认为无效）：残片作废整段重来，不再报网络错卡死重试。
	// 200＝不支持 Range 的全量响应，残片作废整段重来。
	switch {
	case resp.StatusCode == http.StatusPartialContent && resumeFrom > 0:
		if start, ok := contentRangeStart(resp.Header.Get("Content-Range")); ok && start != resumeFrom {
			resumeFrom = 0
		}
	case resp.StatusCode == http.StatusOK, resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			_ = os.Remove(partPath)
		}
		resumeFrom = 0
		// 416/200 都要重新请求（不带 Range）才能拿到全量。
		resp.Body.Close()
		req2, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
		if err != nil {
			return "", fmt.Errorf("%w：%v", ErrNetwork, err)
		}
		resp, err = c.download.Do(req2)
		if err != nil {
			return "", fmt.Errorf("%w：%w", ErrNetwork, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("%w：下载 %s 得到 HTTP %d", ErrNetwork, asset.Name, resp.StatusCode)
		}
	default:
		return "", fmt.Errorf("%w：下载 %s 得到 HTTP %d", ErrNetwork, asset.Name, resp.StatusCode)
	}

	flags := os.O_WRONLY | os.O_CREATE
	if resumeFrom > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	part, err := os.OpenFile(partPath, flags, 0o600)
	if err != nil {
		return "", fmt.Errorf("续传文件建不出来：%w", err)
	}
	// 错误分支也要关：弱网反复重试每失败一次漏一个 fd。成功路径显式关在前，重复关无害。
	defer func() { _ = part.Close() }()

	// 进度按节拍汇报：直发会随 Write 逐块刷屏；节流 200ms，完成时补一条终值。
	var tick func(n int64)
	if onProgress != nil {
		var last time.Time
		received := resumeFrom
		total := asset.Size
		if total <= 0 {
			total = resp.ContentLength
			if total < 0 {
				// chunked 无总长：报 0，前端走「已收 N 字节」分支，不算假百分比。
				total = 0
			}
		}
		tick = func(n int64) {
			received += n
			if now := time.Now(); now.Sub(last) >= progressTick {
				last = now
				onProgress(received, total)
			}
		}
	}
	received, err := io.Copy(part, io.TeeReader(io.LimitReader(resp.Body, maxDownloadSize), countingWriter{onWrite: tick}))
	if onProgress != nil {
		finalTotal := asset.Size
		if finalTotal <= 0 && resp.ContentLength > 0 {
			finalTotal = resp.ContentLength
		}
		onProgress(resumeFrom+received, finalTotal)
	}
	if err != nil {
		// 中断保留 .part：下一次从断点接着收（这正是续传存在的意义）。
		return "", fmt.Errorf("%w：下载 %s 中断：%v", ErrNetwork, asset.Name, err)
	}
	// 实收和声明对不上：要么传输断了，要么资产被人换过。两种都拒收；
	// 残片内容不可信，删掉让下一次整段重来。
	if asset.Size > 0 && resumeFrom+received != asset.Size {
		_ = os.Remove(partPath)
		return "", fmt.Errorf("资产 %s 实收 %d 字节，声明 %d 字节，拒收", asset.Name, resumeFrom+received, asset.Size)
	}
	if err := part.Close(); err != nil {
		return "", fmt.Errorf("临时文件关不上：%w", err)
	}
	if err := os.Rename(partPath, dest); err != nil {
		return "", fmt.Errorf("落盘失败：%w", err)
	}
	return dest, nil
}

// progressTick 是进度汇报的节拍：再快界面也读不出来，只会刷事件。
const progressTick = 200 * time.Millisecond

// countingWriter 把经过的字节数喂给进度回调（回调为空时零开销路径）。
type countingWriter struct {
	onWrite func(n int64)
}

func (w countingWriter) Write(b []byte) (int, error) {
	if w.onWrite != nil {
		w.onWrite(int64(len(b)))
	}
	return len(b), nil
}

// FetchChecksums 把 checksums.txt 拉回来取某一行，返回该文件的期望摘要；没有清单就返回空串。
// destDir 由调用方给，用的是升级包那个目录：清单跟着包走，落在同一个目录里才谈得上「超过 3 天的缓存统一清」——
// 以前丢在系统临时目录，那是全机共享的地方，按保留期限去扫它会误删别人家的同名文件。
func (c *Client) FetchChecksums(ctx context.Context, asset UpdateAsset, name, destDir string) (string, error) {
	if asset.Name == "" {
		return "", nil
	}
	fetch := func() ([]byte, error) {
		path, err := c.Download(ctx, asset, destDir, nil)
		if err != nil {
			return nil, err
		}
		defer os.Remove(path) // 清单取完即删：落点只留当前这次下载的东西。
		return os.ReadFile(path)
	}
	raw, err := fetch()
	if err != nil {
		return "", err
	}
	// 复用的成品可能是上一个版本的同名清单（Size 撞车，keep 表按名留了它）：
	// 目标行不在就删掉重下一次，不让用户吃一次假「清单读不出来」。
	if !strings.Contains(string(raw), name) {
		raw, err = fetch()
		if err != nil {
			return "", err
		}
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

// contentRangeStart 解析 "bytes=start-end/total" 里的 start；解析不出按「对不上」处理（ok=false）。
func contentRangeStart(header string) (int64, bool) {
	rest, found := strings.CutPrefix(header, "bytes=")
	if !found {
		return 0, false
	}
	start, _, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(start, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
