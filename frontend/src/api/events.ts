import { Events } from '@wailsio/runtime'

// 事件名必须和 Go 侧 internal/event 的常量一致，改一边就要改另一边。
const STATUS_EVENT = 'app:runtime-status'

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
