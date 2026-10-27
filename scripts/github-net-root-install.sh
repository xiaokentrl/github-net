#!/usr/bin/env bash
# root 助手：只干两件事——卸载 github-net 旧包、安装仓库里打好的 deb。
# 会被 /etc/sudoers.d/github-net-reinstall 设成免密 sudo 调用，所以：
# 这个文件装到 /usr/local/sbin/ 后必须保持 root 所有、普通用户不可写；
# 也别给它加别的功能——免密之下，给它加什么能力等于给用户开什么 root 权限。
set -euo pipefail

USER_NAME="${SUDO_USER:-kentrl}"
DEB="/home/$USER_NAME/github-net/bin/github-net.deb"

[ -f "$DEB" ] || { echo "root 助手：找不到 $DEB，先在仓库里完成打包" >&2; exit 1; }

# purge 连二进制、图标、桌面入口一起清；没装过就跳过
dpkg --purge github-net 2>/dev/null || true
# 用 apt 装：依赖(libgtk-4-1、libwebkitgtk-6.0-4)缺了它会自己补
apt-get install -y "$DEB"

echo "root 助手完成：github-net $(dpkg-query -W -f='${Version}' github-net) 已安装"
