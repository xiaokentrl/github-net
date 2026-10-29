#!/usr/bin/env bash
# verify-signing-guard.sh —— 发布前的签名钥守卫：确认「要拿来签名的私钥」和「嵌进客户端的公钥」是同一对。
#
# 为什么必须有这一步：私钥换了而公钥没换（或反过来），签出来的包所有已分发客户端都验不过，
# 用户拿到的就是一次永远装不上的更新。在签名之前反推公钥比对，不匹配当场拒发布。
# 与 phpo 的 verify-signing-guard.sh 同一思路；github-net 的公钥在 internal/updater/signing/public.key。
#
# 用法：bash scripts/verify-signing-guard.sh <私钥 PEM 路径> <嵌入公钥路径>
set -euo pipefail

KEY="${1:?用法: verify-signing-guard.sh <私钥PEM> <嵌入公钥文件>}"
PUBFILE="${2:?缺少嵌入公钥文件路径}"
[ -f "$KEY" ] || { echo "私钥不存在: $KEY" >&2; exit 1; }
[ -f "$PUBFILE" ] || { echo "嵌入公钥文件不存在: $PUBFILE" >&2; exit 1; }

derived="$(openssl pkey -in "$KEY" -pubout -outform DER 2>/dev/null | tail -c 32 | base64 -w0)"
embedded="$(tr -d '[:space:]' < "$PUBFILE")"

if [ -z "$derived" ]; then
  echo "守卫失败：从私钥反推公钥失败" >&2
  exit 1
fi
if [ "$derived" != "$embedded" ]; then
  echo "守卫失败：私钥反推出的公钥和嵌入客户端的公钥不是一对" >&2
  echo "  反推: $derived" >&2
  echo "  嵌入: $embedded" >&2
  echo "先换钥或先换嵌入文件，两边对齐再发布，否则所有客户端都验不了这次的包。" >&2
  exit 1
fi
echo "✓ 签名钥守卫通过：私钥与嵌入公钥配对"
