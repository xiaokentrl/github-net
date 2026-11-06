import type { KeyboardEvent as ReactKeyboardEvent } from 'react'

// 列表方向键导航：↑/↓ 在相邻行之间换焦点，到头就停住，不循环。
// 三处共用：左侧二级菜单、详情页全二级菜单表、候选 IP 表。
// 做成普通函数而不是 hook，是因为有的列表在数据算完、提前返回之后才拿得到行数。
// 事件挂在容器（tree / table）上、由行冒泡上来，所以 currentTarget 就是容器，
// 不必再传一份 ref 去找滚动范围。行统一带 data-arrow-index。
export function arrowKeys(count: number, index: number, onIndex: (next: number) => void) {
  return (event: ReactKeyboardEvent<HTMLElement>) => {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    const target = event.target as HTMLElement | null
    // 勾选框、输入框里的方向键是它们自己的操作，别抢。
    if (target && (target.isContentEditable || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT')) return
    if (count === 0) return
    event.preventDefault()
    const step = event.key === 'ArrowDown' ? 1 : -1
    // 还没有焦点行时：↓ 从第一行开始，↑ 从最后一行开始。
    const base = index < 0 ? (step > 0 ? -1 : count) : index
    const next = Math.min(count - 1, Math.max(0, base + step))
    onIndex(next)
    const container = event.currentTarget
    requestAnimationFrame(() => {
      const row = container.querySelector<HTMLElement>(`[data-arrow-index="${next}"]`)
      if (!row) return
      row.scrollIntoView({ block: 'nearest' })
      // 树行本身可聚焦，把键盘焦点一起带过去；表格行不可聚焦，容器留着焦点即可。
      if (row.tabIndex >= 0) row.focus()
    })
  }
}
