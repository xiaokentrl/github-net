#!/usr/bin/env bash
# 一次性配置「推送前自动重装」的免密 sudo。输这一次密码，以后 git push 永不再问。
# 用法：bash scripts/setup-passwordless-reinstall.sh   （普通用户运行；脚本自己向 sudo 要一次密码）
#
# 做三件事：
#   1. 把 root 助手脚本装成 /opt/github-net/libexec/<产品>-root-install（root 所有，用户不可写）
#   2. 写 sudoers 免密规则 /etc/sudoers.d/<产品>-reinstall（%sudo 组），只对这一个文件免密
#   3. 清掉旧布局的孤儿助手（/usr/local/sbin/<产品>-root-install）
#
# 两段式设计（2026-11-15 修）：身份取值必须放在「调用用户」的环境里——sudo 会把
# PATH/HOME 换成受限环境，go 与构建缓存全不在那边，`sudo bash 本脚本` 必挂在
# identity 取值上（两次真机失败实证）。所以：第一段以普通用户跑（go 取身份＋全部校验），
# 第二段 `sudo -E` 原样保留身份变量、只做三个特权动作，不在 root 环境里碰 go。
#
# 落盘路径不写死在这里，取自 internal/identity；取不到或形状不对就当场中断，
# 免得把免密规则挂到一个空路径或意外路径上。
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"

if [ "${1:-}" = "--root-phase" ]; then
  # ===== 第二段（root）：身份值由第一段以位置参数显式传入（sudo -E 会被 SETENV 拒） =====
  HELPER_PATH="${2:-}"; SUDOERS_FILE="${3:-}"; SLUG="${4:-}"
  # 形状硬校验：两个落点必须长在预期前缀下，坏值当场中断（此处已是 root，坏路径=事故）。
  case "$HELPER_PATH" in
    /opt/github-net/libexec/github-net-root-install) ;;
    *) echo "helper 路径形状不对：$HELPER_PATH" >&2; exit 1 ;;
  esac
  case "$SUDOERS_FILE" in
    /etc/sudoers.d/github-net-reinstall) ;;
    *) echo "sudoers 路径形状不对：$SUDOERS_FILE" >&2; exit 1 ;;
  esac
  [ "$SLUG" = github-net ] || { echo "slug 不对：$SLUG" >&2; exit 1; }

  HELPER_SRC="scripts/$SLUG-root-install.sh"
  [ -f "$HELPER_SRC" ] || { echo "找不到助手源文件：$ROOT/$HELPER_SRC" >&2; exit 1; }

  install -D -o root -g root -m 755 "$ROOT/$HELPER_SRC" "$HELPER_PATH"

  # 规则按组收口（定稿 v2）：sudo 组成员都能免密调这个 root 所有、只认固定 deb 路径的助手。
  RULE="%sudo ALL=(root) NOPASSWD: $HELPER_PATH"
  TMP="$(mktemp)"
  printf '%s\n' "$RULE" > "$TMP"
  visudo -cf "$TMP" >/dev/null
  install -D -o root -g root -m 440 "$TMP" "$SUDOERS_FILE"
  rm -f "$TMP"

  # 旧布局的孤儿助手清掉（新位置已在 /opt/github-net/libexec）。
  rm -f /usr/local/sbin/github-net-root-install

  echo "配置完成：sudo $HELPER_PATH 不再要密码（%sudo 组）。"
  echo "撤销方法：sudo rm $SUDOERS_FILE $HELPER_PATH"
  exit 0
fi

# ===== 第一段（调用用户）：go 取身份＋全部校验，然后向 sudo 移交 =====
if [ "$(id -u)" = 0 ]; then
  echo '请以普通用户运行本脚本（不要加 sudo 前缀；脚本自己会向 sudo 要一次密码）。' >&2
  exit 1
fi

. "$ROOT/scripts/lib/identity-guard.sh"
identity_load "$ROOT" || { echo '拿不到产品身份（go 不在当前 PATH？本脚本须在能跑 go 的普通用户环境运行）' >&2; exit 1; }
identity_guard || { echo '产品身份的值不合法，中断配置' >&2; exit 1; }

echo '接下来 sudo 会要一次密码（这是本脚本唯一一次要密码）。'
# 身份值以位置参数显式传递（sudo -E 的环境保留不可依赖），root 段对形状做硬校验。
exec sudo bash "$ROOT/scripts/setup-passwordless-reinstall.sh" --root-phase \
  "$IDENTITY_HELPER_PATH" "$IDENTITY_SUDOERS_FILE" "$IDENTITY_SLUG"
