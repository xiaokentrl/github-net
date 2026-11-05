import { useCallback, useState } from 'react'
import { getTasks, restoreAllHosts } from '../../api/client'
import { Dialog } from '../../components/ui/Dialog'
import { pushNotice } from '../../components/ui/toastStore'
import uiStyles from '../../components/ui/ui.module.css'
import { t } from '../../i18n'
import { applyTaskSnapshot, refreshAllResults, useTaskQueue } from './taskStore'

// 「恢复」只有这一份实现：工具栏那颗走这里，队列面板不放还原按钮。
// 规则：队列里还有没落定的任务就先不动；用户确认后才让后端一笔原子还原；还原完重拉一遍快照。
// 队列记录不在这里清——恢复不进队列，历史轮次留着让用户自己按「清空已完成」收。
// onRestored 是「Hosts 变了，去重读一遍」的出口：还原不进队列，也就没有任务落定事件，
// 不在这里喊一声，树上的「已替换/部分生效」会一直停在还原前的样子。
//
// 确认框走公共 Dialog，不用系统原生 window.confirm：原生框的标题和两颗按钮归 WebKit，
// 语言包一个字也改不了，写不出「先不还原／全部还原」这种带结果的文案。
// 点按钮只是开框，一个数据都不动；只有框里点「全部还原」才真发命令（开框只读的规矩）。
export function useRestoreAllTasks(onError: (err: unknown) => void, onRestored?: () => void) {
  const state = useTaskQueue()
  const busy = state.tasks.some(
    (task) => task.status === 'running' || task.status === 'waiting' || task.status === 'restoring',
  )
  const [askOpen, setAskOpen] = useState(false)

  const restoreAll = useCallback(() => {
    if (busy) return
    setAskOpen(true)
  }, [busy])

  const runRestoreAll = useCallback(() => {
    setAskOpen(false)
    restoreAllHosts()
      .then((count) => {
        // 全部还原不进队列、也没有任务落定事件：除了树里那行状态退回「未替换」，
        // 界面上本来没有别的回执，这里补一条。成功档是回执，读完自己走。
        if (count > 0) pushNotice('success', t('hosts.restoreAll.done', { count }))
      })
      .then(() => getTasks().then(applyTaskSnapshot))
      // 「当前写入 IP」的权威在后端检测结果里，还原后整批重拉一遍。
      .then(() => refreshAllResults())
      .then(() => onRestored?.())
      .catch(onError)
  }, [onError, onRestored])

  const confirmDialog = askOpen ? (
    <Dialog
      title={t('queue.restoreAll.ask.title')}
      onClose={() => setAskOpen(false)}
      footer={
        <>
          <button type="button" className={uiStyles.btn} onClick={() => setAskOpen(false)}>
            {t('queue.restoreAll.ask.cancel')}
          </button>
          <button type="button" className={`${uiStyles.btn} ${uiStyles.primary}`} onClick={runRestoreAll}>
            {t('queue.restoreAll.ask.action')}
          </button>
        </>
      }
    >
      <p>{t('queue.restoreAll.confirm')}</p>
    </Dialog>
  ) : null

  return { busy, restoreAll, confirmDialog }
}
