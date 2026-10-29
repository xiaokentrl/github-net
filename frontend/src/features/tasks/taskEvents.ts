import { applyTaskEvent, applyTaskSnapshot, applyDetectEvent, markTaskLoadError } from './taskStore'
import { getTasks } from '../../api/client'
import { subscribeTaskProgress, subscribeDetectProgress } from '../../api/events'

// 应用级只挂一份任务事件订阅：先订阅、再拉快照，快照水位以内的事件被整体替换，
// 之后的事件按水位逐条应用。StrictMode 下本函数可能被调两次（挂载-卸载-挂载），
// 每次调用都返回自己的退订函数，卸载时退掉，不会留双份监听。
export function subscribeTaskEvents(): () => void {
  const unsubscribeTask = subscribeTaskProgress(applyTaskEvent)
  const unsubscribeDetect = subscribeDetectProgress(applyDetectEvent)

  void getTasks()
    .then(applyTaskSnapshot)
    .catch(() => {
      // 快照拉不到不装成空队列：store 记下失败，面板给重试入口。
      markTaskLoadError()
    })

  return () => {
    unsubscribeTask()
    unsubscribeDetect()
  }
}
