import { useCallback } from 'react'
import { clearQueue, getTasks, restoreAllHosts } from '../../api/client'
import { t } from '../../i18n'
import { applyTaskSnapshot, refreshAllResults, useTaskQueue } from './taskStore'

// 「恢复」只有这一份实现：工具栏那颗和队列面板那颗都走这里，免得同一条规则写两遍。
// 规则照原型（第 3740–3768 行）：队列里还有没落定的任务就先不动；用户确认后才让后端一笔原子还原；
// 真还原过就把队列记录清掉，最后重拉快照。失败交给调用方挂在各自的错误位置。
// onRestored 是「Hosts 变了，去重读一遍」的出口：还原不进队列，也就没有任务落定事件，
// 不在这里喊一声，树上的「已替换/部分生效」会一直停在还原前的样子。
export function useRestoreAllTasks(onError: (err: unknown) => void, onRestored?: () => void) {
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
      // 「当前写入 IP」的权威在后端检测结果里，还原后整批重拉一遍。
      .then(() => refreshAllResults())
      .then(() => onRestored?.())
      .catch(onError)
  }, [busy, onError, onRestored])

  return { busy, restoreAll }
}
