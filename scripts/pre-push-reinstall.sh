#!/usr/bin/env bash
# pre-push 自动重装：打包 → 停旧进程 → 卸载旧包并删配置 → 装新包。
# 任何一步失败都中断推送，不带病上线。
# 跳过一次（比如只改了文档）：GITHUB_NET_SKIP_REINSTALL=1 git push
set -euo pipefail
cd "$(dirname "$0")/.."

echo '== 1/4 打包 deb =='
wails3 task linux:create:deb

# wails3 打出来的 deb 就叫 bin/github-net.deb（不带版本后缀，实测如此）
DEB="$(ls -t bin/github-net*.deb 2>/dev/null | head -n1 || true)"
[ -n "$DEB" ] || { echo '打完包却没找到 deb，中断推送' >&2; exit 1; }
echo "  产物：$DEB"

echo '== 2/4 停掉在跑的 github-net =='
# 装着的和 wails3 dev 起的都叫 github-net，一并停掉，避免旧实例占着旧代码
pkill -x github-net 2>/dev/null || true

echo '== 3/4 卸载旧包并删配置 =='
# purge 连 dpkg 登记的二进制、图标、桌面入口一起清；没装过就跳过
sudo dpkg --purge github-net 2>/dev/null || echo '  本来就没装，跳过卸载'
# 配置和数据按规划只落在这三个用户目录，删掉让新包从默认配置起步
rm -rf ~/.local/share/github-net ~/.cache/github-net ~/.config/github-net

echo '== 4/4 安装新包 =='
# 用 apt 装（而不是 dpkg -i）：依赖(libgtk-4-1、libwebkitgtk-6.0-4)缺了它会自己补
sudo apt-get install -y "./$DEB"

echo "== 完成：github-net 已重装，版本 $(dpkg-query -W -f='${Version}' github-net) =="
