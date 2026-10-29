package updater

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
)

// 升级包完整性双校验：SHA256 摘要比对 + Ed25519 验签，缺一不可。
// 签名契约（scripts/sign-release.sh 同一份话，改一边必须改另一边）：
//
//	signature = base64(Ed25519(privateKey, 小写 sha256Hex 字符串的字节))
//	被签消息是「hex 字符串」本身（64 个 ASCII 字节），不是文件字节。
var (
	ErrChecksum    = errors.New("升级包 SHA256 校验失败")
	ErrSignature   = errors.New("升级包签名校验失败")
	ErrNoPublicKey = errors.New("应用里没有可用的验签公钥")
	ErrKeyLength   = errors.New("Ed25519 公钥长度不对")
)

// SHA256File 算文件的小写 hex 摘要。
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifyChecksum 比对实际与期望 SHA256（大小写无关；期望为空一律拒绝）。
func VerifyChecksum(gotHex, wantHex string) error {
	if wantHex == "" || !strings.EqualFold(gotHex, wantHex) {
		return ErrChecksum
	}
	return nil
}

// VerifySignature 用公钥验「对 sha256Hex 签的」Ed25519 签名（base64 编码）。
// pub 为 nil 表示应用里没有公钥：直接拒绝，不降级成只看摘要。
func VerifySignature(pub ed25519.PublicKey, sha256Hex, signatureB64 string) error {
	if pub == nil {
		return ErrNoPublicKey
	}
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return ErrSignature
	}
	if !ed25519.Verify(pub, []byte(strings.ToLower(sha256Hex)), sig) {
		return ErrSignature
	}
	return nil
}
