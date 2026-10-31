import { useSyncExternalStore } from 'react'
import type { Round, Snapshot, Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import type { DetectResultView } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { getDetectResult, getTasks } from '../../api/client'
import type { DetectProgressPayload, TaskProgressPayload } from '../../api/events'

// 队列与检测结果是跨页面共享的状态：详情页和队列面板读同一份，这里就是那唯一一份。
// 不引第二份 localStorage、也不让组件各存任务副本；页面卸载不影响它，订阅挂在应用级。
//
// 用 React 自带的 useSyncExternalStore 做模块级外部存储，不引入新的状态库——
// 现有代码都是 hooks + props 风格，这里保持一致；外部存储的纪律（快照不可变、订阅可清理）都守住了。

export type LiveCounters = { done: number; total: number; active: number; probesUsed: number; dnsQueries: number }

type TaskState = {
  loaded: boolean
  loadError: boolean
  paused: boolean
  sequence: number
  tasks: Task[]
  // rounds 是轮次（原型队列的根节点/包裹）：一次批量检测、一次应用、一次恢复各算一轮，
  // 任务明细按 roundId 挂在轮下。只列「还有任务挂着」的轮，后端已剪空。
  rounds: Round[]
  // results 是每个项目最近一轮检测的权威快照；事件带来的行是临时补画，任务终态后会被 refreshResult 换成后端原样。
  results: Record<string, DetectResultView>
  // live 是正在跑的那一轮的整体计数（解析/探测阶段的 done/total/active 等），任务行上显示用。
  live: Record<string, LiveCounters>
}

let state: TaskState = {
  loaded: false,
  loadError: false,
  paused: false,
  sequence: 0,
  tasks: [],
  rounds: [],
  results: {},
  live: {},
}

const listeners = new Set<() => void>()

function publish(next: Partial<TaskState>) {
  state = { ...state, ...next }
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function getSnapshot(): TaskState {
  return state
}

// 队列面板和详情页都从这一个 hook 读队列；应用级只挂一份事件订阅（见 taskEvents.ts）。
export function useTaskQueue(): TaskState {
  return useSyncExternalStore(subscribe, getSnapshot)
}

// applyTaskSnapshot 先订阅后快照：水位以内的事件会被这条整体替换掉，不重复应用。
export function applyTaskSnapshot(snap: Snapshot) {
  publish({
    loaded: true,
    loadError: false,
    paused: snap.paused,
    sequence: snap.sequence,
    tasks: snap.tasks ?? [],
    rounds: snap.rounds ?? [],
  })
}

// markTaskLoadError 记下「快照没拉到」；队列面板据此显示错误和重试入口，不装成空队列。
export function markTaskLoadError() {
  publish({ loaded: true, loadError: true })
}

// 轮只由后端发号，前端不自己拼一个根节点。事件里出现没见过的新 roundId 就说明队列新开了一轮，
// 这时补拉一次快照，把那条轮（和同一批的其它任务）一起拿回来——否则任务行挂在队列里、
// 界面上却没有它们的根节点，任务列表整块显示成空队列。
// 一次开轮会有多条事件，所以同一时刻只拉一次；拉到的水位比手上的旧就丢掉，不让界面退回旧状态。
let roundRefreshRunning = false

function refreshRounds() {
  if (roundRefreshRunning) return
  roundRefreshRunning = true
  getTasks()
    .then((snap) => {
      if (snap.sequence >= state.sequence) applyTaskSnapshot(snap)
    })
    .catch(() => {
      /* 补拉失败就在这里不叠错误：队列面板自己有快照失败的显示与重试入口 */
    })
    .finally(() => {
      roundRefreshRunning = false
    })
}

// applyTaskEvent 按水位收任务事件：旧序号丢弃（快照里已包含），新任务按 ID 覆盖。
// 检测任务开跑（进入 running）意味着新一轮开始：把该项目上一轮的候选和计数清掉，
// 免得上一轮的行和这一轮的行混在一张表里。
export function applyTaskEvent(payload: TaskProgressPayload) {
  if (payload.sequence <= state.sequence) return
  const tasks = state.tasks.some((t) => t.id === payload.task.id)
    ? state.tasks.map((t) => (t.id === payload.task.id ? payload.task : t))
    : [...state.tasks, payload.task]
  const next: Partial<TaskState> = { sequence: payload.sequence, tasks }
  if (payload.task.roundId && !state.rounds.some((round) => round.id === payload.task.roundId)) refreshRounds()
  if (payload.task.kind === 'detect' && payload.task.status === 'running') {
    const results = { ...state.results }
    const live = { ...state.live }
    delete results[payload.task.itemId]
    delete live[payload.task.itemId]
    next.results = results
    next.live = live
  }
  if (isTerminalTask(payload.task.status) && (payload.task.kind === 'detect' || payload.task.kind === 'apply' || payload.task.kind === 'restore')) {
    // 终态后后端已经把整轮结论存好（应用/还原收尾还会按 Hosts 现状重标「当前生效」），
    // 这里拉权威快照替换事件补画的临时行。
    void refreshResult(payload.task.itemId)
    const live = { ...state.live }
    delete live[payload.task.itemId]
    next.live = live
  }
  publish(next)
}

// applyDetectEvent 把候选行合并进该项目的视图。行按 candidateID 幂等更新：
// 同一行的重复事件不产生第二行，乱序事件以先到的定格态为准（终态行不再被改写）。
export function applyDetectEvent(p: DetectProgressPayload) {
  const live: Record<string, LiveCounters> = { ...state.live, [p.itemId]: { done: p.done, total: p.total, active: p.active, probesUsed: p.probesUsed, dnsQueries: p.dnsQueries } }
  if (!p.candidate) {
    publish({ live })
    return
  }
  const previous = state.results[p.itemId] ?? emptyResult(p.itemId)
  const row = p.candidate
  const hosts = (previous.hosts ?? []).map((h) => ({ ...h, candidates: [...(h.candidates ?? [])] }))
  let host = hosts.find((h) => h.hostname === row.hostname)
  if (!host) {
    host = { hostname: row.hostname, resolved: true, candidates: [] }
    hosts.push(host)
  }
  const existing = host.candidates.findIndex((c) => c.id === row.id)
  const current = existing >= 0 ? host.candidates[existing] : null
  if (current && isFinalStatus(current.status)) {
    // 已经定格的行不再吃迟到事件，界面上「定格后闪回检测中」就是这么来的。
    publish({ live })
    return
  }
  if (existing >= 0) host.candidates[existing] = row
  else host.candidates.push(row)
  publish({ live, results: { ...state.results, [p.itemId]: { ...previous, hosts } } })
}

// refreshResult 从后端拉一个项目的权威结果放进 store；任务终态后调用。
export async function refreshResult(itemId: string): Promise<DetectResultView> {
  const view = await getDetectResult(itemId)
  publish({ results: { ...state.results, [itemId]: view } })
  return view
}

function emptyResult(itemId: string): DetectResultView {
  return { itemId, scopeId: '', strategyId: '', available: 0, probesUsed: 0, dnsQueries: 0, truncated: false, deadlineHit: false, cancelled: false, hosts: [] }
}

// refreshAllResults 把已在 store 里的项目全部重拉一遍：RestoreAll 一笔清空「当前生效」，
// 不走队列没有逐项事件，还原成功后调它，各详情页的「当前写入 IP」才回得来。
export async function refreshAllResults(): Promise<void> {
  await Promise.all(Object.keys(state.results).map((id) => refreshResult(id).catch(() => undefined)))
}

// isTerminalTask 判断任务是否已到终态；taskEvents 用它触发「任务落定」回调。
export function isTerminalTask(status: Task['status']): boolean {
  return status === 'done' || status === 'failed' || status === 'cancelled' || status === 'skipped' || status === 'rolledback' || status === 'restored'
}

function isFinalStatus(status: string): boolean {
  return status === 'available' || status === 'unavailable' || status === 'timeout' || status === 'cancelled' || status === 'notStarted' || status === 'untested'
}
