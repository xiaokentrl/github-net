#!/usr/bin/env bash
# 产品身份取值的唯一入口，加上「删除/卸载之前必须过」的那道空值门。
#
# 为什么要有这个文件：`4475f5b` 之前，包名、二进制路径、配置目录名这些字符串在脚本里各抄一份；
# 从此它们只写在 internal/identity，脚本 eval 一次，而万一取不到值（没有 Go 工具链、
# 模块编不过、输出被截断），空字符串会被拼成 `rm -rf ~/.local/share/`、`dpkg --purge`（无包名）
# 这种灾难命令。所以这里把校验集中在删除和卸载那一刻，失败就中断，绝不带着空值往下走。
#
# 用法（调用方脚本）：
#   . "$ROOT/scripts/lib/identity-guard.sh"
#   identity_load "$ROOT" || { echo '拿不到产品身份，中断' >&2; exit 1; }   # 每个进程只 eval 这一次
#   identity_guard || exit 1                                               # 任何删除/卸载动作之前
#
# 之后一律读 IDENTITY_* 变量，不要再逐键 `--key` 调用（那是给 Taskfile 一类单值场合留的）。
#
# 例外：scripts/github-net-root-install.sh 不 source 这个文件。它会被免密 sudo 调用，
# 而仓库目录是用户可写的——让 root 去读或执行仓库里的东西，等于给用户开一个提权入口。
# 它自己保留字面量（包名只写在 `PKG="github-net"` 这一行，别处引用它），
# 由 internal/identity 的测试逐字核对——那是第二道金标准：改户口值要常量、金标准、这个 PKG 三处一起改。

# 需要的键（不带 IDENTITY_ 前缀）。少任何一个都当成取值失败。
IDENTITY_REQUIRED_KEYS="APP_ID SLUG DATA_DIR PACKAGE_NAME BINARY_NAME HELPER_PATH SUDOERS_FILE DEB_PATH DESKTOP_ID DESKTOP_FILE"

identity_load() {
  local root="${1:?用法：identity_load <仓库根目录>}"

  # 已经加载过就原样返回：一个进程里只 eval 一次。
  [ "${IDENTITY_LOADED:-0}" = "1" ] && identity_guard && return 0

  if [ ! -d "$root/cmd/identity" ]; then
    printf 'identity-guard：找不到 %s/cmd/identity\n' "$root" >&2
    return 1
  fi
  if ! command -v go >/dev/null 2>&1; then
    printf 'identity-guard：PATH 里没有 go，取不到产品身份\n' >&2
    return 1
  fi

  local out
  if ! out="$(cd "$root" && go run ./cmd/identity --shell 2>/dev/null)"; then
    printf 'identity-guard：go run ./cmd/identity 失败，取不到产品身份\n' >&2
    return 1
  fi
  # 只接受 IDENTITY_键='值' 这一种形状，别的（Go 的告警串进 stdout 也算）整行丢掉，
  # 再靠后面的必检键判断输出是否完整。值里不许有引号——我们的常量都没有，出现即视为异常。
  local re="^IDENTITY_[A-Z0-9_]+='[^\"']*'\$"
  local line
  while IFS= read -r line; do
    # shellcheck disable=SC2254  # 形状已经过上一行的正则收口
    [[ "$line" =~ $re ]] && eval "export $line"
  done <<<"$out"

  IDENTITY_LOADED=1
  identity_guard
}

# identity_guard：在任何 rm、dpkg --purge、apt、install、写 sudoers 之前调一次。
# 它只管一件事——手上这些值是不是「能用且不会扩大删除范围」。
identity_guard() {
  local name var value

  for name in $IDENTITY_REQUIRED_KEYS; do
    var="IDENTITY_$name"
    value="${!var:-}"
    if [ -z "$value" ]; then
      printf 'identity-guard：%s 是空的，拒绝继续（不要带着空值删东西或卸载包）\n' "$var" >&2
      return 1
    fi
  done

  # 单段名字：不许带路径分隔符、通配符、空格，也不许是 . 或 ..
  for name in APP_ID SLUG DATA_DIR PACKAGE_NAME BINARY_NAME DESKTOP_ID DESKTOP_FILE; do
    var="IDENTITY_$name"
    value="${!var:-}"
    case "$value" in
      . | .. | *[!A-Za-z0-9._-]*)
        printf 'identity-guard：%s 形状不合要求（只允许字母、数字、点、下划线、连字符，且不能带路径符）：%s\n' "$var" "$value" >&2
        return 1
        ;;
    esac
  done

  # 落盘绝对路径：各自必须在自己的那一层下，防止拼成 /usr、/etc 这种范围爆炸。
  # 字符集也一并收口——即便调用方全都加了引号，带空格或 $ 的路径也不该往下传。
  for name in HELPER_PATH SUDOERS_FILE DEB_PATH; do
    var="IDENTITY_$name"
    value="${!var:-}"
    case "$value" in
      *[!A-Za-z0-9._/-]*)
        printf 'identity-guard：IDENTITY_%s 含非法字符（只允许字母、数字、点、下划线、连字符和 /）：%s\n' "$name" "$value" >&2
        return 1
        ;;
    esac
  done

  _identity_under HELPER_PATH /usr/local/sbin/ || return 1
  _identity_under SUDOERS_FILE /etc/sudoers.d/ || return 1
  _identity_under DEB_PATH bin/ || return 1
  case "$IDENTITY_DEB_PATH" in
    *.deb) ;;
    *)
      printf 'identity-guard：IDENTITY_DEB_PATH 不以 .deb 结尾：%s\n' "$IDENTITY_DEB_PATH" >&2
      return 1
      ;;
  esac
  return 0
}

_identity_under() {
  local name="$1" prefix="$2"
  local var="IDENTITY_$name"
  local value="${!var:-}"
  case "$value" in
    "$prefix"?*) return 0 ;;
    *)
      printf 'identity-guard：IDENTITY_%s 必须落在 %s 之下：%s\n' "$name" "$prefix" "$value" >&2
      return 1
      ;;
  esac
}
