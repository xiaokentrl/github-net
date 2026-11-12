#!/usr/bin/env bash
# pre-push 自动重装：验树干净 → 打包 → 停旧进程 → 卸载旧包并装新包 → 保留配置 → 装后三验 → 拉回。
# 任何一步失败都中断推送，不带病上线。
# 跳过一次（比如只改了文档）：GITHUB_NET_SKIP_REINSTALL=1 git push
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"

# 推送前验树干净（对照复核 2026-11-13 高危）：deb 从工作树构建——脏树的改动会被装上
# 本机却不在推送内容里，「装过能跑」就成了假证据。要么先提交，要么显式跳过钩子。
if [ -n "$(git status --porcelain)" ]; then
  echo '工作区有未提交的改动：先提交（或 GITHUB_NET_SKIP_REINSTALL=1 显式跳过重装）再推' >&2
  git status --short >&2
  exit 1
fi

# 产品身份：包名、二进制名、产物路径、助手路径、配置目录名统统来自 internal/identity，
# 这里只 eval 这一次；取不到值就中断推送，绝不带着空字符串往下删。
. "$ROOT/scripts/lib/identity-guard.sh"
identity_load "$ROOT" || { echo '拿不到产品身份，中断推送' >&2; exit 1; }
identity_guard || { echo '产品身份的值不合法，中断推送' >&2; exit 1; }

# 装好的 root 助手：只干卸载+安装这两件事，可由 setup-passwordless-reinstall.sh 配成免密。
HELPER="$IDENTITY_HELPER_PATH"
PRIV=()

# 提权通道，按「已经免密 → 终端能问 → 图形弹框」依次挑一条。
# 三条都是真实场景：配了免密规则、人在终端里推送、以及在 git 钩子里推送（没有终端）。
setup_priv() {
  [ ${#PRIV[@]} -gt 0 ] && return 0
  if sudo -n true 2>/dev/null; then
    PRIV=(sudo -n)
    return 0
  fi
  local out
  if out=$(sudo -v 2>&1); then
    PRIV=(sudo)
    return 0
  fi
  # 只有「读不到密码」这一种失败才换图形弹框；其余（密码错、被拒）直接中断，不反复折腾。
  case "$out" in
    *terminal* | *TTY* | *tty*) ;;
    *) printf 'sudo 授权失败：%s\n' "$out" >&2; return 1 ;;
  esac
  export SUDO_ASKPASS="$ROOT/scripts/gui-askpass.sh"
  if sudo -A -v 2>/dev/null; then
    PRIV=(sudo -A)
    echo '  已用图形密码框完成授权'
    return 0
  fi
  echo '图形密码框没通过授权（或被取消），中断推送' >&2
  return 1
}

priv() {
  setup_priv || return 1
  "${PRIV[@]}" "$@"
}

# 卸载旧包并装新包：有 root 助手就整件事交给它（可能免密，不再问密码）；
# 没有助手才在这里逐条执行 dpkg/apt。
swap_packages() {
  local deb="$1"
  # 助手只认固定产物路径，别的文件名不能让它装（否则会装到旧包）。
  if [ -x "$HELPER" ] && [ "$deb" = "$IDENTITY_DEB_PATH" ]; then
    echo "  走 root 助手：$HELPER"
    if sudo -n "$HELPER"; then
      return 0
    fi
    echo '  助手还没配免密，改为问一次密码'
    priv "$HELPER"
    return
  fi
  if [ -x "$HELPER" ]; then
    echo "  这次的产物不是 $IDENTITY_DEB_PATH，助手认不出，改走脚本直装" >&2
  fi
  identity_guard || { echo '卸载前产品身份校验没过，中断' >&2; return 1; }
  if dpkg-query -W -f='${Status}' "$IDENTITY_PACKAGE_NAME" 2>/dev/null | grep -q 'install ok installed'; then
    priv dpkg --purge "$IDENTITY_PACKAGE_NAME"
  else
    echo '  本来就没装，跳过卸载'
  fi
  # 用 apt 装（而不是 dpkg -i）：依赖(libgtk-4-1、libwebkitgtk-6.0-4)缺了它会自己补
  priv apt-get install -y "./$deb"
}

echo '== 1/4 打包 deb =='
wails3 task linux:create:deb

# wails3 打出来的 deb 就是 identity 里登记的产物路径（不带版本后缀，实测如此）。
# 找的时候按前缀通配，兼容万一带上版本号的产物名。
DEB="$(ls -t "${IDENTITY_DEB_PATH%.deb}"*.deb 2>/dev/null | head -n1 || true)"
[ -n "$DEB" ] || { echo '打完包却没找到 deb，中断推送' >&2; exit 1; }
echo "  产物：$DEB ($(stat -c %s "$DEB") 字节)"

# 记住推送前应用在不在跑：装完要把它拉回来（关停与拉回对称，对照复核 2026-11-13）。
WAS_RUNNING=0
pgrep -x "$IDENTITY_BINARY_NAME" >/dev/null 2>&1 && WAS_RUNNING=1

echo "== 2/4 停掉在跑的 $IDENTITY_BINARY_NAME =="
# 装着的和 wails3 dev 起的都叫同一个二进制名，一并停掉，避免旧实例占着旧代码
pkill -x "$IDENTITY_BINARY_NAME" 2>/dev/null || true

echo '== 3/4 卸载旧包并装新包 =='
swap_packages "$DEB"

echo '== 4/4 保留配置 =='
# 配置不再删除（拍板 2026-11-03）：重装后沿用 ~/.config/github-net 里已有的设置，
# 模拟的是「升级」而不是「新用户起步」。要体验全新配置就手动删 ~/.config/github-net。

# 装后三验（对照复核 2026-11-13）：包状态装好、可执行文件在位、装的版本 == 源里声明
# 的版本——「装完」要拿证据说话，不只看安装命令退出码。
INSTALLED_VERSION="$(dpkg-query -W -f='${Version}' "$IDENTITY_PACKAGE_NAME")"
dpkg-query -W -f='${Status}' "$IDENTITY_PACKAGE_NAME" | grep -q 'install ok installed' \
  || { echo '装后验包状态失败：dpkg 不认为已安装' >&2; exit 1; }
[ -x "$IDENTITY_BINARY_PATH" ] || { echo "装后验可执行文件失败：$IDENTITY_BINARY_PATH 不在或不可执行" >&2; exit 1; }
SOURCE_VERSION="$(bash "$ROOT/scripts/version.sh")"
if [ "$INSTALLED_VERSION" != "${SOURCE_VERSION}-1" ]; then
  echo "装后验版本失败：装的是 $INSTALLED_VERSION，源里是 ${SOURCE_VERSION}-1" >&2
  exit 1
fi

# 推送前在跑就拉回来：推送不该把用户的应用顺手关掉。
if [ "$WAS_RUNNING" = "1" ]; then
  echo '== 拉回应用 =='
  setsid nohup "$IDENTITY_BINARY_PATH" >/dev/null 2>&1 < /dev/null &
fi

echo "== 完成：$IDENTITY_PACKAGE_NAME 已重装，版本 $INSTALLED_VERSION（装后三验过），配置已保留 =="
