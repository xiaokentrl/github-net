import { useCallback, useEffect, useRef, useState } from 'react'
import { getTasks, previewRestoreHosts, restoreAllHosts } from '../../api/client'
import { getErrorMessage } from '../../api/errors'
import type { HostsRestorePreview } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { Dialog } from '../../components/ui/Dialog'
import { pushNotice } from '../../components/ui/toastStore'
import uiStyles from '../../components/ui/ui.module.css'
import { t } from '../../i18n'
import { RestorePreviewTable } from '../items/RestorePreviewTable'
import tableStyles from '../items/RestorePreviewTable.module.css'
import { applyTaskSnapshot, useTaskQueue } from './taskStore'
import { invalidate } from '../../sync/syncHub'

// 「恢复」只有这一份实现：工具栏那颗走这里，队列面板不放还原按钮。
// 规则：队列里还有没落定的任务就先不动；用户确认后才让后端一笔原子还原；还原完重拉一遍快照。
// 队列记录不在这里清——恢复不进队列，历史轮次留着让用户自己按「清空已完成」收。
// onRestored 是「Hosts 变了，去重读一遍」的出口：还原不进队列，也就没有任务落定事件，
// 不在这里喊一声，树上的「已替换/部分生效」会一直停在还原前的样子。
//
// 确认框走公共 Dialog，不用系统原生 window.confirm：原生框的标题和两颗按钮归 WebKit，
// 语言包一个字也改不了，写不出「先不还原／全部还原」这种带结果的文案。
// 点按钮只是开框，一个数据都不动；框里照「还原本菜单」同一套前后对照表（只读预览），
// 用户看清每条映射还原成什么样，才点「全部还原」真发命令（开框只读的规矩）。
// groups 是域名→二级菜单的归属（App 按目录 + Hosts 实写条目算好），对照表左列分组用。
export function useRestoreAllTasks(onError: (err: unknown) => void, onRestored?: () => void, groups: { name: string; hostnames: string[] }[] = []) {
  const state = useTaskQueue()
  const busy = state.tasks.some(
    (task) => task.status === 'running' || task.status === 'waiting' || task.status === 'restoring',
  )
  const [askOpen, setAskOpen] = useState(false)
  // 还原不进队列，busy 看不见它：这颗 ref 防同一笔还原被点两次。
  const restoringRef = useRef(false)
  // 前后对照只在开框那刻拉一遍全量预览：只读，不动队列也不动 Hosts。
  const [preview, setPreview] = useState<HostsRestorePreview | null>(null)
  const [previewError, setPreviewError] = useState('')
  useEffect(() => {
    if (!askOpen) return
    let live = true
    setPreview(null)
    setPreviewError('')
    previewRestoreHosts()
      .then((res) => { if (live) setPreview(res) })
      .catch((err: unknown) => { if (live) setPreviewError(getErrorMessage(err)) })
    return () => { live = false }
  }, [askOpen])
  const previewRows = preview?.rows ?? []
  const nothingToRestore = preview !== null && previewRows.length === 0

  const restoreAll = useCallback(() => {
    if (busy) return
    setAskOpen(true)
  }, [busy])

  // 返回 Promise：清理弹框把「清除已写入的 Hosts」和其它清理项一起排进同一批任务时，
  // 要能等这一笔还原落定再统一刷新，逻辑仍只有这一份。
  // skipCatalogRefresh 给清理弹框用：它自己会在整批结束后重读一次目录，这里不重复读。
  const runRestoreAll = useCallback((skipCatalogRefresh = false) => {
    // 双保险：队列转忙（弹框开着时别的入口排了任务）或上一笔还原还没收尾，都不再发。
    if (busy) return Promise.resolve()
    if (restoringRef.current) return Promise.resolve()
    restoringRef.current = true
    setAskOpen(false)
    return restoreAllHosts()
      .then((count) => {
        // 全部还原不进队列、也没有任务落定事件：除了树里那行状态退回「未替换」，
        // 界面上本来没有别的回执，这里补一条。一条都没还原也明说，免得点了像没反应。
        if (count > 0) pushNotice('success', t('hosts.restoreAll.done', { count }))
        else pushNotice('info', t('hosts.restoreAll.none'))
      })
      .then(() => getTasks().then(applyTaskSnapshot))
      // 「当前写入 IP」的权威在后端检测结果里，还原后整批重拉一遍。
      .then(() => invalidate('results'))
      .then(() => { if (!skipCatalogRefresh) onRestored?.() })
      .catch(onError)
      .finally(() => { restoringRef.current = false })
  }, [onError, onRestored, busy])

  const confirmDialog = askOpen ? (
    <Dialog
      title={t('queue.restoreAll.ask.title')}
      onClose={() => setAskOpen(false)}
      width={720}
      height={560}
      footer={
        <>
          <button type="button" className={uiStyles.btn} onClick={() => setAskOpen(false)}>
            {t('queue.restoreAll.ask.cancel')}
          </button>
          <button
            type="button"
            className={`${uiStyles.btn} ${uiStyles.primary}`}
            // 预览没读到（读取中/失败）也不给点：什么都看不到却执行全量还原，
            // 等于拿着一句「读不出来」去动全系统 Hosts。失败原因摆在框里，读了再点。
            disabled={preview === null || nothingToRestore}
            title={nothingToRestore
              ? t('hosts.restoreCategory.nothingToRestore')
              : previewError
                ? t('detail.detectBox.previewFailed', { reason: previewError })
                : undefined}
            onClick={() => { void runRestoreAll() }}
          >
            {t('queue.restoreAll.ask.action')}
          </button>
        </>
      }
    >
      <p className={tableStyles.lead}>{t('queue.restoreAll.confirm')}</p>
      <RestorePreviewTable
        rows={previewRows}
        groups={groups}
        loading={preview === null && previewError === ''}
        error={previewError}
      />
    </Dialog>
  ) : null

  return { busy, restoreAll, restoreAllNow: runRestoreAll, confirmDialog }
}
