#!/usr/bin/env bash
# root 助手：只干两件事——卸载 github-net 旧包、安装仓库里打好的 deb。
# 会被 /etc/sudoers.d/github-net-reinstall 设成免密 sudo 调用，所以：
# 这个文件装到 /usr/local/sbin/ 后必须保持 root 所有、普通用户不可写；
# 也别给它加别的功能——免密之下，给它加什么能力等于给用户开什么 root 权限。
#
# 这里不 source 仓库里的 scripts/lib/identity-guard.sh，也不调用 go 取值：
# 免密 sudo 会在没有人工过目的情况下执行本文件，而仓库目录是用户可写的——
# 让 root 去读或执行仓库里的代码，等于给用户开一个提权入口。
# 因此包名按字面写在本文件里（不从参数传入），由 internal/identity 的测试核对一致性。
# 那是第二道金标准：改户口值要同时改 identity.go 的常量、identity_test.go 的金标准和本文件的 PKG 那一行，
# 只改其中一处测试就红。PKG 是本文件里唯一写包名的地方，别处一律引用它，避免同一个名字抄好几遍。
set -euo pipefail

PKG="github-net"
USER_NAME="${SUDO_USER:-kentrl}"
DEB="/home/$USER_NAME/$PKG/bin/$PKG.deb"

# 卸载和安装之前收口：用户名必须是正常单段名字，否则上面拼出来的路径会指向意外位置。
case "$USER_NAME" in
  . | .. | *[!A-Za-z0-9._-]*)
    echo "root 助手：调用者用户名不合法（$USER_NAME），拒绝执行" >&2
    exit 1
    ;;
esac
case "$DEB" in
  /home/*?/"$PKG"/bin/"$PKG".deb) ;;
  *)
    echo "root 助手：产物路径不合预期（$DEB），拒绝执行" >&2
    exit 1
    ;;
esac

[ -f "$DEB" ] || { echo "root 助手：找不到 $DEB，先在仓库里完成打包" >&2; exit 1; }

# purge 连二进制、图标、桌面入口一起清；没装过就跳过
dpkg --purge "$PKG" 2>/dev/null || true
# 用 apt 装：依赖(libgtk-4-1、libwebkitgtk-6.0-4)缺了它会自己补
apt-get install -y "$DEB"

echo "root 助手完成：$PKG $(dpkg-query -W -f='${Version}' "$PKG") 已安装"
