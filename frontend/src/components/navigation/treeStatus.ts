import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { DetectResultView } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { Kind, Status } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'

// 树上那一行的状态字与色点：把「后端读出来的 Hosts 状态」和「队列里正在跑的那几条」折算成一句话。
// 原型每个项目自己维护一套 8 档生命周期（ItemMeta，第 1299–1308 行），那是演示状态机；
// 这里不照抄那套机器，只照抄它显示出来的那几种说法，每个词都必须由后端真实数据推出来。

export type LiveKind = 'detecting' | 'applying' | 'restoring'

export type Tone = 'unset' | 'applied' | 'stale' | 'error' | 'pending' | 'detected'

export type TreeStatus = { key: string; tone: Tone }

const ACTIVE = new Set<string>([Status.StatusWaiting, Status.StatusRunning, Status.StatusRestoring])

// 队列折算成「哪一项正在被做什么」。只看还没跑完的任务：
// 终态记录要等用户点「清理已结束」才离开队列，让它们长期占着树上那一行的颜色就成了假象。
export function liveByItem(tasks: { itemId: string; kind: Kind; status: Status }[]): Map<string, LiveKind> {
  const out = new Map<string, LiveKind>()
  for (const task of tasks) {
    if (!task.itemId || !ACTIVE.has(task.status)) continue
    if (task.kind === Kind.KindDetect) out.set(task.itemId, 'detecting')
    else if (task.kind === Kind.KindApply) out.set(task.itemId, 'applying')
    else if (task.kind === Kind.KindRestore) out.set(task.itemId, 'restoring')
  }
  return out
}

function usableCandidates(result: DetectResultView | undefined) {
  return (result?.hosts ?? []).flatMap((host) => (host.candidates ?? []).filter((c) => c.status === 'available'))
}

// 一句话状态。顺序是「正在做的事 > 后端报的坏消息 > 查出来还没用的好消息 > Hosts 现状」：
// 界面同一时刻只能显示一个词，先说用户此刻最该知道的那件。
export function itemStatus(status: HostsStatus, live: LiveKind | undefined, result: DetectResultView | undefined): TreeStatus {
  if (live === 'detecting') return { key: 'tree.status.detecting', tone: 'pending' }
  if (live === 'applying') return { key: 'tree.status.applying', tone: 'pending' }
  if (live === 'restoring') return { key: 'tree.status.restoring', tone: 'pending' }

  if (status === HostsStatus.HostsConflict) return { key: 'badge.conflict', tone: 'error' }
  if (status === HostsStatus.HostsReadFailed) return { key: 'badge.readFailed', tone: 'error' }

  // 查出来的可用候选里有一条不是当前生效的那条：这一项还能再应用一次（原型叫「待更新」）。
  const usable = usableCandidates(result)
  if (usable.length > 0 && (status === HostsStatus.HostsApplied || status === HostsStatus.HostsPartial)) {
    return usable.some((c) => !c.applied) ? { key: 'tree.status.stale', tone: 'stale' } : { key: 'badge.applied', tone: 'applied' }
  }
  if (usable.length > 0 && (status === HostsStatus.HostsNone || status === HostsStatus.HostsUnknown || status === HostsStatus.$zero)) {
    return { key: 'tree.status.detected', tone: 'detected' }
  }

  switch (status) {
    case HostsStatus.HostsApplied:
      return { key: 'badge.applied', tone: 'applied' }
    case HostsStatus.HostsPartial:
      // 部分生效本身就是「还不齐」，用琥珀色点，和待更新同一档颜色。
      return { key: 'badge.partial', tone: 'stale' }
    case HostsStatus.HostsNone:
      return { key: 'badge.none', tone: 'unset' }
    default:
      return { key: 'badge.unknown', tone: 'unset' }
  }
}
