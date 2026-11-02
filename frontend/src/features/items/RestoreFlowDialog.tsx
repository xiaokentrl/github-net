import { useTaskQueue } from '../tasks/taskStore'
import { isTerminalTask } from '../tasks/taskStore'
import type { Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import { t } from '../../i18n'
import { formatInteger } from '../../i18n/formatters'
import styles from './ItemDetail.module.css'

export type RestoreFlow = { itemId: string; mode: 'choose' | 'detect' | 'apply' }

// 「还原此项」弹框（拍板 2026-11-03）：纯展示 + 回调——
//   choose 模式：三选一（检测此还原项 / 应用此还原项 / 放弃还原），选了通知父组件
//   detect/apply 模式：三行阶段状态（还原/检测/应用），全部从队列任务现推
// 动作的触发（还原入队、检测/应用的跟进）都在父组件；关掉弹框不取消任务。
export function RestoreFlowDialog({
  flow,
  itemName,
  onChooseDetect,
  onChooseApply,
  onClose,
}: {
  flow: RestoreFlow
  itemName: string
  onChooseDetect: () => void
  onChooseApply: () => void
  onClose: () => void
}) {
  const state = useTaskQueue()
  const flowTasks = state.tasks.filter((tk: Task) => tk.itemId === flow.itemId)
  const restoreTask = flowTasks.find((tk: Task) => tk.kind === 'restore')
  const detectTask = flowTasks.find((tk: Task) => tk.kind === 'detect')
  const applyTask = flowTasks.find((tk: Task) => tk.kind === 'apply')
  const live = state.live[flow.itemId]
  const result = state.results[flow.itemId]

  const stageText = (tk: Task | undefined): string => {
    if (!tk) return '—'
    if (tk.status === 'waiting') return t('detail.flow.waiting')
    if (tk.status === 'running' || tk.status === 'restoring') {
      if (tk.kind === 'detect' && live) {
        return `${t('detail.flow.running')}（${formatInteger(live.done)}/${formatInteger(live.total)}）`
      }
      return t('detail.flow.running')
    }
    if (tk.status === 'done' || tk.status === 'restored') return t('detail.flow.done')
    if (tk.status === 'skipped' || tk.status === 'failed' || tk.status === 'rolledback') {
      return `${t('detail.flow.failed')}（${tk.errorMsg || t('error.unknown')}）`
    }
    if (tk.status === 'cancelled') return t('detail.flow.cancelled')
    return '—'
  }

  const allSettled = flowTasks.length > 0 && flowTasks.every((tk: Task) => isTerminalTask(tk.status))
  const detectAvailable = result?.available ?? null

  return (
    <div
      className={styles.flowMask}
      onClick={(event) => {
        if (event.target === event.currentTarget) onClose()
      }}
    >
      <section className={styles.flowPanel} role="dialog" aria-modal="true" aria-label={t('detail.flow.title')}>
        <h3 className={styles.flowTitle}>{t('detail.flow.title')}</h3>
        <p className={styles.flowDesc}>{t('detail.flow.desc', { name: itemName })}</p>

        {flow.mode === 'choose' ? (
          <div className={styles.flowOptions}>
            <button type="button" className={styles.flowOption} onClick={onChooseDetect}>
              {t('detail.flow.detect')}
              <span className={styles.flowOptionHint}>{t('detail.flow.detectHint')}</span>
            </button>
            <button type="button" className={styles.flowOption} onClick={onChooseApply}>
              {t('detail.flow.apply')}
              <span className={styles.flowOptionHint}>{t('detail.flow.applyHint')}</span>
            </button>
            <button type="button" className={styles.flowOption} onClick={onClose}>
              {t('detail.flow.abandon')}
              <span className={styles.flowOptionHint}>{t('detail.flow.abandonHint')}</span>
            </button>
          </div>
        ) : (
          <div className={styles.flowStages}>
            <div className={styles.flowStageLine}>
              <span className={styles.flowStageName}>{t('detail.flow.stageRestore')}</span>
              <span>{stageText(restoreTask)}</span>
            </div>
            {flow.mode === 'detect' || detectTask ? (
              <div className={styles.flowStageLine}>
                <span className={styles.flowStageName}>{t('detail.flow.stageDetect')}</span>
                <span>
                  {stageText(detectTask)}
                  {detectTask && detectTask.status === 'done' && detectAvailable !== null
                    ? ` · ${t('detail.flow.detectDone', { count: detectAvailable })}`
                    : ''}
                </span>
              </div>
            ) : null}
            {flow.mode === 'apply' || applyTask ? (
              <div className={styles.flowStageLine}>
                <span className={styles.flowStageName}>{t('detail.flow.stageApply')}</span>
                <span>{stageText(applyTask)}</span>
              </div>
            ) : null}
            {allSettled ? (
              <button type="button" className={styles.flowClose} onClick={onClose}>
                {t('detail.flow.close')}
              </button>
            ) : null}
          </div>
        )}
      </section>
    </div>
  )
}
