import type { HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { HostsStatus, Kind, Status } from '../../api/client'
import { PROGRESS_LABEL, TONE_LABEL, noAvailableThisRound, nothingAccelerates, type Tone } from '../../features/tasks/statusTone'
import type { DetectResultView } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'

// 树不再自己发明档位：用和表格、徽章、队列同一套四档、同一批词、同一批颜色。
// 这里只做一件事——把队列折算成「每个项目现在在做什么 / 有没有没做成」。
export type LiveKind = 'detecting' | 'applying' | 'restoring' | 'failed'

export type TreeStatus = { key: string; tone: Tone; reason?: string }

const ACTIVE = new Set<string>([Status.StatusWaiting, Status.StatusRunning, Status.StatusRestoring])
const FAILED = new Set<string>([Status.StatusFailed, Status.StatusRolledback])

// 在途优先于失败：同一个项目重新检测时，先显示正在检测。
export function liveByItem(tasks: { itemId: string; kind: Kind; status: Status }[]): Map<string, LiveKind> {
  const out = new Map<string, LiveKind>()
  for (const task of tasks) {
    if (!task.itemId || !FAILED.has(task.status)) continue
    out.set(task.itemId, 'failed')
  }
  for (const task of tasks) {
    if (!task.itemId || !ACTIVE.has(task.status)) continue
    if (task.kind === Kind.KindDetect) out.set(task.itemId, 'detecting')
    else if (task.kind === Kind.KindApply) out.set(task.itemId, 'applying')
    else if (task.kind === Kind.KindRestore) out.set(task.itemId, 'restoring')
  }
  return out
}

// 四档判定，和 statusTone.projectState 同规则：在跑的 > 坏消息 > 本轮零可用（红） > 逐条写入事实，
// 最后过一道「能不能加速」的降级。「已写Hosts」看后端现读的 entries[].written。
export function itemStatus(hosts: HostsState | undefined, live: LiveKind | undefined, result?: DetectResultView): TreeStatus {
  if (live === 'detecting') return { key: PROGRESS_LABEL.detecting, tone: 'working' }
  if (live === 'applying') return { key: PROGRESS_LABEL.applying, tone: 'working' }
  if (live === 'restoring') return { key: PROGRESS_LABEL.restoring, tone: 'working' }
  if (live === 'failed') return { key: TONE_LABEL.failed, tone: 'failed' }
  if (hosts?.status === HostsStatus.HostsConflict || hosts?.status === HostsStatus.HostsReadFailed) {
    return { key: TONE_LABEL.failed, tone: 'failed', reason: hosts.message }
  }
  const written = (hosts?.entries ?? []).some((entry) => entry.written && entry.ip)
  // 本轮检测一个可用候选都没有：没有可写 Hosts，色点和行底色一起走失败档（红），
  // 和表格、徽章、队列明细同一个状态源。
  if (noAvailableThisRound(result)) return { key: TONE_LABEL.failed, tone: 'failed' }
  // 手上地址全连不通（例如旧映射失效）：写着一条连不通的地址不算写成，退回未写档。
  if (written && nothingAccelerates(hosts, result)) return { key: TONE_LABEL.unwritten, tone: 'unwritten' }
  return written
    ? { key: TONE_LABEL.written, tone: 'written' }
    : { key: TONE_LABEL.unwritten, tone: 'unwritten' }
}
