import { Events } from '@wailsio/runtime'
import type { Task } from '../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import type { Config } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { AutomationStatus } from '../../bindings/github.com/xiaokentrl/github-net/internal/event/models'
import type { CandidateStatus } from '../../bindings/github.com/xiaokentrl/github-net/internal/accelerator/models'

// 事件名必须和 Go 侧 internal/event 的常量一致，改一边就要改另一边。
const STATUS_EVENT = 'app:runtime-status'
const TASK_EVENT = 'task:progress'
const DETECT_EVENT = 'detect:progress'
const SETTINGS_EVENT = 'settings:changed'
const UPDATE_EVENT = 'update:status'
const AUTOMATION_EVENT = 'automation:status'
// 事件名必须和 Go 侧 internal/app/browser_restart.go 的 browserRestartedEvent 一致。
const BROWSER_EVENT = 'browser:restarted'

// reason 是后端给的「这一轮到底做了什么」原因码。旧版特权服务/旧回执没有这个字段，
// 前端按 undefined 走原来的「重启了几家 / 没发现浏览器」两档，不炸。
export type BrowserRestartedPayload = {
  restarted: number
  families: string[]
  reason?: string
  scanned?: number
  launchers?: number
  signalFailed?: number
  notGone?: number
  launchFailed?: number
}

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
    applied: boolean
  } | null
}

export function subscribeTaskProgress(onPayload: (payload: TaskProgressPayload) => void): () => void {
  return Events.On(TASK_EVENT, (event: Events.WailsEvent) => {
    const payload = event.data as TaskProgressPayload | undefined
    if (!payload || typeof payload.sequence !== 'number' || !payload.task) return
    onPayload(payload)
  })
}

export type UpdateNoticePayload = {
  info?: {
    currentVersion: string
    latestVersion?: string
    updateAvailable: boolean
    releaseUrl?: string
    publishedAt?: string
    checkedAt: string
  }
  error?: string
}

// 启动自动检查的结果走事件；设置页手动检查走命令返回值，两条路最终都落到同一块显示。
export function subscribeUpdateStatus(onPayload: (payload: UpdateNoticePayload) => void): () => void {
  return Events.On(UPDATE_EVENT, (event: Events.WailsEvent) => {
    const payload = event.data as UpdateNoticePayload | undefined
    if (!payload) return
    onPayload(payload)
  })
}

// 托盘菜单切主题后推来的新配置（载荷形如 { config: Config }）：
// App 用它整体刷新（语言/主题/缩放同一条 applyConfig 路）。
export function subscribeSettingsChanged(onPayload: (payload: Config) => void): () => void {
  return Events.On(SETTINGS_EVENT, (event: Events.WailsEvent) => {
    const payload = event.data as { config?: Config } | undefined
    if (!payload || !payload.config) return
    onPayload(payload.config)
  })
}

export function subscribeDetectProgress(onPayload: (payload: DetectProgressPayload) => void): () => void {
  return Events.On(DETECT_EVENT, (event: Events.WailsEvent) => {
    const payload = event.data as DetectProgressPayload | undefined
    if (!payload || typeof payload.sequence !== 'number' || !payload.itemId) return
    onPayload(payload)
  })
}

// ===== 启动自动化 =====
// 载荷的字段和 GetAutomationStatus 返回的是同一份（internal/event.AutomationStatus），
// 所以类型直接用绑定生成的那一份，不再手抄。stage 是机器码，句子由界面按语言拼。
export function subscribeAutomationStatus(onPayload: (payload: AutomationStatus) => void): () => void {
  return Events.On(AUTOMATION_EVENT, (event: Events.WailsEvent) => {
    const payload = event.data as AutomationStatus | undefined
    if (!payload || typeof payload.stage !== 'string' || payload.stage === '') return
    onPayload(payload)
  })
}

// 订阅「应用后浏览器已重启」回执：restarted 是重启了几家，families 是家族名清单。
export function subscribeBrowserRestarted(onPayload: (payload: BrowserRestartedPayload) => void): () => void {
  return Events.On(BROWSER_EVENT, (event: Events.WailsEvent) => {
    const payload = event.data as BrowserRestartedPayload | undefined
    if (!payload || typeof payload.restarted !== 'number') return
    onPayload(payload)
  })
}
