#!/usr/bin/env bash
# version.sh：打印当前应用版本。
# 版本单一真实来源现在是 main.go 的 appVersion 常量；等以后建了 wails.json，
# 再改成从它的 info.productVersion 读，两个地方不能同时算数。
set -euo pipefail
cd "$(dirname "$0")/.."
grep -oE 'appVersion\s*=\s*"[0-9][0-9.]+"' main.go | grep -oE '[0-9][0-9.]*' | head -n1
