#!/usr/bin/env bash
# sudo -A 的取密码入口：弹一个图形密码框，把输入交给 sudo。
# 密码只在 sudo 认证那一刻用，不落文件、不进日志、不进仓库。
set -euo pipefail

command -v zenity >/dev/null || { echo '没装 zenity，弹不出密码框' >&2; exit 1; }
export DISPLAY="${DISPLAY:-:0}"

zenity --password --title='推送 github-net 需要提权' \
  --text='输入本机密码（卸载旧包、安装新包要用 sudo）'
