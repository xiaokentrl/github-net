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
	info, err := svc.CheckForUpdates()
	if err != nil {
		t.Fatalf("检查: %v", err)
	}
	result, err := svc.DownloadUpdate(info)
	if err != nil {
		t.Fatalf("下载并校验不该失败: %v", err)
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
	c.SetBaseURL(baseURL)
	return &Service{updater: c, pubKey: pub}
}
