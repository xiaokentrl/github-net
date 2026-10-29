import { useCallback } from 'react'
import type { Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import { cancelTask, clearFinishedTasks, clearQueue, getTasks, pauseQueue, restoreAllHosts, restoreItems, retryFailedTasks, resumeQueue } from '../../api/client'
import { getErrorMessage } from '../../api/errors'
import { t } from '../../i18n'
import { applyTaskSnapshot, markTaskLoadError, useTaskQueue } from './taskStore'
import styles from './TaskPanel.module.css'

// 任务状态到文案键的映射。九档状态来自后端 TaskMeta，界面按状态字符串显示，不自己推断。
const STATUS_KEY: Record<string, string> = {
  waiting: 'queue.status.waiting',
  running: 'queue.status.running',
  paused: 'queue.status.paused',
  done: 'queue.status.done',
  failed: 'queue.status.failed',
  cancelled: 'queue.status.cancelled',
  skipped: 'queue.status.skipped',
  rolledback: 'queue.status.rolledback',
  restoring: 'queue.status.restoring',
  restored: 'queue.status.restored',
}

function statusKey(task: Task): string {
  // 任务被暂停时，等待中的条目按「已暂停」显示；其余状态按自己的档位显示。
  if (task.status === 'waiting') return 'queue.status.waiting'
  return STATUS_KEY[task.status] ?? 'queue.status.unknown'
}

// 队列面板：全应用唯一的任务列表。切页面不丢进度——状态住 taskStore，不在这里。
// 操作失败显示在面板内（一句话 + 重试走下一次点击），不弹 Toast，也不静默吞掉。
// height 来自配置里的队列高度（LayoutPane 可调；null 用默认）。
export function TaskPanel({ height }: { height: number | null }) {
  const state = useTaskQueue()

  const handleError = useCallback((err: unknown) => {
    // 命令失败时重新拉一次快照，让面板回到后端的真实状态；拉不到就标错误。
    getTasks()
      .then(applyTaskSnapshot)
      .catch(() => markTaskLoadError())
    console.error('[queue]', getErrorMessage(err))
  }, [])

  const onCancel = useCallback(
    (id: string) => {
      cancelTask(id).then(applyTaskSnapshot).catch(handleError)
    },
    [handleError],
  )

  const onPause = useCallback(() => {
    pauseQueue().then(applyTaskSnapshot).catch(handleError)
  }, [handleError])

  const onResume = useCallback(() => {
    resumeQueue().then(applyTaskSnapshot).catch(handleError)
  }, [handleError])

  const onClearFinished = useCallback(() => {
    clearFinishedTasks().then(applyTaskSnapshot).catch(handleError)
  }, [handleError])

  const onClearAll = useCallback(() => {
    clearQueue().then(applyTaskSnapshot).catch(handleError)
  }, [handleError])

  const onRetryLoad = useCallback(() => {
    getTasks().then(applyTaskSnapshot).catch(markTaskLoadError)
  }, [])

  // 重试一条失败/已回滚的应用任务：后端建 retryOf 指回原任务的新任务，原记录保持终态。
  const onRetry = useCallback(
    (id: string) => {
      retryFailedTasks([id])
        .then(() => getTasks().then(applyTaskSnapshot))
        .catch(handleError)
    },
    [handleError],
  )

  const onRetryFailed = useCallback(() => {
    const failed = state.tasks.filter((task) => task.kind === 'apply' && (task.status === 'failed' || task.status === 'rolledback'))
    if (failed.length === 0) return
    retryFailedTasks(failed.map((task) => task.id))
      .then(() => getTasks().then(applyTaskSnapshot))
      .catch(handleError)
  }, [state.tasks, handleError])

  // 行内恢复：把这条应用任务的项目退回应用前状态。
  const onRestoreRow = useCallback(
    (itemId: string) => {
      restoreItems([itemId]).catch(handleError)
    },
    [handleError],
  )

  // 取消全部等待中的任务（原型「取消等待」的形状）；执行中的不受影响。
  const onCancelWaiting = useCallback(() => {
    const waiting = state.tasks.filter((task) => task.status === 'waiting')
    Promise.all(waiting.map((task) => cancelTask(task.id).catch(() => undefined)))
      .then(() => getTasks().then(applyTaskSnapshot))
      .catch(handleError)
  }, [state.tasks, handleError])

  // 全部还原：要求队列空闲（原型语义）；成功后清空队列记录并刷新 Hosts 状态。
  const onRestoreAll = useCallback(() => {
    const busy = state.tasks.some((task) => task.status === 'running' || task.status === 'waiting' || task.status === 'restoring')
    if (busy) return
    if (!window.confirm(t('queue.restoreAll.confirm'))) return
    restoreAllHosts()
      .then((count) => {
        if (count > 0) return clearQueue().catch(() => undefined)
        return undefined
      })
      .then(() => getTasks().then(applyTaskSnapshot))
      .catch(handleError)
  }, [state.tasks, handleError])

  const activeCount = state.tasks.filter((task) => task.status === 'running' || task.status === 'waiting').length

  return (
    <section className={styles.panel} style={height !== null ? { maxHeight: height } : undefined} aria-label={t('queue.title')}>
      <header className={styles.head}>
        <h2 className={styles.title}>{t('queue.title')}</h2>
        <span className={styles.summary}>
          {t('queue.summary', { active: activeCount, total: state.tasks.length })}
          {state.paused ? ` · ${t('queue.pausedBadge')}` : ''}
        </span>
        <span className={styles.spacer} />
        {state.paused ? (
          <button type="button" onClick={onResume}>
            {t('queue.resume')}
          </button>
        ) : (
          <button type="button" onClick={onPause} disabled={!state.loaded || state.loadError}>
            {t('queue.pause')}
          </button>
        )}
        <button type="button" onClick={onCancelWaiting} disabled={!state.loaded || state.loadError || !state.tasks.some((task) => task.status === 'waiting')}>
          {t('queue.cancelWaiting')}
        </button>
        <button
          type="button"
          onClick={onRetryFailed}
          disabled={!state.loaded || state.loadError || !state.tasks.some((task) => task.kind === 'apply' && (task.status === 'failed' || task.status === 'rolledback'))}
        >
          {t('queue.retryFailed')}
        </button>
        <button type="button" onClick={onClearFinished} disabled={!state.loaded || state.loadError}>
          {t('queue.clearFinished')}
        </button>
        <button type="button" onClick={onClearAll} disabled={!state.loaded || state.loadError || activeCount > 0}>
          {t('queue.clearAll')}
        </button>
        <button type="button" onClick={onRestoreAll} disabled={!state.loaded || state.loadError || activeCount > 0}>
          {t('queue.restoreAll')}
        </button>
      </header>

      {state.loadError ? (
        <p className={styles.error}>
          {t('queue.loadError')}
          <button type="button" onClick={onRetryLoad}>
            {t('common.retry')}
          </button>
        </p>
      ) : state.tasks.length === 0 ? (
        <p className={styles.empty}>{state.loaded ? t('queue.empty') : t('detail.loading')}</p>
      ) : (
        <ul className={styles.list}>
          {state.tasks.map((task) => (
            <li key={task.id} className={styles.row}>
              <span className={`${styles.badge} ${styles[task.status] ?? ''}`}>{t(statusKey(task))}</span>
              <span className={styles.name} title={task.name}>
                {task.name}
              </span>
              <span className={styles.progress}>
                {task.status === 'running' || task.status === 'restoring'
                  ? t('queue.progress.phase', { text: task.phaseText ?? '' })
                  : task.duration
                    ? t('queue.progress.duration', { duration: task.duration })
                    : ''}
              </span>
              {task.errorMsg ? (
                <span className={styles.errorText} title={task.errorMsg}>
                  {task.errorMsg}
                </span>
              ) : null}
              {(task.status === 'waiting' || task.status === 'running') && (
                <button type="button" onClick={() => onCancel(task.id)}>
                  {t('queue.cancel')}
                </button>
              )}
              {task.kind === 'apply' && (task.status === 'failed' || task.status === 'rolledback') && (
                <button type="button" onClick={() => onRetry(task.id)}>
                  {t('queue.retry')}
                </button>
              )}
              {task.kind === 'apply' && task.status === 'done' && (
                <button type="button" onClick={() => onRestoreRow(task.itemId)}>
                  {t('queue.restoreRow')}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
