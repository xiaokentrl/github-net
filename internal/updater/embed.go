package updater

import (
	"crypto/ed25519"
	"embed"
	"encoding/base64"
	"strings"
)

// 发布公钥随包分发（go:embed），用于校验升级包的 Ed25519 签名。
//
// 配钥流程（一次性，离线，scripts/sign-release.sh）：
//  1. genkey 生成私钥，长期保管在持钥者处（本仓库不入库）；
//  2. pubkey 导出 base64 裸公钥写入 internal/updater/signing/public.key 并提交；
//  3. 之后每次发布用私钥对产物出 .sig，客户端按 verify.go 的契约验签。
//
// 换钥等于让所有已分发客户端永久无法验签——除非连新公钥一起发新版。私钥务必长期保管。
//
//go:embed signing
var signingFS embed.FS

// PublicKey 解析嵌入的 Ed25519 公钥；缺失或长度不符返回 nil，
// VerifySignature 据此拒绝一切升级——绝不退回「不验签」。
func PublicKey() ed25519.PublicKey {
	b, err := signingFS.ReadFile("signing/public.key")
	if err != nil {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(raw)
}
