#!/usr/bin/env bash
# gen-release-notes.sh：生成 GitHub Release 的正文 release-notes.md。
#
# 干什么：把「本版相对上一版改了什么」排成中英双语、分类固定的说明，直接当 Release 正文。
#        顺序是英文在前、中文在后；两种语言各自的小标题（英文句进 `## English`，中文句进 `## 中文`）
#        和「版本类型」那一行都放在自己那一段里——英文段写 `**Version type: Stable｜Preview**`，
#        中文段写 `**版本类型：稳定版｜预览版**`，顶部不再放任何中英混排的行。
# 为什么要脚本排：标题、分类名、顺序、空行是固定契约，手写每次都会漂；只有条目内容是该轮现写的，
#                 所以脚本把可变的那部分从一份条目清单读进来，其余照模板吐。
#
# 用法：bash scripts/gen-release-notes.sh <条目文件> [版本号]
#   · 版本号省略时取 main.go 的 appVersion（经 scripts/version.sh）；预览版手动传完整串（如 0.2.0-beta.1）
#   · 上一版本从本地标签里取「比本版小的最大那个」，发版前先 git fetch --tags
#   · 第一次发版（本地还没有任何 vX.Y.Z 标签）时没有「上一版」，
#     标题退化成「github-net X.Y.Z What's Changed」和「X.Y.Z 更新内容」，不写 from/to
#   · 只写本版净变化，一条一行、动词开头；某类一条没有就整类不出现
#
# 条目文件每行一条，# 开头是注释，格式「分类|语言|内容」：
#   added|zh|加一颗 AppImage 安装包
#   added|en|Add an AppImage package
# 分类只认 added / changed / fixed / removed，语言只认 zh / en。
set -euo pipefail
cd "$(dirname "$0")/.."

SRC="${1:-}"
[ -n "$SRC" ] && [ -f "$SRC" ] || { echo "用法：bash scripts/gen-release-notes.sh <条目文件> [版本号]" >&2; exit 2; }

ver="${2:-$(bash scripts/version.sh)}"
[ -n "$ver" ] || { echo "取不到版本号（main.go 的 appVersion 是空的）" >&2; exit 1; }
[[ "$ver" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || { echo "版本号形状不认识：$ver" >&2; exit 2; }

# 上一版本：把本地所有 vX.Y.Z（带 -beta.N 也算，先剥后缀）与本版一起按版本序排，本版前一条就是上一版。
# 找不到更早标签时不报错，按「第一次发版」处理，标题里不写 from/to。
base="${ver%%-*}"
prev="$( { git tag --list 'v[0-9]*' --format='%(refname:short)' | sed 's/^v//; s/-.*//'
           grep -E '^[0-9]+\.[0-9]+\.[0-9]+$'
           printf '%s\n' "$base"; } | sort -uV \
        | awk -v cur="$base" '{ if ($0 == cur) { print prev; exit } prev = $0 }' )"

if [[ "$ver" == *-* ]]; then
  kind_zh='预览版'; kind_en='Preview'
else
  kind_zh='稳定版'; kind_en='Stable'
fi

keys=(added changed fixed removed)
zh_names=(新增 修改 修复 删除)
en_names=(Added Changed Fixed Removed)
known() { local k; for k in "${keys[@]}"; do [ "$1" = "$k" ] && return 0; done; return 1; }

# 先把整份清单认一遍再收：分类或语言写错时宁可当场报错，也不要把一条改动静默丢掉
while IFS='|' read -r c l t; do
  known "${c,,}" || { echo "条目文件里有不认识的分类：$c（只认 added / changed / fixed / removed）" >&2; exit 2; }
  case "${l,,}" in zh|en) ;; *) echo "条目文件里有不认识的语言：$l（只认 zh / en）" >&2; exit 2 ;; esac
  [ -n "$t" ] || { echo "条目文件里有空内容：$c|$l|" >&2; exit 2; }
done < <(grep -vE '^[[:space:]]*(#|$)' "$SRC")

for i in 0 1 2 3; do
  zh="" ; en=""
  while IFS='|' read -r c l t; do
    [ "${c,,}" = "${keys[$i]}" ] || continue
    case "${l,,}" in
      zh) zh+="- $t"$'\n' ;;
      en) en+="- $t"$'\n' ;;
    esac
  done < <(grep -vE '^[[:space:]]*(#|$)' "$SRC")
  ZH[$i]="$zh"
  EN[$i]="$en"
  # 一类只写一种语言不行：中英两侧的分类必须同时出现或同时缺席
  if [ -n "$zh" ] && [ -z "$en" ]; then echo "分类 ${keys[$i]} 只有中文条目，没有对应的英文" >&2; exit 2; fi
  if [ -z "$zh" ] && [ -n "$en" ]; then echo "分类 ${keys[$i]} 只有英文条目，没有对应的中文" >&2; exit 2; fi
done

# 一节一节拼：每个分类前一个空行，分类之间不攒额外空行；空类整节不出现
block() { # $1=分类名 $2=条目（每行自带换行）
  [ -n "$2" ] || return 0
  printf '\n**%s**\n\n%s' "$1" "$2"
}

{
  if [ -n "$prev" ]; then
    printf '## English\n\n### github-net %s What'"'"'s Changed from %s to %s\n\n**Version type: %s**\n' "$ver" "$prev" "$ver" "$kind_en"
    for i in 0 1 2 3; do block "${en_names[$i]}" "${EN[$i]}"; done
    printf '\n## 中文\n\n### %s 相比 %s 更新内容\n\n**版本类型：%s**\n' "$ver" "$prev" "$kind_zh"
  else
    printf '## English\n\n### github-net %s What'"'"'s Changed\n\n**Version type: %s**\n' "$ver" "$kind_en"
    for i in 0 1 2 3; do block "${en_names[$i]}" "${EN[$i]}"; done
    printf '\n## 中文\n\n### %s 更新内容\n\n**版本类型：%s**\n' "$ver" "$kind_zh"
  fi
  for i in 0 1 2 3; do block "${zh_names[$i]}" "${ZH[$i]}"; done
} > release-notes.md

echo "release-notes.md 已生成：$ver（$kind_zh）"
