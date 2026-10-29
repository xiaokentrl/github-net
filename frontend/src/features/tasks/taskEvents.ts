import type { Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import { applyTaskEvent, applyTaskSnapshot, applyDetectEvent, isTerminalTask, markTaskLoadError } from './taskStore'
import { getTasks } from '../../api/client'
import { subscribeTaskProgress, subscribeDetectProgress } from '../../api/events'

// 应用级只挂一份任务事件订阅：先订阅、再拉快照，快照水位以内的事件被整体替换，
// 之后的事件按水位逐条应用。StrictMode 下本函数可能被调两次（挂载-卸载-挂载），
// 每次调用都返回自己的退订函数，卸载时退掉，不会留双份监听。
// onSettled 在任务到终态时被叫一次：应用/还原完成后主界面要重读 Hosts 状态，
// 这个联动由调用方（App）决定，事件层只负责叫人。
export function subscribeTaskEvents(onSettled?: (task: Task) => void): () => void {
  const unsubscribeTask = subscribeTaskProgress((payload) => {
    applyTaskEvent(payload)
    if (isTerminalTask(payload.task.status)) onSettled?.(payload.task)
  })
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
