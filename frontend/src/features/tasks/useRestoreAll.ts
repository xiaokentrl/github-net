import { useCallback } from 'react'
import { clearQueue, getTasks, restoreAllHosts } from '../../api/client'
import { t } from '../../i18n'
import { applyTaskSnapshot, useTaskQueue } from './taskStore'

// 「恢复」只有这一份实现：工具栏那颗和队列面板那颗都走这里，免得同一条规则写两遍。
// 规则照原型（第 3740–3768 行）：队列里还有没落定的任务就先不动；用户确认后才让后端一笔原子还原；
// 真还原过就把队列记录清掉，最后重拉快照。失败交给调用方挂在各自的错误位置。
export function useRestoreAllTasks(onError: (err: unknown) => void) {
  const state = useTaskQueue()
  const busy = state.tasks.some(
    (task) => task.status === 'running' || task.status === 'waiting' || task.status === 'restoring',
  )

  const restoreAll = useCallback(() => {
    if (busy) return
    if (!window.confirm(t('queue.restoreAll.confirm'))) return
    restoreAllHosts()
      .then((count) => {
        if (count > 0) return clearQueue().catch(() => undefined)
        return undefined
      })
      .then(() => getTasks().then(applyTaskSnapshot))
      .catch(onError)
  }, [busy, onError])

  return { busy, restoreAll }
}
