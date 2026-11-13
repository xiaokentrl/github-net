package app

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaokentrl/github-net/internal/config"
	"github.com/xiaokentrl/github-net/internal/updater"
)

// testSigner 用测试私钥签摘要，返回 base64 签名——和 scripts/sign-release.sh 的契约同一份话。
func testSigner(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("生成测试密钥: %v", err)
	}
	return priv, pub
}

func signDigest(t *testing.T, priv ed25519.PrivateKey, sha256Hex string) string {
	t.Helper()
	sig := ed25519.Sign(priv, []byte(strings.ToLower(sha256Hex)))
	return base64.StdEncoding.EncodeToString(sig)
}

func TestDownloadUpdateVerifiesAndHandsOff(t *testing.T) {
	priv, pub := testSigner(t)
	deb := []byte("FAKE-DEB-PAYLOAD-0.2.0")
	sum := sha256.Sum256(deb)
	hexSum := hex.EncodeToString(sum[:])
	sig := signDigest(t, priv, hexSum)
	checksums := hexSum + "  github-net_0.2.0_amd64.deb\n"

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/releases/latest") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"tag_name":"v0.2.0","html_url":"https://github.com/x/releases/tag/v0.2.0",
				"assets":[
					{"name":"github-net_0.2.0_amd64.deb","browser_download_url":"%s/github-net_0.2.0_amd64.deb","size":%d},
					{"name":"github-net_0.2.0_amd64.deb.sig","browser_download_url":"%s/github-net_0.2.0_amd64.deb.sig","size":%d},
					{"name":"checksums.txt","browser_download_url":"%s/checksums.txt","size":%d}
				]}`, srv.URL, len(deb), srv.URL, len(sig), srv.URL, len(checksums))
			return
		}
		http.FileServer(http.Dir(srvDirWith(t, map[string][]byte{
			"github-net_0.2.0_amd64.deb":     deb,
			"github-net_0.2.0_amd64.deb.sig": []byte(sig),
			"checksums.txt":                  []byte(checksums),
		}))).ServeHTTP(w, r)
	}))
	defer srv.Close()

	svc := newUpdateService(t, srv.URL+"/releases/latest", pub)
	opened := withOpenSeam(t, svc)
	info, err := svc.CheckForUpdates()
	if err != nil {
		t.Fatalf("检查: %v", err)
	}
	result, err := svc.DownloadUpdate(info)
	if err != nil {
		t.Fatalf("下载并校验不该失败: %v", err)
	}
	// 校验通过后必须把安装包交给系统安装流程，且交给的就是落盘那颗。
	if len(*opened) != 1 || (*opened)[0] != result.Path {
		t.Fatalf("该把安装包交给系统安装流程一次: %v", *opened)
	}
	if result.Size != int64(len(deb)) {
		t.Fatalf("回给界面的大小和资产声明对不上: %+v", result)
	}
	raw, err := os.ReadFile(result.Path)
	if err != nil || string(raw) != string(deb) {
		t.Fatalf("落盘的包内容不对: %v", err)
	}
}

func TestDownloadUpdateRejectsTamperedPackage(t *testing.T) {
	priv, pub := testSigner(t)
	deb := []byte("FAKE-DEB-PAYLOAD-0.2.0")
	sum := sha256.Sum256(deb)
	hexSum := hex.EncodeToString(sum[:])
	sig := signDigest(t, priv, hexSum)
	tampered := append([]byte{}, deb...)
	tampered[len(tampered)-1] ^= 0xFF // 内容改一个字节，大小不变

	srv := serveUpdate(t, deb2map(t, tampered, sig, hexSum+"  github-net_0.2.0_amd64.deb\n", len(tampered)))
	defer srv.Close()

	svc := newUpdateService(t, srv.URL+"/releases/latest", pub)
	info, _ := svc.CheckForUpdates()
	_, err := svc.DownloadUpdate(info)
	if codeOf(t, err) != CodeUpdateVerifyFailed {
		t.Fatalf("被篡改的包该在校验关被拦（update_verify_failed），实际 %v", err)
	}
}

func TestDownloadUpdateRejectsWrongSigner(t *testing.T) {
	priv, pub := testSigner(t)
	other, _ := testSigner(t)
	deb := []byte("FAKE-DEB-PAYLOAD-0.2.0")
	sum := sha256.Sum256(deb)
	hexSum := hex.EncodeToString(sum[:])
	sig := signDigest(t, other, hexSum) // 别人的钥签的

	srv := serveUpdate(t, deb2map(t, deb, sig, hexSum+"  github-net_0.2.0_amd64.deb\n", len(deb)))
	defer srv.Close()

	svc := newUpdateService(t, srv.URL+"/releases/latest", pub)
	info, _ := svc.CheckForUpdates()
	_, err := svc.DownloadUpdate(info)
	if codeOf(t, err) != CodeUpdateVerifyFailed {
		t.Fatalf("换钥签的包该被拦，实际 %v", err)
	}
	_ = priv
}

func TestVerifySignatureRejectsMissingKey(t *testing.T) {
	if err := updater.VerifySignature(nil, "ab", "YQ=="); err == nil {
		t.Fatal("没有公钥该拒绝，不能退回不验签")
	}
}

// ===== 底座 =====

type assetFiles map[string][]byte

func deb2map(t *testing.T, deb []byte, sig string, checksums string, declared int) assetFiles {
	t.Helper()
	return assetFiles{
		"github-net_0.2.0_amd64.deb":     deb,
		"github-net_0.2.0_amd64.deb.sig": []byte(sig),
		"checksums.txt":                  []byte(checksums),
	}
}

// serveUpdate 起一个带「资产 + 声明大小」的假 GitHub：releases/latest 里的 size 用实际字节数。
func serveUpdate(t *testing.T, files assetFiles) *httptest.Server {
	t.Helper()
	deb := files["github-net_0.2.0_amd64.deb"]
	sig := string(files["github-net_0.2.0_amd64.deb.sig"])
	checksums := string(files["checksums.txt"])
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/releases/latest") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"tag_name":"v0.2.0","html_url":"https://github.com/x/releases/tag/v0.2.0",
				"assets":[
					{"name":"github-net_0.2.0_amd64.deb","browser_download_url":"%s/github-net_0.2.0_amd64.deb","size":%d},
					{"name":"github-net_0.2.0_amd64.deb.sig","browser_download_url":"%s/github-net_0.2.0_amd64.deb.sig","size":%d},
					{"name":"checksums.txt","browser_download_url":"%s/checksums.txt","size":%d}
				]}`, srv.URL, len(deb), srv.URL, len(sig), srv.URL, len(checksums))
			return
		}
		if body, ok := files[strings.TrimPrefix(r.URL.Path, "/")]; ok {
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	return srv
}

func srvDirWith(t *testing.T, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatalf("写测试资产: %v", err)
		}
	}
	return dir
}

// newUpdateService 建一个只有更新这一路的服务：假 GitHub 地址 + 测试公钥。
func newUpdateService(t *testing.T, baseURL string, pub ed25519.PublicKey) *Service {
	t.Helper()
	c := updater.NewClient()
	// 资产来源白名单跟着查询地址走：假应答里的 asset.URL 与 baseURL 同源（同一假服务器），
	// 生产白名单（https+GitHub 域）在测试环境必须换成假服务器的来源。
	c.SetAllowedAssetOrigins([]string{baseURL})
	c.SetBaseURL(baseURL)
	// 升级包落点在 config.json 旁边（应用数据目录内），没有配置服务就没有落点——
	// 测试服务也按生产形状配上（2026-11-13 审计把落点从 ~/.config/updates 收回应用目录内）。
	settings := config.NewStore(filepath.Join(t.TempDir(), "config", "config.json"), nil, nil, nil)
	return &Service{updater: c, pubKey: pub, settings: settings}
}

// withOpenSeam 把「请系统打开文件」换成记录函数：CI 容器里没有 xdg-open，
// 更不该在测试里真弹安装器；断言只看交接有没有发生、交给的是不是那个文件。
func withOpenSeam(t *testing.T, svc *Service) *[]string {
	t.Helper()
	opened := []string{}
	svc.openPath = func(path string) error {
		opened = append(opened, path)
		return nil
	}
	return &opened
}

// 下载目录按名保留（用户追问 2026-11-14）：重复点击不重复下载——同版本成品复用、
// 同版本 .part 留着续传，只清「这次用不上」的旧产物。
func TestDownloadUpdateKeepsCurrentVersionFiles(t *testing.T) {
	priv, pub := testSigner(t)
	svc, opened := func() (*Service, *[]string) {
		svc := newUpdateService(t, fakeUpdateServerFor(t, priv, pub).URL+"/releases/latest", pub)
		return svc, withOpenSeam(t, svc)
	}()
	updatesDir, err := svc.updatesDir()
	if err != nil {
		t.Fatalf("拿 updates 目录: %v", err)
	}
	// 预置：旧版本成品（该删）＋本版本残片（该留）。目录要现建：DownloadUpdate 才 MkdirAll。
	if err := os.MkdirAll(updatesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(updatesDir, "github-net_0.1.13_amd64.deb")
	if err := os.WriteFile(stale, []byte("OLD"), 0o600); err != nil {
		t.Fatal(err)
	}
	part := filepath.Join(updatesDir, ".github-net_0.2.0_amd64.deb.part")
	// 与真包前 4 字节一致，模拟真实中断残片。
	if err := os.WriteFile(part, []byte("FAKE"), 0o600); err != nil {
		t.Fatal(err)
	}

	info, err := svc.CheckForUpdates()
	if err != nil {
		t.Fatalf("检查更新: %v", err)
	}
	res, err := svc.DownloadUpdate(info)
	if err != nil {
		t.Fatalf("下载并交接: %v", err)
	}
	_ = opened
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("旧版本的包该被清掉")
	}
	// .part 在下载成功那一刻会被改名成成品（os.Rename），所以这里不该再有它——
	// 「保留 .part」要验证的是「按名清理不删它」：下载失败重试时它还在（Download
	// 的中断分支不动 .part，client_test 的续传用例钉住），只有按名清理把它误删
	// 才会让续传失效。这里退一步验证清理范围：keep 名单之外的才被删。
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Error("下载成功后 .part 该已被改名成成品，不该还在")
	}
	if _, err := os.Stat(res.Path); err != nil {
		t.Errorf("下载的成品该在: %v", err)
	}
}

// fakeUpdateServerFor 把 v0.2.0 假发布服务抽出来给两个用例共用。
func fakeUpdateServerFor(t *testing.T, priv ed25519.PrivateKey, pub ed25519.PublicKey) *httptest.Server {
	t.Helper()
	deb := []byte("FAKE-DEB-PAYLOAD-0.2.0")
	sum := sha256.Sum256(deb)
	hexSum := hex.EncodeToString(sum[:])
	sig := signDigest(t, priv, hexSum)
	checksums := hexSum + "  github-net_0.2.0_amd64.deb\n"

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/releases/latest") {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"tag_name":"v0.2.0","html_url":"https://github.com/x/releases/tag/v0.2.0",
				"assets":[
					{"name":"github-net_0.2.0_amd64.deb","browser_download_url":"%s/github-net_0.2.0_amd64.deb","size":%d},
					{"name":"github-net_0.2.0_amd64.deb.sig","browser_download_url":"%s/github-net_0.2.0_amd64.deb.sig","size":%d},
					{"name":"checksums.txt","browser_download_url":"%s/checksums.txt","size":%d}
				]}`, srv.URL, len(deb), srv.URL, len(sig), srv.URL, len(checksums))
			return
		}
		http.FileServer(http.Dir(srvDirWith(t, map[string][]byte{
			"github-net_0.2.0_amd64.deb":     deb,
			"github-net_0.2.0_amd64.deb.sig": []byte(sig),
			"checksums.txt":                  []byte(checksums),
		}))).ServeHTTP(w, r)
	}))
	return srv
}
