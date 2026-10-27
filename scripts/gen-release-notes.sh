#!/usr/bin/env bash
# gen-release-notes.sh：生成 GitHub Release 的正文 release-notes.md。
#
# 干什么：把「本版相对上一版改了什么」排成中英双语、分组固定的说明，直接当 Release 正文。
#        顺序是英文在前、中文在后；两种语言各自的小标题（英文句进 `## English`，中文句进 `## 中文`），
#        顶部不放任何中英混排的行。
# 为什么要脚本排：标题、分组名、顺序、编号、空行是固定契约，手写每次都会漂；只有条目内容是该轮现写的，
#                 所以脚本把可变的那部分从一份条目清单读进来，其余照模板吐。
#
# 标题契约：版本类型并进标题，不再有单独的「版本类型」一行。
#   第一次发版  `### github-net 0.1.0 Stable` / `### 0.1.0 稳定版`
#   之后发版    `### github-net 0.1.1 Stable (0.1.0 → 0.1.1)` / `### 0.1.1 稳定版（0.1.0 → 0.1.1）`
#   带 -beta/-rc 后缀的版本名判成预览版（Preview / 预览版），其余是稳定版。
#
# 分组契约（固定三组，中英各一份，顺序不变）：
#   新增 / Added      —— 新做出来的能力
#   修复 / Fixed      —— 原来不对、现在对了
#   更新 / Updated    —— 已有能力的变化；删除也写在这一组里，条目动词写清「删除了…」
# 每组都要出现：有内容就按 `1.`、`2.`、`3.` 顺序编号，一条一行；没有内容整组只写一个占位（中文「无」，英文 `None`）。
# 排版固定为：分组名一行（`**名字**`）→ 空一行 → 条目；组与组之间恰好一个空行；段末不留多余空行，文件以一个换行结束。
#
# 用法：bash scripts/gen-release-notes.sh <条目文件> [版本号]
#   · 版本号省略时取 main.go 的 appVersion（经 scripts/version.sh）；预览版手动传完整串（如 0.2.0-beta.1）
#   · 上一版本从本地标签里取「比本版小的最大那个」，发版前先 git fetch --tags
#   · 第一次发版（本地还没有任何 vX.Y.Z 标签）时没有「上一版」，标题不写 from/to
#   · 只写本版净变化，一条一行、动词开头、精简准确；不确定的不写
#
# 条目文件每行一条，# 开头是注释，格式「分类|语言|内容」：
#   added|zh|加一颗 AppImage 安装包
#   added|en|Add an AppImage package
#   updated|zh|删除了旧的演示状态按钮
# 分类只认 added / fixed / updated，语言只认 zh / en。条目正文只写文字，
# 不要自带 `-`、`1.` 这类前缀，编号由脚本统一加。
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

keys=(added fixed updated)
zh_names=(新增 修复 更新)
en_names=(Added Fixed Updated)
known() { local k; for k in "${keys[@]}"; do [ "$1" = "$k" ] && return 0; done; return 1; }

# 去掉条目正文首尾空白：脚本负责排版，条目文件里手滑多打空格不该渗进正文。
trim() { local s="$1"; s="${s#"${s%%[![:space:]]*}"}"; printf '%s' "${s%"${s##*[![:space:]]}"}"; }

# 先把整份清单认一遍再收：分类或语言写错、正文自带列表符号时宁可当场报错，也不要悄悄改形状或丢条目
while IFS='|' read -r c l t; do
  known "${c,,}" || { echo "条目文件里有不认识的分类：$c（只认 added / fixed / updated）" >&2; exit 2; }
  case "${l,,}" in zh|en) ;; *) echo "条目文件里有不认识的语言：$l（只认 zh / en）" >&2; exit 2 ;; esac
  t="$(trim "$t")"
  [ -n "$t" ] || { echo "条目文件里有空内容：$c|$l|" >&2; exit 2; }
  case "$t" in "- "*|"* "*) echo "条目正文要只留文字（编号和空行由脚本加）：$t" >&2; exit 2 ;; esac
done < <(grep -vE '^[[:space:]]*(#|$)' "$SRC")

for i in 0 1 2; do
  zh="" ; en=""
  while IFS='|' read -r c l t; do
    [ "${c,,}" = "${keys[$i]}" ] || continue
    t="$(trim "$t")"
    case "${l,,}" in
      zh) zh+="$t"$'\n' ;;
      en) en+="$t"$'\n' ;;
    esac
  done < <(grep -vE '^[[:space:]]*(#|$)' "$SRC")
  ZH[$i]="$zh"
  EN[$i]="$en"
  # 一类只写一种语言不行：中英两侧的分组必须同时有条目或同时为空
  if [ -n "$zh" ] && [ -z "$en" ]; then echo "分类 ${keys[$i]} 只有中文条目，没有对应的英文" >&2; exit 2; fi
  if [ -z "$zh" ] && [ -n "$en" ]; then echo "分类 ${keys[$i]} 只有英文条目，没有对应的中文" >&2; exit 2; fi
done

# 有条目按 1. 2. 3. 编号；没条目整组写一个占位（中文「无」/ 英文 `None`）。
numbered() { # $1=每行一条的正文 $2=空分组的占位
  local lines="$1" t out="" n=0
  if [ -z "$lines" ]; then printf '%s\n' "$2"; return 0; fi
  while IFS= read -r t; do
    [ -n "$t" ] || continue
    n=$((n + 1))
    out+="$n. $t"$'\n'
  done <<< "$lines"
  printf '%s' "$out"
}

# 一节一节拼：分组名前一个空行，组与组之间不攒多余空行；三组都要出现。
# 这里不用命令 substitution 取条目：那会把行尾换行吃掉，最后一组就会缺文件结尾的那个换行。
block() { # $1=分组名 $2=每行一条的正文 $3=空分组的占位
  printf '\n**%s**\n\n' "$1"
  numbered "$2" "$3"
}

{
  if [ -n "$prev" ]; then
    printf '## English\n\n### github-net %s %s (%s → %s)\n' "$ver" "$kind_en" "$prev" "$ver"
    for i in 0 1 2; do block "${en_names[$i]}" "${EN[$i]}" 'None'; done
    printf '\n## 中文\n\n### %s %s（%s → %s）\n' "$ver" "$kind_zh" "$prev" "$ver"
  else
    printf '## English\n\n### github-net %s %s\n' "$ver" "$kind_en"
    for i in 0 1 2; do block "${en_names[$i]}" "${EN[$i]}" 'None'; done
    printf '\n## 中文\n\n### %s %s\n' "$ver" "$kind_zh"
  fi
  for i in 0 1 2; do block "${zh_names[$i]}" "${ZH[$i]}" '无'; done
} > release-notes.md

echo "release-notes.md 已生成：$ver（$kind_zh）"
