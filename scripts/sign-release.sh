#!/usr/bin/env bash
# sign-release.sh —— 发布产物 Ed25519 签名
#
# 签名约定必须与 internal/updater/verify.go 逐字一致，否则应用内验签失败：
#   signature = base64(Ed25519(privateKey, 小写sha256Hex字符串的字节))   —— 被签消息是「hex 字符串」本身（64 ASCII 字节），不是文件字节
#   公钥嵌入格式 = base64(StdEncoding, 32 字节裸 Ed25519 公钥)，落 internal/updater/signing/public.key
#
# 私钥绝不入库。推荐位置 ~/.config/github-net-signing/release.key（本仓库 .gitignore 不需要挡它，
# 但任何情况下都不要把私钥拷进仓库或 CI 日志）。
#
# 子命令：
#   genkey   <私钥路径>                 生成 Ed25519 私钥（已存在则报错，防覆盖）
#   pubkey   <私钥路径> <公钥输出路径>   从私钥导出 base64 裸公钥（替换嵌入公钥后提交）
#   sign     [<产物...>]                对 bin/ 下的安装包（或显式参数）逐个出 <artifact>.sig
#
# 环境变量：
#   GITHUB_NET_SIGNING_KEY   私钥 PEM 路径（sign 时必填；genkey/pubkey 用位置参数）
#   BIN_DIR                  产物目录，默认 bin
set -euo pipefail

BIN_DIR="${BIN_DIR:-bin}"
ARTIFACT_RE='\.(deb|rpm|exe|dmg|AppImage)$'

sha256_of() {
  sha256sum "$1" | awk '{print $1}'
}

cmd="${1:-sign}"; shift || true
case "$cmd" in
  genkey)
    key="${1:?用法: sign-release.sh genkey <私钥路径>}"
    [ -e "$key" ] && { echo "私钥已存在，拒绝覆盖: $key" >&2; exit 1; }
    mkdir -p "$(dirname "$key")"
    openssl genpkey -algorithm ed25519 -out "$key"
    chmod 600 "$key"
    echo "✓ 已生成 Ed25519 私钥: $key（请妥善保管，切勿提交或进 CI 日志）"
    ;;
  pubkey)
    key="${1:?用法: sign-release.sh pubkey <私钥路径> <公钥输出路径>}"
    out="${2:?缺少公钥输出路径}"
    mkdir -p "$(dirname "$out")"
    # 私钥→SPKI DER→末尾 32 字节裸公钥→base64（与 verify.go / embed 解析口径一致）
    openssl pkey -in "$key" -pubout -outform DER 2>/dev/null | tail -c 32 | base64 -w0 > "$out"
    echo "✓ 已导出公钥: $out（提交到仓库，客户端用它验签）"
    ;;
  sign)
    key="${GITHUB_NET_SIGNING_KEY:?签名需设置环境变量 GITHUB_NET_SIGNING_KEY 指向私钥 PEM}"
    [ -f "$key" ] || { echo "私钥不存在: $key" >&2; exit 1; }
    files=()
    if [ "$#" -gt 0 ]; then
      files=("$@")
    else
      [ -d "$BIN_DIR" ] || { echo "目录不存在: $BIN_DIR" >&2; exit 1; }
      while IFS= read -r f; do
        printf '%s\n' "$(basename "$f")" | grep -Eq "$ARTIFACT_RE" && files+=("$f")
      done < <(find "$BIN_DIR" -maxdepth 1 -type f | sort)
    fi
    [ ${#files[@]} -gt 0 ] || { echo "没有可签的产物（匹配 $ARTIFACT_RE）" >&2; exit 1; }
    tmp="$(mktemp)"
    trap 'rm -f "$tmp"' EXIT
    for f in "${files[@]}"; do
      hex="$(sha256_of "$f")"
      printf '%s' "$hex" > "$tmp"                # 被签消息＝小写 hex 字符串，无尾换行
      openssl pkeyutl -sign -inkey "$key" -rawin -in "$tmp" | base64 -w0 > "$f.sig"
      echo "✓ $f  (sha256 $hex) → $f.sig"
    done
    ;;
  verify)
    # 本地自检：验一颗产物 + 它的 .sig 是否配对（发布前人工跑一遍）
    f="${1:?用法: sign-release.sh verify <产物路径>}"
    sig="$f.sig"
    key="${GITHUB_NET_SIGNING_KEY:?验证需设置 GITHUB_NET_SIGNING_KEY 指向私钥 PEM}"
    [ -f "$sig" ] || { echo "签名文件不存在: $sig" >&2; exit 1; }
    hex="$(sha256_of "$f")"
    tmp="$(mktemp)"
    trap 'rm -f "$tmp"' EXIT
    printf '%s' "$hex" > "$tmp"
    base64 -d "$sig" > "$tmp.sig"
    openssl pkeyutl -verify -inkey "$key" -rawin -in "$tmp" -sigfile "$tmp.sig" >/dev/null \
      && echo "✓ $f 验签通过" || { echo "✗ $f 验签失败" >&2; exit 1; }
    ;;
  *)
    echo "用法: sign-release.sh {genkey|pubkey|sign|verify} ..." >&2
    exit 2
    ;;
esac
