// 全应用唯一的项目状态模型，只有四档，见名知义：
//   处理中（蓝）／失败（红）／已写Hosts（绿）／未写Hosts（灰）
//
// 树、状态徽章、全二级菜单表、当前写入 IP 列、候选表、队列明细都从这里取档位，
// 颜色只用这四个值。状态源每次渲染都会重算，一变所有视图一起变，不会停在旧颜色上。
//
// 「已写Hosts / 未写Hosts」只看后端现读回来的逐条事实 entries[].written，
// 不看检测结果：这样页面刷新、异步回读、检测完成都不会让状态自己乱跳。
import { Status } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import type { Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import type { DetectResultView, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'

export type Tone = 'working' | 'failed' | 'written' | 'unwritten'
export type Progress = 'detecting' | 'applying' | 'restoring'
export type ProjectState = { tone: Tone; progress?: Progress; reason?: string; written: boolean }

export const TONE_VALUES: readonly Tone[] = ['working', 'failed', 'written', 'unwritten']

// 「这一轮一个可用候选都没有」算坏消息，落 failed 档（红）。
// 没检测过、这一轮还没有域名行、用户主动取消，都不算：那些不是「测完没结果」。
// 有没有可用按候选实算，和详情页挑最优那份口径一致，不另抄一份数字。
export function noUsableResult(result: DetectResultView | undefined): boolean {
  if (!result || result.cancelled) return false
  const hosts = result.hosts ?? []
  if (hosts.length === 0) return false
  return !hosts.some((h) => (h.candidates ?? []).some((c) => String(c.status) === 'available'))
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

// 单一入口：所有视图都调它。优先级＝在跑的 > 坏消息 > 逐条写入事实。
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
  // 这一轮测完一个可用候选都没有：和失败同档（红），列表、树、徽章一起变红。
  if (noUsableResult(result)) return { tone: 'failed', written }
  return { tone: written ? 'written' : 'unwritten', written }
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
