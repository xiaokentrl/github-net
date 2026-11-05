// 全应用唯一的项目状态模型，只有四档，见名知义：
//   处理中（蓝）／失败（红）／已写Hosts（绿）／未写Hosts（灰）
//
// 树、状态徽章、全二级菜单表、当前写入 IP 列、候选表、队列明细都从这里取档位，
// 颜色只用这四个值。状态源每次渲染都会重算，一变所有视图一起变，不会停在旧颜色上。
//
// 「已写Hosts」的档位先看后端现读回来的逐条事实 entries[].written，
// 再看这一批地址的实测结论：一条能加速的都没有，那一格就压成「未写」（拍板 2026-11-06 晚）。
// 实测结论有两个来源（同名同址，缺一个就认另一个）：后端从候选池贴回的 entries[].probeStatus（重启也还在）
// 和最近一轮检测里每条候选的定格。没测过不等于连不通——一点证据都没有时不降级，照文件事实给档。
import { Status } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import type { Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import type { DetectResultView, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'

export type Tone = 'working' | 'failed' | 'written' | 'unwritten'
export type Progress = 'detecting' | 'applying' | 'restoring'
export type ProjectState = { tone: Tone; progress?: Progress; reason?: string; written: boolean }

export const TONE_VALUES: readonly Tone[] = ['working', 'failed', 'written', 'unwritten']

// 只有这三种是「测完给出的结论」。排队中、在测、被取消、没排上测——这些都只是还没有结论，
// 拿它们当证据会把「没测过」显示成「连不通」。
const DECISIVE = new Set(['available', 'unavailable', 'timeout'])

// addressUsable 判断「这一条地址到底能不能加速」。只认这条地址自己的定格：
// 没有定格＝不知道＝算能用（不许凭不知道降级）。unavailable／timeout＝连不通。
export function addressUsable(status: string | undefined): boolean {
  if (!status || !DECISIVE.has(status)) return true
  return status === 'available'
}

// 这个项目手上所有「已出结论」的地址，两个来源摊平：区段里正写着的地址自己的定格 + 最近一轮每条候选的定格。
function verdictsOf(hosts: HostsState | undefined, result: DetectResultView | undefined): string[] {
  const out: string[] = []
  for (const entry of hosts?.entries ?? []) {
    if (entry.ip && entry.probeStatus) out.push(entry.probeStatus)
  }
  if (result && !result.cancelled) {
    for (const host of result.hosts ?? []) {
      for (const c of host.candidates ?? []) out.push(String(c.status))
    }
  }
  return out.filter((s) => DECISIVE.has(s))
}

// 「全都不能加速」＝有实测证据、且证据里没有任何一条 available。
// 这一句判的是整个项目，和逐条的 addressUsable 是同一条规则的两次落点：一条不通只降那一条。
export function nothingAccelerates(hosts: HostsState | undefined, result: DetectResultView | undefined): boolean {
  const verdicts = verdictsOf(hosts, result)
  if (verdicts.length === 0) return false
  return !verdicts.some((s) => s === 'available')
}

// 四档文案键；处理中再按「在做什么」换成更具体的一个词。
export const TONE_LABEL: Record<Tone, string> = {
  working: 'state.working',
  failed: 'state.failed',
  written: 'state.written',
  unwritten: 'state.unwritten',
}

export const PROGRESS_LABEL: Record<Progress, string> = {
  detecting: 'state.detecting',
  applying: 'state.applying',
  restoring: 'state.restoring',
}

// 三张表覆盖后端全部状态，由 internal/app/status_tone_parity_test.go 逐字核对。
export const TASK_TONE: Record<string, Tone> = {
  'waiting': 'working',
  'running': 'working',
  'restoring': 'working',
  'done': 'written',
  'failed': 'failed',
  'skipped': 'unwritten',
  'rolledback': 'failed',
  'restored': 'unwritten',
}

export const CANDIDATE_TONE: Record<string, Tone> = {
  'queued': 'working',
  'probing': 'working',
  'available': 'written',
  'unavailable': 'failed',
  'timeout': 'failed',
  'cancelled': 'unwritten',
  'notStarted': 'unwritten',
  'untested': 'unwritten',
}

export const HOSTS_TONE: Record<string, Tone> = {
  'unknown': 'unwritten',
  'none': 'unwritten',
  'partial': 'written',
  'applied': 'written',
  'conflict': 'failed',
  'readFailed': 'failed',
}

const ACTIVE = new Set<string>([Status.StatusWaiting, Status.StatusRunning, Status.StatusRestoring])
const TERMINAL = new Set<string>([
  Status.StatusDone, Status.StatusFailed, Status.StatusSkipped,
  Status.StatusRolledback, Status.StatusRestored,
])

export type ToneTask = Pick<Task, 'itemId' | 'kind' | 'status' | 'errorMsg' | 'finishedAt'>

export function toneForTask(task: Pick<Task, 'status'>): Tone {
  const status = String(task.status)
  if (ACTIVE.has(status)) return 'working'
  return TASK_TONE[status] ?? 'unwritten'
}

export function toneForCandidate(status: string): Tone {
  return CANDIDATE_TONE[status] ?? 'unwritten'
}

export function toneForHosts(status: string | undefined): Tone {
  return status ? HOSTS_TONE[status] ?? 'unwritten' : 'unwritten'
}

function progressOf(task: ToneTask): Progress {
  if (task.kind === 'detect') return 'detecting'
  if (task.kind === 'apply') return 'applying'
  return 'restoring'
}

// 单一入口：所有视图都调它。优先级＝在跑的 > 坏消息 > 逐条写入事实，最后再过一道「能不能加速」的降级。
export function projectState(hosts: HostsState | undefined, tasks: readonly ToneTask[], itemId: string, result?: DetectResultView): ProjectState {
  let active: ToneTask | undefined
  let last: ToneTask | undefined
  for (const task of tasks) {
    if (task.itemId !== itemId) continue
    const status = String(task.status)
    if (ACTIVE.has(status)) active = task
    else if (TERMINAL.has(status)) last = task
  }
  const written = (hosts?.entries ?? []).some((entry) => entry.written && entry.ip)
  if (active) return { tone: 'working', progress: progressOf(active), written }

  // 坏消息也走登记过的映射表，不在代码里另抄一份状态名。
  if (hosts && HOSTS_TONE[String(hosts.status)] === 'failed') {
    return { tone: 'failed', reason: hosts.message, written }
  }
  if (last && TASK_TONE[String(last.status)] === 'failed') {
    return { tone: 'failed', reason: last.errorMsg, written }
  }
  // 这一轮测完一个可用候选都没有：不再报失败，而是照「这一项现在能不能加速」压成未写档（拍板 2026-11-06 晚）。
  const usable = !nothingAccelerates(hosts, result)
  return { tone: written && usable ? 'written' : 'unwritten', written }
}

// 一行项目该显示的那个词；处理中会说得更具体。
export function stateLabel(state: ProjectState): string {
  if (state.tone === 'working' && state.progress) return PROGRESS_LABEL[state.progress]
  return TONE_LABEL[state.tone]
}

// 兼容旧调用点，内部走同一入口。
export function toneForItem(hosts: HostsState | undefined, tasks: readonly ToneTask[], itemId: string, result?: DetectResultView): Tone {
  return projectState(hosts, tasks, itemId, result).tone
}
