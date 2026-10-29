import { useCallback } from 'react'
import type { Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import { cancelTask, clearFinishedTasks, clearQueue, getTasks, pauseQueue, resumeQueue } from '../../api/client'
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
export function TaskPanel() {
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

  const activeCount = state.tasks.filter((task) => task.status === 'running' || task.status === 'waiting').length

  return (
    <section className={styles.panel} aria-label={t('queue.title')}>
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
        <button type="button" onClick={onClearFinished} disabled={!state.loaded || state.loadError}>
          {t('queue.clearFinished')}
        </button>
        <button type="button" onClick={onClearAll} disabled={!state.loaded || state.loadError || activeCount > 0}>
          {t('queue.clearAll')}
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
                {task.status === 'running'
                  ? t('queue.progress.running', { percent: task.progress, text: task.phaseText ?? '' })
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
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
