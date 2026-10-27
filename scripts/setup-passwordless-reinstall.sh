#!/usr/bin/env bash
# 一次性配置「推送前自动重装」的免密 sudo。输这一次密码，以后 git push 永不再问。
# 用法：sudo bash scripts/setup-passwordless-reinstall.sh
#
# 做三件事：
#   1. 把 scripts/github-net-root-install.sh 装成 /usr/local/sbin/github-net-root-install（root 所有，用户不可写）
#   2. 写 sudoers 免密规则 /etc/sudoers.d/github-net-reinstall，只对这一个文件免密
#   3. visudo 校验通过才落地，规则坏了当场撤销，不会把 sudo 搞挂
set -euo pipefail
cd "$(dirname "$0")/.."

USER_NAME="${SUDO_USER:?请用 sudo 运行：sudo bash scripts/setup-passwordless-reinstall.sh}"

install -o root -g root -m 755 scripts/github-net-root-install.sh /usr/local/sbin/github-net-root-install

RULE="$USER_NAME ALL=(root) NOPASSWD: /usr/local/sbin/github-net-root-install"
TMP="$(mktemp)"
printf '%s\n' "$RULE" > "$TMP"
visudo -cf "$TMP" >/dev/null
install -o root -g root -m 440 "$TMP" /etc/sudoers.d/github-net-reinstall
rm -f "$TMP"

echo "配置完成：$USER_NAME 执行 sudo /usr/local/sbin/github-net-root-install 不再要密码。"
echo "撤销方法：sudo rm /etc/sudoers.d/github-net-reinstall /usr/local/sbin/github-net-root-install"
