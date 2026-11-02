import { useSyncExternalStore } from 'react'

// 右下角提示面板的唯一数据源：任何模块都能递一条进来，界面上只有一份。
// 做法和队列 store（features/tasks/taskStore.ts）一致——模块级不可变快照 + useSyncExternalStore，
// 不引新的状态库；订阅挂在应用级，切页面、收起面板都不丢已经递进来的提示。
// 倒计时也住在这里：组件卸载重挂（StrictMode）不会让一条提示提前消失。

export type NoticeKind = 'error' | 'warning' | 'info' | 'success'

export type Notice = {
  id: number
  kind: NoticeKind
  text: string
  /** 到点自己消失的时刻（毫秒）；null＝不自己消失，或此刻正收起、时间冻在 remainingMs 里。 */
  expiresAt: number | null
  /** 收起期间冻结下来的剩余毫秒；没冻结时为 null。收起＝暂停，展开接着数，不重新给一整轮。 */
  remainingMs: number | null
}

/** 一屏最多留几条：超了丢最老的，不让一次批量失败把界面刷成日志。 */
const MAX_NOTICES = 50

/** 每档活多久（毫秒），0＝不自动消失。
 *  错误和警告要留着——看一眼就没了等于没说；提醒和成功是回执，读完就该走。 */
const LIFESPAN: Record<NoticeKind, number> = {
  error: 0,
  warning: 20000,
  info: 8000,
  success: 6000,
}

let notices: Notice[] = []
let collapsed = false
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

function getCollapsed(): boolean {
  return collapsed
}

/** 面板和队列共用 useSyncExternalStore，组件读快照，不各自存副本。 */
export function useNotices(): Notice[] {
  return useSyncExternalStore(subscribe, getNotices)
}

/** 收起状态也在 store 里，而不只是组件里：收起时用户看不见这些提示，
 *  就不该让倒计时偷偷把它们清掉，这件事只有数据源自己知道才靠得住。 */
export function useNoticeCollapsed(): boolean {
  return useSyncExternalStore(subscribe, getCollapsed)
}

/** 这条会不会自己走：正在数（expiresAt）或收起期间冻着（remainingMs）都算。 */
function willExpire(notice: Notice): boolean {
  return notice.expiresAt !== null || notice.remainingMs !== null
}

/** 按档位算出这条的时间字段。收起时递进来的不马上数，先把整轮时长冻住。 */
function timingFor(kind: NoticeKind): { expiresAt: number | null; remainingMs: number | null } {
  const lifespan = LIFESPAN[kind]
  if (lifespan <= 0) return { expiresAt: null, remainingMs: null }
  return collapsed ? { expiresAt: null, remainingMs: lifespan } : { expiresAt: Date.now() + lifespan, remainingMs: null }
}

function scheduleNext(): void {
  if (timer !== null) {
    clearTimeout(timer)
    timer = null
  }
  if (collapsed) return
  const now = Date.now()
  let soonest = Infinity
  for (const notice of notices) {
    if (notice.expiresAt !== null && notice.expiresAt < soonest) soonest = notice.expiresAt
  }
  if (soonest === Infinity) return
  timer = setTimeout(tick, Math.max(0, soonest - now))
}

function tick(): void {
  timer = null
  const now = Date.now()
  const kept = notices.filter((notice) => notice.expiresAt === null || notice.expiresAt > now)
  if (kept.length !== notices.length) {
    notices = kept
    publish()
  }
  scheduleNext()
}

/** 递一条提示，返回它的 ID。
 *  同样的文字已经在屏上就不再叠第二条，只把它的时间续上：连点五次「应用」被拒五次，
 *  该看到的是一条还在那儿的，而不是五条一模一样。错误档不续也不消失。 */
export function pushNotice(kind: NoticeKind, text: string): number {
  const trimmed = text.trim()
  if (trimmed === '') return 0
  const existing = notices.find((notice) => notice.text === trimmed)
  if (existing) {
    if (willExpire(existing) || LIFESPAN[kind] > 0) {
      const timing = timingFor(kind)
      notices = notices.map((notice) => (notice.id === existing.id ? { ...notice, kind, ...timing } : notice))
      publish()
      scheduleNext()
    }
    return existing.id
  }
  const notice: Notice = { id: nextId, kind, text: trimmed, ...timingFor(kind) }
  nextId += 1
  notices = [...notices, notice]
  // 超出上限先丢最老的，但「永不消失」的错误档不能被成功回执挤掉：
  // 从头部找到第一条会自动消失的先扔，全都不会消失时仍按最老的扔。
  if (notices.length > MAX_NOTICES) {
    const first = notices.findIndex(willExpire)
    notices = first === -1 ? notices.slice(1) : notices.filter((_, index) => index !== first)
  }
  publish()
  scheduleNext()
  return notice.id
}

export function dismissNotice(id: number): void {
  const kept = notices.filter((notice) => notice.id !== id)
  if (kept.length === notices.length) return
  notices = kept
  publish()
  scheduleNext()
}

export function clearNotices(): void {
  if (notices.length === 0) return
  notices = []
  publish()
  scheduleNext()
}

export function setNoticeCollapsed(next: boolean): void {
  if (collapsed === next) return
  collapsed = next
  const now = Date.now()
  if (next) {
    // 收起＝把所有还在数的倒计时冻在「还剩多少」上，用户没看见的这段时间不该算进去。
    notices = notices.map((notice) => (notice.expiresAt === null ? notice : { ...notice, remainingMs: Math.max(0, notice.expiresAt - now), expiresAt: null }))
  } else {
    // 展开＝接着冻住的剩余往下数；没有剩余记录的（理论不该有）按整轮时长补上。
    notices = notices.map((notice) => {
      if (notice.expiresAt !== null || notice.remainingMs === null) return notice
      return { ...notice, expiresAt: now + notice.remainingMs, remainingMs: null }
    })
  }
  publish()
  scheduleNext()
}

export function toggleNoticeCollapsed(): void {
  setNoticeCollapsed(!collapsed)
}

/** 只剩测试和「重置界面」会用：把面板恢复到刚进应用时的样子。 */
export function resetNotices(): void {
  if (timer !== null) {
    clearTimeout(timer)
    timer = null
  }
  notices = []
  collapsed = false
  nextId = 1
  publish()
}
