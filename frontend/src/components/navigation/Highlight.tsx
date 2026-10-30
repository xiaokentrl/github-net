import type { ReactNode } from 'react'
import styles from './CategoryTree.module.css'

// 搜索命中的那一段套一层底色，字还是原来的字（原型 renderTree 第 2318–2322 行那个 hl）。
// 命中可能落在名字的任何一个位置，所以整段按小写逐次找，找到就切一段。
function split(text: string, needle: string): ReactNode[] {
  const out: ReactNode[] = []
  const lower = text.toLowerCase()
  let from = 0
  let at = lower.indexOf(needle, from)
  let key = 0
  while (at !== -1) {
    if (at > from) out.push(text.slice(from, at))
    out.push(
      <span key={key++} className={styles.hl}>
        {text.slice(at, at + needle.length)}
      </span>,
    )
    from = at + needle.length
    at = lower.indexOf(needle, from)
  }
  if (from < text.length) out.push(text.slice(from))
  return out
}

export function Highlight({ text, needle }: { text: string; needle: string }) {
  if (needle === '') return <>{text}</>
  const parts = split(text, needle)
  // 没命中就原样返回，不套空标签。
  if (!parts.some((part) => typeof part === 'object')) return <>{text}</>
  return <>{parts}</>
}
