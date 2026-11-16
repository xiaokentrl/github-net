import { useSyncExternalStore } from 'react'

// Toast 提示的唯一数据源（2026-11-15 重写，照中央指南库 Toast/NotificationRouter 模式）：
// 任何模块都能递一条进来，界面上只有一份。做法和队列 store（features/tasks/taskStore.ts）
// 一致——模块级不可变快照 + useSyncExternalStore，不引新的状态库。
// 与旧「提示面板」的区别（用户拍板重写）：每条到点自动消失（Toast 语义），不再有
// 常驻面板/胶囊/清空/收起/拖拽；重要的结果在树上和设置里有持久状态，Toast 只负责
// 「此刻让你看见」，消失了也不丢事实。

export type NoticeKind = 'error' | 'warning' | 'info' | 'success'

export type Notice = {
  id: number
  kind: NoticeKind
  text: string
  /** 到点自动消失的时刻（毫秒）。 */
  expiresAt: number
}

/** 一屏最多留几条：超了丢最老的，不让一次批量失败把界面刷成日志。 */
const MAX_NOTICES = 5

/** 每档活多久（毫秒）：error/warning 给足阅读与行动时间，info/success 克制。
 *  写入结果本身在树上有持久状态（已替换/未替换），Toast 消失不丢事实。 */
const LIFESPAN: Record<NoticeKind, number> = {
  error: 8000,
  warning: 6000,
  info: 4000,
  success: 4000,
}

let notices: Notice[] = []
let nextId = 1
let timer: ReturnType<typeof setTimeout> | null = null

const listeners = new Set<() => void>()

function publish(): void {
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function getNotices(): Notice[] {
  return notices
}

/** Toast 堆栈读口：组件经 useSyncExternalStore 拿快照，不各自存副本。 */
export function useNotices(): Notice[] {
  return useSyncExternalStore(subscribe, getNotices)
}

function scheduleNext(): void {
  if (timer !== null) clearTimeout(timer)
  let soonest = Infinity
  for (const notice of notices) {
    if (notice.expiresAt < soonest) soonest = notice.expiresAt
  }
  if (soonest === Infinity) return
  timer = setTimeout(tick, Math.max(0, soonest - Date.now()))
}

function tick(): void {
  const now = Date.now()
  const kept = notices.filter((notice) => notice.expiresAt > now)
  if (kept.length !== notices.length) {
    notices = kept
    publish()
  }
  scheduleNext()
}

/** 递一条 Toast，返回它的 ID。
 *  同样的文字已在屏上就只续时间不叠第二条：连点五次「补充可用IP」被拒五次，
 *  该看到的是一条还在那儿的，而不是五条一模一样。 */
export function pushNotice(kind: NoticeKind, text: string): number {
  const trimmed = text.trim()
  if (trimmed === '') return 0
  const expiresAt = Date.now() + LIFESPAN[kind]
  const existing = notices.find((notice) => notice.text === trimmed && notice.kind === kind)
  if (existing) {
    notices = notices.map((notice) => (notice.id === existing.id ? { ...notice, expiresAt } : notice))
    publish()
    scheduleNext()
    return existing.id
  }
  const notice: Notice = { id: nextId, kind, text: trimmed, expiresAt }
  nextId += 1
  notices = [...notices, notice]
  if (notices.length > MAX_NOTICES) notices = notices.slice(notices.length - MAX_NOTICES)
  publish()
  scheduleNext()
  return notice.id
}
