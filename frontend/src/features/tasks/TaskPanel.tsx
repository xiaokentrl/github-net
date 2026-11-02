import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { HostEntryState, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import { cancelTask, clearFinishedTasks, clearQueue, getTasks, pauseQueue, retryFailedTasks, resumeQueue } from '../../api/client'
import { getErrorMessage } from '../../api/errors'
import { useBarResize } from '../../components/layout/useBarResize'
import { t } from '../../i18n'
import { clampQueueHeight, QUEUE_HEIGHT_DEFAULT, QUEUE_HEIGHT_MIN } from './queueHeight'
import { clampQueueListWidth, QUEUE_LIST_DEFAULT_WIDTH } from './queueListWidth'
import { applyTaskSnapshot, markTaskLoadError, useTaskQueue } from './taskStore'
import { useRestoreAllTasks } from './useRestoreAll'
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

// 明细第二列「当前阶段 / 结果」的文字。以前落定的行只剩一个色点和一个「—」，
// 用户看不出这一项到底做成了什么，所以逐档给一句实话：
// 写了几条、还差几条、还是没写；检测给「几可用」；排队和取消也照实说。
// 数字来源是后端这次读回来的逐条 Hosts 事实（HostsState.entries），界面不自己数区段。
function stageResultOf(task: Task, entries: HostEntryState[], detect: { hosts: number; available: number } | null): string {
  if (task.status === 'running' || task.status === 'restoring') return task.phaseText || t('queue.detail.running')
  if (task.status === 'failed' || task.status === 'rolledback' || task.status === 'skipped') {
    return task.errorMsg || t(statusKeyOf(task.status))
  }
  if (task.status === 'waiting') return t('queue.status.waiting')
  if (task.status === 'cancelled') return t('queue.status.cancelled')
  const written = entries.filter((e) => e.written).length
  if (task.status === 'done') {
    if (task.kind === 'detect') {
      return detect === null ? t('queue.status.done') : t('queue.detail.detected', { hosts: detect.hosts, available: detect.available })
    }
    // 应用成功：说清这个项目的哪几条现在写在 Hosts 上（covered/total 一物二用，partial 一眼能看出）。
    return entries.length === 0 ? t('queue.status.done') : t('queue.detail.applied', { covered: written, total: entries.length })
  }
  if (task.status === 'restored') {
    return entries.length === 0 ? t('queue.status.restored') : t('queue.detail.restored', { total: entries.length })
  }
  return '—'
}


// 队列面板：双栏——左「任务列表」（轮，对应原型包裹任务），右「任务明细」（轮下的项目任务，
// 三列：名称 / 当前阶段结果 / 进度耗时）。切页面不丢进度——状态住 taskStore，不在这里。
// height 来自配置里的队列高度（布局屏预设和面板上方那条横向手柄都写这一个字段；null 用默认）。
// 收起是界面自己的事：原型也没把它落进配置，重开窗口还是展开的。
// listWidth + onListWidthChange：两栏之间那条分隔条可拖，宽度落在配置里（原型同一条规则）。
// activeItemId + onPickItem 承担双向联动：点明细行 → 主详情显示该子菜单的候选 IP；
// 主详情/左树选中子菜单 → 明细行高亮并翻到所在页。
// onHostsChanged：面板里的「全部还原」改的是系统 Hosts，改完要让主界面重读一遍目录状态。
// hostsOf：后端每次 GetItems 现读的 Hosts 事实（含逐条主机名写到哪个地址）。
// 明细那一列要说「写了几条、哪几条」，这份数字只能从这儿来，界面不自己数区段。
export function TaskPanel({
  height,
  onHeightChange,
  listWidth,
  onListWidthChange,
  activeItemId,
  onPickItem,
  onHostsChanged,
  hostsOf,
}: {
  height: number | null
  onHeightChange: (height: number) => void
  listWidth: number | null
  onListWidthChange: (width: number) => void
  activeItemId: string
  onPickItem: (itemId: string) => void
  onHostsChanged: () => void
  hostsOf: Map<string, HostsState>
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
  // 工具栏那颗「恢复」和这里共用同一条实现（useRestoreAll），规则只写一遍。
  const { restoreAll: onRestoreAll } = useRestoreAllTasks(handleError, onHostsChanged)

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
  // 收起/展开只藏正文（原型 .queue.collapsed），也不落配置——重开窗口还是展开的。
  const [collapsed, setCollapsed] = useState(false)
  // 逐条替换默认就铺开：应用一轮跑完，每一项的每条域名写到了哪个 IP（或没写上）要一眼看到，
  // 不该一行一行点开才知道。▸/▾ 留着给手动收起；切轮时重置，上一轮收起的那几条不带到这一轮。
  const [collapsedRows, setCollapsedRows] = useState<ReadonlySet<string>>(new Set())
  const toggleRow = useCallback((id: string) => {
    setCollapsedRows((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])
  useEffect(() => setCollapsedRows(new Set()), [selectedRoundId])
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
  }, [selectedRoundId, collapsed])
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

  // 两栏之间的分隔条：拖动期间只有本地预览，松手提交一次给配置（和侧栏同一套实现）。
  // 上限当场算——按队列区实测宽度取一份比例，右边明细必须留过半才看得清三列。
  const clampList = useCallback((value: number) => clampQueueListWidth(value, bodyRef.current?.clientWidth ?? 0), [])
  const listResize = useBarResize(listWidth, onListWidthChange, clampList, QUEUE_LIST_DEFAULT_WIDTH)
  const shownListWidth = listResize.liveValue ?? listWidth

  // 详情与队列之间那条横向手柄：拖的是队列高度，时序和两条竖栏共用同一份实现。
  // 方向取反——手柄往上拖队列才变高；天花板按窗口高度现算（原型 calcQueueMaxH）。
  const clampHeight = useCallback((value: number) => clampQueueHeight(value, window.innerHeight), [])
  const heightResize = useBarResize(height, onHeightChange, clampHeight, QUEUE_HEIGHT_DEFAULT, { axis: 'y', invert: true })
  // 配置还没读回来那一下也用默认高度顶着，不留「按内容撑开」这一种第三态。
  const shownHeight = heightResize.liveValue ?? height ?? QUEUE_HEIGHT_DEFAULT


  // 窗口变窄或变矮后，配置里的宽度、高度可能已经超过当前天花板：
  // CSS 只是压着显示，配置里仍留着超界值，布局屏会显示一个界面拿不到的数字。
  // 这里在尺寸稳定 300ms 后把合法值写回去；和后端一致时 App 那层直接吞掉，不会白涨 revision。
  // 拖动期间与收起状态都不插手：松手那一下提交的本就是夹好的值，再叠一条写盘会打架。
  const draggingList = listResize.dragging
  const draggingHeight = heightResize.dragging
  useEffect(() => {
    if (draggingList || draggingHeight || collapsed) return
    let timer = 0
    const heal = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        const bodyWidth = bodyRef.current?.clientWidth ?? 0
        if (listWidth !== null && bodyWidth > 0) {
          const healed = clampQueueListWidth(listWidth, bodyWidth)
          if (healed !== listWidth) onListWidthChange(healed)
        }
        if (height !== null) {
          const healedHeight = clampQueueHeight(height, window.innerHeight)
          if (healedHeight !== height) onHeightChange(healedHeight)
        }
      }, 300)
    }
    heal()
    window.addEventListener('resize', heal)
    return () => {
      window.clearTimeout(timer)
      window.removeEventListener('resize', heal)
    }
  }, [collapsed, draggingList, draggingHeight, listWidth, height, onListWidthChange, onHeightChange])

  const pickItem = useCallback(
    (itemId: string) => {
      if (itemId) onPickItem(itemId)
    },
    [onPickItem],
  )

  const activeCount = state.tasks.filter((task) => task.status === 'running' || task.status === 'waiting').length

  return (
    <>
      {/* 手柄在面板外面，是详情与队列之间的兄弟节点——原型 .right 里就是这个顺序（第 840 行）。
          收起时跟着藏起来（原型 toggleQueueCollapsed 里 display:none），不然拖着改一个看不见的高度。 */}
      {!collapsed && height !== null && (
        <div
          className={`${styles.rowResizer} ${heightResize.dragging ? styles.rowResizerDragging : ''}`}
          role="separator"
          aria-orientation="horizontal"
          title={t('queue.heightResizer.title')}
          onPointerDown={heightResize.onPointerDown}
          onPointerMove={heightResize.onPointerMove}
          onPointerUp={heightResize.onPointerUp}
          onPointerCancel={heightResize.onPointerUp}
        />
      )}
      <section
        ref={heightResize.targetRef}
        className={`${styles.panel} ${collapsed ? styles.panelCollapsed : ''}`}
        style={{ height: `${shownHeight}px`, minHeight: `${QUEUE_HEIGHT_MIN}px` }}
        aria-label={t('queue.title')}
      >
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
          {/* 收起/展开：原型队列头最后那颗 toggle-btn。只改界面，不写配置。 */}
          <button
            type="button"
            className={styles.toggleBtn}
            aria-expanded={!collapsed}
            title={collapsed ? t('queue.expand') : t('queue.collapse')}
            aria-label={collapsed ? t('queue.expand') : t('queue.collapse')}
            onClick={() => setCollapsed((value) => !value)}
          >
            {collapsed ? '▴' : '▾'}
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
            <aside className={styles.roundsPane} ref={listResize.targetRef} style={shownListWidth !== null ? { width: shownListWidth } : undefined}>
              <div className={styles.paneHead}>{t('queue.roundsTitle')}</div>
              {/* 空队列留白（拍板 2026-10-31）：不放提示文字。 */}
              {roundRows.length === 0 ? null : (
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
            <div
              className={`${styles.colResizer} ${listResize.dragging ? styles.colResizerDragging : ''}`}
              role="separator"
              aria-orientation="vertical"
              title={t('layout.queueListResizer.title')}
              onPointerDown={listResize.onPointerDown}
              onPointerMove={listResize.onPointerMove}
              onPointerUp={listResize.onPointerUp}
              onPointerCancel={listResize.onPointerUp}
            />
            <section className={styles.detailPane}>
              {selected === null ? null : (
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
                      const entries = (task.itemId && hostsOf.get(task.itemId)?.entries) || []
                      const result = task.itemId ? state.results[task.itemId] : undefined
                      const stageResult = stageResultOf(
                        task,
                        entries,
                        result ? { hosts: (result.hosts ?? []).length, available: result.available } : null,
                      )
                      const prog = task.progress > 0 || task.status === 'running'
                        ? `${task.progress}%${task.duration ? ' · ' + task.duration : ''}`
                        : task.duration || '—'
                      // 只有应用/还原这两种任务有「逐条替换」可看；检测的逐条在详情页那张候选表里。
                      const expandable = entries.length > 0 && (task.kind === 'apply' || task.kind === 'restore')
                      const open = !collapsedRows.has(task.id)
                      return (
                        <Fragment key={task.id}>
                          <tr
                            className={`${task.id === kids[focus]?.id ? styles.focus : ''} ${task.itemId && task.itemId === activeItemId ? styles.sel : ''}`}
                            onClick={() => pickItem(task.itemId)}
                          >
                            <td>
                              {expandable ? (
                                <button
                                  type="button"
                                  className={styles.expandBtn}
                                  aria-expanded={open}
                                  aria-label={open ? t('queue.detail.collapseEntries') : t('queue.detail.expandEntries')}
                                  title={open ? t('queue.detail.collapseEntries') : t('queue.detail.expandEntries')}
                                  onClick={(event) => {
                                    event.stopPropagation()
                                    toggleRow(task.id)
                                  }}
                                >
                                  {open ? '▾' : '▸'}
                                </button>
                              ) : (
                                <span className={styles.expandSlot} aria-hidden="true" />
                              )}
                              <span className={`${styles.dot} ${styles['dot-' + task.status]}`} />
                              <span title={task.name}>{task.name}</span>
                            </td>
                            <td className={task.status === 'failed' ? styles.err : ''}>{stageResult}</td>
                            <td className={styles.dim}>{prog}</td>
                          </tr>
                          {expandable && open ? (
                            <tr className={styles.entryRow}>
                              <td colSpan={4}>
                                <ul className={styles.entryList}>
                                  {entries.map((entry) => (
                                    <li key={entry.hostname} className={styles.entryItem}>
                                      <span className={styles.entryHost}>{entry.hostname}</span>
                                      <span className={`${styles.entryIp} ${entry.written ? '' : styles.entryMissing}`}>
                                        {entry.written ? entry.ip : t('queue.detail.entryNotWritten')}
                                      </span>
                                    </li>
                                  ))}
                                </ul>
                              </td>
                            </tr>
                          ) : null}
                        </Fragment>
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
    </>
  )
}
