#!/usr/bin/env bash
# 一次性配置「推送前自动重装」的免密 sudo。输这一次密码，以后 git push 永不再问。
# 用法：sudo bash scripts/setup-passwordless-reinstall.sh
#
# 做三件事：
#   1. 把 root 助手脚本装成 /usr/local/sbin/<产品>-root-install（root 所有，用户不可写）
#   2. 写 sudoers 免密规则 /etc/sudoers.d/<产品>-reinstall，只对这一个文件免密
#   3. visudo 校验通过才落地，规则坏了当场撤销，不会把 sudo 搞挂
#
# 这两个落盘路径不写死在这里，取自 internal/identity；取不到或形状不对就当场中断，
# 免得把免密规则挂到一个空路径或意外路径上。
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"

USER_NAME="${SUDO_USER:?请用 sudo 运行：sudo bash scripts/setup-passwordless-reinstall.sh}"

. "$ROOT/scripts/lib/identity-guard.sh"
# sudo 会换成受限的 PATH，Go 工具链常装在这之外；取不到就如实报错，不退回写死的字面量。
identity_load "$ROOT" || {
  echo '拿不到产品身份，中断配置。若是「PATH 里没有 go」，用带上 go 的 PATH 重试，例如：' >&2
  echo '  sudo env PATH="$PATH" bash scripts/setup-passwordless-reinstall.sh' >&2
  exit 1
}
identity_guard || { echo '产品身份的值不合法，中断配置' >&2; exit 1; }

HELPER_SRC="scripts/$IDENTITY_SLUG-root-install.sh"
[ -f "$HELPER_SRC" ] || { echo "找不到助手源文件：$HELPER_SRC" >&2; exit 1; }

install -o root -g root -m 755 "$HELPER_SRC" "$IDENTITY_HELPER_PATH"

RULE="$USER_NAME ALL=(root) NOPASSWD: $IDENTITY_HELPER_PATH"
TMP="$(mktemp)"
printf '%s\n' "$RULE" > "$TMP"
visudo -cf "$TMP" >/dev/null
install -o root -g root -m 440 "$TMP" "$IDENTITY_SUDOERS_FILE"
rm -f "$TMP"

echo "配置完成：$USER_NAME 执行 sudo $IDENTITY_HELPER_PATH 不再要密码。"
echo "撤销方法：sudo rm $IDENTITY_SUDOERS_FILE $IDENTITY_HELPER_PATH"
