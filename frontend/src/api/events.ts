import { Events } from '@wailsio/runtime'
import type { Task } from '../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import type { CandidateStatus } from '../../bindings/github.com/xiaokentrl/github-net/internal/accelerator/models'

// 事件名必须和 Go 侧 internal/event 的常量一致，改一边就要改另一边。
const STATUS_EVENT = 'app:runtime-status'
const TASK_EVENT = 'task:progress'
const DETECT_EVENT = 'detect:progress'

export type RuntimeStatusPayload = {
  sequence: number
  state: string
  windowVisible: boolean
  emittedAt: string
}

// 订阅运行状态事件，返回退订函数；调用方销毁时必须退订，否则回调会一直堆积。
export function subscribeRuntimeStatus(onPayload: (payload: RuntimeStatusPayload) => void): () => void {
  return Events.On(STATUS_EVENT, (event: Events.WailsEvent) => {
    const payload = event.data as RuntimeStatusPayload | undefined
    if (!payload || typeof payload.sequence !== 'number') return
    onPayload(payload)
  })
}

// ===== 任务与候选进度 =====
// 这两条事件的载荷不经过绑定生成器（生成器只覆盖服务方法签名），类型在这里镜像 Go 侧的 JSON 字段：
//   task:progress   ← internal/task.Progress        {sequence, task}
//   detect:progress ← internal/accelerator.Progress  候选级粒度
// 改 Go 侧字段必须同步这里；镜像错了界面会拿 undefined，等于事件丢失。

export type TaskProgressPayload = { sequence: number; task: Task }

export type DetectProgressPayload = {
  sequence: number
  itemId: string
  hostname?: string
  candidateId?: string
  phase?: string
  status?: string
  done: number
  total: number
  active: number
  probesUsed: number
  dnsQueries: number
  candidate?: {
    id: string
    hostname: string
    ip: string
    ipv6: boolean
    status: CandidateStatus
    latencyMs: number
    tlsMs: number
    reason?: string
    startedAt?: string
    finishedAt?: string
  } | null
}

export function subscribeTaskProgress(onPayload: (payload: TaskProgressPayload) => void): () => void {
  return Events.On(TASK_EVENT, (event: Events.WailsEvent) => {
    const payload = event.data as TaskProgressPayload | undefined
    if (!payload || typeof payload.sequence !== 'number' || !payload.task) return
    onPayload(payload)
  })
}

export function subscribeDetectProgress(onPayload: (payload: DetectProgressPayload) => void): () => void {
  return Events.On(DETECT_EVENT, (event: Events.WailsEvent) => {
    const payload = event.data as DetectProgressPayload | undefined
    if (!payload || typeof payload.sequence !== 'number' || !payload.itemId) return
    onPayload(payload)
  })
}
