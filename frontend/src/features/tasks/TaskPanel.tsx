import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import { cancelTask, clearFinishedTasks, clearQueue, getTasks, pauseQueue, restoreAllHosts, retryFailedTasks, resumeQueue } from '../../api/client'
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

// 轮次没有自己的状态字段：由轮下的任务推出来，圆点和文案用同一份。
const NODE_TERMINAL = new Set(['done', 'failed', 'skipped', 'cancelled', 'rolledback', 'restored'])
function statusKeyOf(status: string): string {
  return STATUS_KEY[status] ?? 'queue.status.unknown'
}
// 焦点 = 第一个未落定的子项；全部落定跟随最后一行。
function qdFocusIndex(kids: Task[]): number {
  const i = kids.findIndex((k) => k.status === 'waiting' || k.status === 'running' || k.status === 'restoring')
  return i >= 0 ? i : Math.max(0, kids.length - 1)
}

// 队列面板：双栏——左「任务列表」（轮，对应原型包裹任务），右「任务明细」（轮下的项目任务，
// 三列：名称 / 当前阶段结果 / 进度耗时）。切页面不丢进度——状态住 taskStore，不在这里。
// height 来自配置里的队列高度（LayoutPane 可调；null 用默认）。
// activeItemId + onPickItem 承担双向联动：点明细行 → 主详情显示该子菜单的候选 IP；
// 主详情/左树选中子菜单 → 明细行高亮并翻到所在页。
export function TaskPanel({
  height,
  activeItemId,
  onPickItem,
}: {
  height: number | null
  activeItemId: string
  onPickItem: (itemId: string) => void
}) {
  const state = useTaskQueue()

  const handleError = useCallback((err: unknown) => {
    // 命令失败时重新拉一次快照，让面板回到后端的真实状态；拉不到就标错误。
    getTasks()
      .then(applyTaskSnapshot)
      .catch(() => markTaskLoadError())
    console.error('[queue]', getErrorMessage(err))
  }, [])

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

  const onRetryFailed = useCallback(() => {
    const failed = state.tasks.filter((task) => task.kind === 'apply' && (task.status === 'failed' || task.status === 'rolledback'))
    if (failed.length === 0) return
    retryFailedTasks(failed.map((task) => task.id))
      .then(() => getTasks().then(applyTaskSnapshot))
      .catch(handleError)
  }, [state.tasks, handleError])

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

  // ===== 双栏状态 =====
  // 轮次状态由它的任务推出来：有在跑的=执行中，全排队=排队中，有失败=失败，其余=完成。
  const roundRows = useMemo(() => {
    return state.rounds.map((round) => {
      const kids = state.tasks.filter((task) => task.roundId === round.id)
      let status: string = 'queued'
      if (kids.some((k) => k.status === 'running' || k.status === 'restoring')) status = 'running'
      else if (kids.length > 0 && kids.every((k) => NODE_TERMINAL.has(k.status))) {
        status = kids.some((k) => k.status === 'failed' || k.status === 'rolledback') ? 'failed' : 'done'
      }
      const done = kids.filter((k) => NODE_TERMINAL.has(k.status)).length
      return { round, kids, status, done, total: kids.length }
    })
  }, [state.rounds, state.tasks])

  // 焦点跟随：有在跑的轮就自动选它；用户点过的轮保持，直到那一轮跑完、新一轮开跑。
  const [selectedRoundId, setSelectedRoundId] = useState('')
  const manualRef = useRef(false)
  const runningRound = roundRows.find((r) => r.status === 'running' || r.status === 'restoring')
  useEffect(() => {
    if (manualRef.current) {
      const stillThere = roundRows.some((r) => r.round.id === selectedRoundId)
      if (stillThere) return
      manualRef.current = false
    }
    if (runningRound && selectedRoundId !== runningRound.round.id) setSelectedRoundId(runningRound.round.id)
    else if (!runningRound && !selectedRoundId && roundRows.length) setSelectedRoundId(roundRows[roundRows.length - 1].round.id)
  }, [roundRows, runningRound, selectedRoundId])

  const selected = roundRows.find((r) => r.round.id === selectedRoundId) ?? roundRows.find((r) => r.round.id === (runningRound || roundRows[roundRows.length - 1])?.round.id) ?? null
  const kids = selected ? selected.kids : []
  const focus = qdFocusIndex(kids)

  // 翻页：每页行数按明细面板高度现算；焦点跨页自动跟随，箭头手动翻后保持。
  const bodyRef = useRef<HTMLDivElement | null>(null)
  const [perPage, setPerPage] = useState(6)
  const [page, setPage] = useState(0)
  useEffect(() => {
    const measure = () => {
      const h = bodyRef.current?.clientHeight ?? 0
      setPerPage(Math.max(3, Math.min(14, Math.floor((h - 40) / 30))))
    }
    measure()
    window.addEventListener('resize', measure)
    return () => window.removeEventListener('resize', measure)
  }, [selectedRoundId])
  const pages = Math.max(1, Math.ceil(kids.length / perPage))
  const focusPage = Math.floor(focus / perPage)
  useEffect(() => {
    if (focusPage !== page) {
      setPage(focusPage)
    }
  }, [focusPage, page])
  const safePage = Math.min(page, pages - 1)
  const sliceStart = safePage * perPage
  const slice = kids.slice(sliceStart, sliceStart + perPage)

  const pickItem = useCallback(
    (itemId: string) => {
      if (itemId) onPickItem(itemId)
    },
    [onPickItem],
  )

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
      ) : (
        <div className={styles.body} ref={bodyRef}>
          <aside className={styles.roundsPane}>
            <div className={styles.paneHead}>{t('queue.roundsTitle')}</div>
            {roundRows.length === 0 ? (
              <div className={styles.paneEmpty}>{state.loaded ? t('queue.empty') : t('detail.loading')}</div>
            ) : (
              roundRows.map((r) => (
                <div
                  key={r.round.id}
                  className={`${styles.qrow} ${selected?.round.id === r.round.id ? styles.qrowSel : ''}`}
                  onClick={() => {
                    manualRef.current = true
                    setSelectedRoundId(r.round.id)
                                }}
                  title={`${r.round.name} · ${t(statusKeyOf(r.status))}`}
                >
                  <span className={`${styles.dot} ${styles['dot-' + r.status]}`} />
                  <span className={styles.qrowName} title={r.round.name}>
                    {r.round.name}
                  </span>
                  <span className={styles.qrowCount}>
                    {r.done}/{r.total}
                  </span>
                </div>
              ))
            )}
          </aside>
          <section className={styles.detailPane}>
            {selected === null ? (
              <div className={styles.paneEmpty}>{t('queue.detail.pick')}</div>
            ) : (
              <table className={styles.qtable2}>
                <thead>
                  <tr>
                    <th>{t('queue.detail.name')}</th>
                    <th>{t('queue.detail.stage')}</th>
                    <th>{t('queue.detail.progress')}</th>
                    <th className={styles.qdCtl}>
                      <span className={styles.qdPage}>
                        {pages > 1 ? `${safePage + 1}/${pages} 页` : ''}
                      </span>
                      <button type="button" className={styles.qdArrow} id="qdPrev" title="上一页" disabled={safePage <= 0} onClick={() => setPage(Math.max(0, safePage - 1))}>
                        ↑
                      </button>
                      <button type="button" className={styles.qdArrow} id="qdNext" title="下一页" disabled={safePage >= pages - 1} onClick={() => setPage(Math.min(pages - 1, safePage + 1))}>
                        ↓
                      </button>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {slice.map((task) => {
                    const stageResult = task.status === 'running' || task.status === 'restoring'
                      ? task.phaseText || t('queue.detail.running')
                      : task.status === 'failed' || task.status === 'rolledback'
                        ? task.errorMsg || t('queue.status.failed')
                        : task.status === 'skipped'
                          ? task.errorMsg || t('queue.status.skipped')
                          : '—'
                    const prog = task.progress > 0 || task.status === 'running'
                      ? `${task.progress}%${task.duration ? ' · ' + task.duration : ''}`
                      : task.duration || '—'
                    return (
                      <tr
                        key={task.id}
                        className={`${task.id === kids[focus]?.id ? styles.focus : ''} ${task.itemId && task.itemId === activeItemId ? styles.sel : ''}`}
                        onClick={() => pickItem(task.itemId)}
                      >
                        <td>
                          <span className={`${styles.dot} ${styles['dot-' + task.status]}`} />
                          <span title={task.name}>{task.name}</span>
                        </td>
                        <td className={task.status === 'failed' ? styles.err : ''}>{stageResult}</td>
                        <td className={styles.dim}>{prog}</td>
                      </tr>
                    )
                  })}
                  {slice.length === 0 && (
                    <tr>
                      <td colSpan={4} className={styles.dim}>
                        {t('queue.detail.none')}
                      </td>
                    </tr>
                  )}
                </tbody>
              </table>
            )}
          </section>
        </div>
      )}
    </section>
  )
}
