import { Fragment, useCallback, useEffect, useMemo, useRef, useState, type MutableRefObject } from 'react'
import type { DetectResultView, HostEntryState, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Task } from '../../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import { clearFinishedTasks, getTasks } from '../../api/client'
import { getErrorMessage } from '../../api/errors'
import { pickBestCandidate } from '../detect/candidateChoice'
import { useBarResize } from '../../components/layout/useBarResize'
import { t } from '../../i18n'
import { formatDateTime } from '../../i18n/formatters'
import { clampQueueHeight, QUEUE_HEIGHT_DEFAULT, QUEUE_HEIGHT_MIN } from './queueHeight'
import { clampQueueListWidth, QUEUE_LIST_DEFAULT_WIDTH } from './queueListWidth'
import { applyTaskSnapshot, markTaskLoadError, useTaskQueue } from './taskStore'
import { projectState, type Tone } from './statusTone'
import styles from './TaskPanel.module.css'

// 档位 → 队列明细行底色类。八档齐全由 Record<Tone, string> 在编译时保证；
// 「什么状态算什么档」统一在 statusTone.ts，这里只管挂哪个类。
const TASK_ROW_CLASS: Record<Tone, string> = {
  working: styles.qWorking,
  failed: styles.qFailed,
  written: styles.qWritten,
  unwritten: styles.qUnwritten,
}

// 任务状态到文案键的映射。八档状态来自后端 TaskMeta，界面按状态字符串显示，不自己推断。
const STATUS_KEY: Record<string, string> = {
  waiting: 'queue.status.waiting',
  running: 'queue.status.running',
  queued: 'queue.status.waiting',
  done: 'queue.status.done',
  failed: 'queue.status.failed',
  skipped: 'queue.status.skipped',
  rolledback: 'queue.status.rolledback',
  restoring: 'queue.status.restoring',
  restored: 'queue.status.restored',
}

// 轮次没有自己的状态字段：由轮下的任务推出来，圆点和文案用同一份。
const NODE_TERMINAL = new Set(['done', 'failed', 'skipped', 'rolledback', 'restored'])
function statusKeyOf(status: string): string {
  return STATUS_KEY[status] ?? 'queue.status.unknown'
}
// 焦点 = 第一个未落定的子项；全部落定跟随最后一行。
function qdFocusIndex(kids: Task[]): number {
  const i = kids.findIndex((k) => k.status === 'waiting' || k.status === 'running' || k.status === 'restoring')
  return i >= 0 ? i : Math.max(0, kids.length - 1)
}

// 明细第二列「当前阶段 / 结果」的文字，只给还没摊平成逐条域名的那些行用：
// 排队说排队、跑起来说当前阶段、失败和跳过说原因。落定（完成／已还原）的行摊成一行一条域名，
// 每条域名自己说「写上了没有、地址是什么」，这里就不再给一句汇总。
function stageResultOf(task: Task): string {
  if (task.status === 'running' || task.status === 'restoring') return task.phaseText || t('queue.detail.running')
  if (task.status === 'failed' || task.status === 'rolledback' || task.status === 'skipped') {
    return task.errorMsg || t(statusKeyOf(task.status))
  }
  if (task.status === 'waiting') return t('queue.status.waiting')
  if (task.status === 'restored') return t('queue.status.restored')
  return t('queue.status.done')
}

// 摊平之后一行就是「一条域名」，没有下面那层点开才铺的子表了。每行带三个事实：
//   state：这一行的下落（已写入／未写入／可用／没有可用地址／解析失败／判不出归属／已还原／还写着）
//   ip ＋ latencyMs ＋ measuredAt：这一行现在的地址、连上要多久、那次实测是什么时候，
//     没有就是空串和 0。耗时和定格时间都只认这一条地址自己的实测（后端逐条带的候选池定格）。
//   tone：这一行自己的底色档——一个项目写了一半时，写上的绿、没写上的灰，一眼看出差哪条
// 选址算法和详情页同一份（pickBestCandidate），两边出现两个地址就是错的。
// 应用／还原的地址来自后端这次读回来的逐条 Hosts 事实；整段判不出归属时后端给空数组，
// 退回目录里这个项目的域名清单，状态写「判不出归属」，不自己编 IP。
type EntryLine = { hostname: string; state: string; ip: string; reason: string; latencyMs: number; measuredAt: string; tone: Tone }

// 一条地址的耗时和通不通，只认它自己那一次实测：后端逐条带的定格（候选池里同名同址那一行）优先，
// 内存这一轮里同一颗地址的候选只作补充。同域名另一个地址的数不能顶上来——那是编。
// 定格说这颗地址不通就不给毫秒数：给一个连不通的地址摆上延迟，等于替它做保证。
type HostRow = NonNullable<DetectResultView['hosts']>[number]
function probeOf(entry: HostEntryState, host: HostRow | undefined): { ms: number; unreachable: boolean } {
  if (entry.probeStatus) {
    const ok = entry.probeStatus === 'available'
    return { ms: ok ? entry.latencyMs ?? 0 : 0, unreachable: !ok }
  }
  const best = host ? pickBestCandidate(host.candidates ?? []) : null
  if (best && best.ip === entry.ip) return { ms: best.latencyMs > 0 ? best.latencyMs : 0, unreachable: false }
  return { ms: 0, unreachable: false }
}

function entryLinesOf(
  kind: string,
  entries: HostEntryState[],
  hostnames: string[],
  result: DetectResultView | undefined,
): EntryLine[] {
  const hostByName = new Map((result?.hosts ?? []).map((h) => [h.hostname, h]))
  if (kind === 'detect') {
    return (result?.hosts ?? []).map((host) => {
      const best = pickBestCandidate(host.candidates ?? [])
      if (best) return { hostname: host.hostname, state: t('queue.detail.rowUsable'), ip: best.ip, reason: '', latencyMs: best.latencyMs > 0 ? best.latencyMs : 0, measuredAt: '', tone: 'written' }
      if (!host.resolved) return { hostname: host.hostname, state: t('queue.detail.resolveFailed'), ip: '', reason: '', latencyMs: 0, measuredAt: '', tone: 'failed' }
      return { hostname: host.hostname, state: t('queue.detail.noUsableAddress'), ip: '', reason: '', latencyMs: 0, measuredAt: '', tone: 'unwritten' }
    })
  }
  if (entries.length > 0) {
    // 没写上的行也照实带上区段里现在写着的地址（含没有认领者的旧行）：「当前写的是什么」和
    // 「是不是我们写的」是两件事，界面只说后一件就把前一件藏掉了。
    // 为什么没写上先取这一条地址自己的定格（连不通），再退回这一轮整域的结论
    // （解析失败／没有可用地址），都没有才只说下落。
    return entries.map((entry) => {
      const host = hostByName.get(entry.hostname)
      const probe = entry.ip ? probeOf(entry, host) : { ms: 0, unreachable: false }
      const ms = probe.ms
      // 这一行说过实测（不管通不通）就把定格时间留着：鼠标停上去能看见这句结论是什么时候测的。
      const at = entry.measuredAt ?? ''
      if (entry.written && entry.ip) {
        // 区段里确实写着这一条，状态词照说「已写入」；但它自己最后一次实测连不通，
        // 底色就不能涂成功那一档——那一绿是在替它保证「这条在加速」，而我们已经知道它不通。
        const reason = probe.unreachable ? t('queue.detail.rowUnreachable') : ''
        const tone: Tone = probe.unreachable ? 'failed' : kind === 'restore' ? 'failed' : 'written'
        return { hostname: entry.hostname, state: kind === 'restore' ? t('queue.detail.rowNotRestored') : t('queue.detail.rowWritten'), ip: entry.ip, reason, latencyMs: ms, measuredAt: at, tone }
      }
      if (kind === 'restore') {
        return { hostname: entry.hostname, state: t('queue.detail.rowRestored'), ip: entry.ip ?? '', reason: probe.unreachable ? t('queue.detail.rowUnreachable') : '', latencyMs: ms, measuredAt: at, tone: 'unwritten' }
      }
      let reason = ''
      let tone: Tone = 'unwritten'
      if (probe.unreachable) reason = t('queue.detail.rowUnreachable')
      else if (host && host.resolved === false) {
        reason = t('queue.detail.resolveFailed')
        tone = 'failed'
      } else if (host && !pickBestCandidate(host.candidates ?? [])) reason = t('queue.detail.noUsableAddress')
      return { hostname: entry.hostname, state: t('queue.detail.entryNotWritten'), ip: entry.ip ?? '', reason, latencyMs: ms, measuredAt: at, tone }
    })
  }
  return hostnames.map((hostname) => ({ hostname, state: t('queue.detail.entryUnowned'), ip: '', reason: '', latencyMs: 0, measuredAt: '', tone: 'unwritten' }))
}


// 队列面板：双栏——左「任务列表」（轮，对应原型包裹任务），右「任务明细」。
// 明细三列：名称 / 当前阶段 · 结果 / 进度 · 耗时。落定（完成／已还原）的项目摊成一行一条域名，
// 名称那一格重复项目名，第二列说这一条的下落和地址，第三列给「第几条／共几条；连上要多久」。
// 排队、跑着、失败、跳过仍是一行一个项目——那几档要说的是项目级别的一句话（阶段名或失败原因）。
// 切页面不丢进度——状态住 taskStore，不在这里。
// height 来自配置里的队列高度（布局屏预设和面板上方那条横向手柄都写这一个字段；null 用默认）。
// 收起是界面自己的事：原型也没把它落进配置，重开窗口还是展开的。
// listWidth + onListWidthChange：两栏之间那条分隔条可拖，宽度落在配置里（原型同一条规则）。
// activeItemId + onPickItem 承担双向联动：点明细行 → 主详情显示该子菜单的候选 IP；
// 主详情/左树选中子菜单 → 明细那几行一起高亮，焦点行滚进视野。
// hostsOf：后端每次 GetItems 现读的 Hosts 事实（含逐条主机名写到哪个地址）。
// 明细那一列要说「写了几条、哪几条」，这份数字只能从这儿来，界面不自己数区段。
// hostnamesOf：目录里每个项目的域名清单，只在整段判不出归属（后端逐条给空数组）时用来逐条列域名。
export function TaskPanel({
  height,
  heightLocked,
  lockSaveError,
  onToggleHeightLock,
  onHeightChange,
  expandRef,
  listWidth,
  onListWidthChange,
  activeItemId,
  onPickItem,
  hostsOf,
  hostnamesOf,
}: {
  height: number | null
  // 高度锁：锁住期间不写配置，磁盘上的高度就停在锁定那一刻，重开仍是它。
  heightLocked: boolean
  lockSaveError: string
  // 切换锁定要顺手报当前面板高度：锁上那一刻的高度就是重启后要恢复的高度。
  onToggleHeightLock: (locked: boolean, height: number) => void
  onHeightChange: (height: number) => void
  // 外部（检测选中项/写入 Hosts/还原 Hosts/选择清理项这些按钮）要动队列时拉一下：收起态自动展开。
  expandRef?: MutableRefObject<(() => void) | null>
  listWidth: number | null
  onListWidthChange: (width: number) => void
  activeItemId: string
  onPickItem: (itemId: string) => void
  hostsOf: Map<string, HostsState>
  hostnamesOf: Map<string, string[]>
}) {
  const state = useTaskQueue()

  const handleError = useCallback((err: unknown) => {
    // 命令失败时重新拉一次快照，让面板回到后端的真实状态；拉不到就标错误。
    getTasks()
      .then(applyTaskSnapshot)
      .catch(() => markTaskLoadError())
    console.error('[queue]', getErrorMessage(err))
  }, [])

  const onClearFinished = useCallback(() => {
    clearFinishedTasks().then(applyTaskSnapshot).catch(handleError)
  }, [handleError])

  const onRetryLoad = useCallback(() => {
    getTasks().then(applyTaskSnapshot).catch(markTaskLoadError)
  }, [])

  // ===== 双栏状态 =====
  // 轮次状态由它的任务推出来：有在跑的=执行中，全排队=排队中，有失败=失败，其余=完成。
  // 显示顺序后进在前（拍板 2026-11-03）：最新一轮在最上面，老轮往下沉。
  const roundRows = useMemo(() => {
    return [...state.rounds].reverse().map((round) => {
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
    else if (!runningRound && !selectedRoundId && roundRows.length) setSelectedRoundId(roundRows[0].round.id)
  }, [roundRows, runningRound, selectedRoundId])

  const selected = roundRows.find((r) => r.round.id === selectedRoundId) ?? roundRows.find((r) => r.round.id === (runningRound || roundRows[0])?.round.id) ?? null
  const kids = selected ? selected.kids : []
  const focus = qdFocusIndex(kids)

  // 落定（完成／已还原）的任务摊成「一行一条域名」，不再有点开才铺的那层子表：
  // 应用一轮跑完，每条域名写到了哪个 IP（或为什么没写上）要一眼看到。
  // 逐条先按整轮算一遍再画，检测、应用、还原三类共用同一份口径。
  const lineRows = useMemo(
    () =>
      kids.map((task) => {
        const entries = (task.itemId && hostsOf.get(task.itemId)?.entries) || []
        const hostnames = (task.itemId && hostnamesOf.get(task.itemId)) || []
        const result = task.itemId ? state.results[task.itemId] : undefined
        return entryLinesOf(task.kind, entries, hostnames, result)
      }),
    [kids, hostsOf, hostnamesOf, state.results],
  )

  const bodyRef = useRef<HTMLDivElement | null>(null)

  // 焦点行滚进视野（拍板 2026-11-03：明细不再翻页，全部行直接渲染、容器自己滚；
  // 之前按面板高度断页，量高失准的时候一页只剩一行，六项要看五页）。
  const focusTaskId = kids[focus]?.id
  useEffect(() => {
    if (!focusTaskId) return
    document.querySelector(`[data-task-id="${focusTaskId}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [focusTaskId, kids, selectedRoundId])

  // 两栏之间的分隔条：拖动期间只有本地预览，松手提交一次给配置（和侧栏同一套实现）。
  // 上限当场算——按队列区实测宽度取一份比例，右边明细必须留过半才看得清三列。
  const clampList = useCallback((value: number) => clampQueueListWidth(value, bodyRef.current?.clientWidth ?? 0), [])
  const listResize = useBarResize(listWidth, onListWidthChange, clampList, QUEUE_LIST_DEFAULT_WIDTH)
  const shownListWidth = listResize.liveValue ?? listWidth

  // 详情与队列之间那条横向手柄：拖的是队列高度，时序和两条竖栏共用同一份实现。
  // 方向取反——手柄往上拖队列才变高；天花板按窗口高度现算（原型 calcQueueMaxH）。
  const clampHeight = useCallback((value: number) => clampQueueHeight(value, window.innerHeight), [])
  const heightResize = useBarResize(height, onHeightChange, clampHeight, QUEUE_HEIGHT_DEFAULT, { axis: 'y', invert: true, disabled: heightLocked })
  // 配置还没读回来那一下也用默认高度顶着，不留「按内容撑开」这一种第三态。
  // 锁上之后这一格永远取配置里那份高度：检测、应用、还原、清理触发的展开，以及收起/展开按钮，
  // 都不再改它；只有解除锁定后才重新接受拖动、预设和折叠。
  // 唯一例外是窗口自己缩到装不下时显示按窗口上限收敛一下，磁盘上的锁定值仍不动。
  const baseHeight = heightLocked
    ? (height ?? QUEUE_HEIGHT_DEFAULT)
    : (heightResize.liveValue ?? height ?? QUEUE_HEIGHT_DEFAULT)
  const shownHeight = clampQueueHeight(baseHeight, window.innerHeight)

  // 收起不再是独立的开关布尔（拍板 2026-11-03 重写）：高度压到 45——正好一行工具条——就是收起。
  // 拖动条、小箭头、配置值改的是同一个数，三条路永远一致；旧实现两边各说各话，箭头和拖动会互相打架。
  const queueCollapsed = shownHeight <= QUEUE_HEIGHT_MIN
  // 展开时记住当时多高，箭头展开回这份高度；重开窗口从默认来。
  const expandedHeightRef = useRef(QUEUE_HEIGHT_DEFAULT)
  // 锁住时任何改高度的请求都在这里止步：既不写配置，也不改本地显示。
  const applyHeight = useCallback(
    (next: number) => {
      if (heightLocked) return
      onHeightChange(next)
    },
    [heightLocked, onHeightChange],
  )
  const toggleQueue = useCallback(() => {
    if (heightLocked) return
    if (queueCollapsed) applyHeight(expandedHeightRef.current)
    else {
      expandedHeightRef.current = shownHeight
      applyHeight(QUEUE_HEIGHT_MIN)
    }
  }, [heightLocked, queueCollapsed, shownHeight, applyHeight])
  // 收起态下的展开动作交给外部按钮拉一把：展开到记住的那份高度。锁定时一律不展开。
  const expandQueue = useCallback(() => {
    if (heightLocked || !queueCollapsed) return
    applyHeight(expandedHeightRef.current)
  }, [heightLocked, queueCollapsed, applyHeight])
  if (expandRef) expandRef.current = expandQueue

  // 窗口变窄或变矮后，配置里的宽度、高度可能已经超过当前天花板：
  // CSS 只是压着显示，配置里仍留着超界值，布局屏会显示一个界面拿不到的数字。
  // 这里在尺寸稳定 300ms 后把合法值写回去；和后端一致时 App 那层直接吞掉，不会白涨 revision。
  // 拖动期间不插手：松手那一下提交的本就是夹好的值，再叠一条写盘会打架。
  const draggingList = listResize.dragging
  const draggingHeight = heightResize.dragging
  useEffect(() => {
    if (draggingList || draggingHeight) return
    let timer = 0
    const heal = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        const bodyWidth = bodyRef.current?.clientWidth ?? 0
        if (listWidth !== null && bodyWidth > 0) {
          const healed = clampQueueListWidth(listWidth, bodyWidth)
          if (healed !== listWidth) onListWidthChange(healed)
        }
        // 锁住时跳过这一步：显示已经夹过，磁盘上的高度要一直停在锁定那一刻。
        if (height !== null && !heightLocked) {
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
  }, [queueCollapsed, draggingList, draggingHeight, listWidth, height, heightLocked, onListWidthChange, onHeightChange])

  const pickItem = useCallback(
    (itemId: string) => {
      if (itemId) onPickItem(itemId)
    },
    [onPickItem],
  )

  const activeCount = state.tasks.filter((task) => task.status === 'running' || task.status === 'waiting').length
  // 锁上时收起/展开按钮整颗禁用，悬停改说原因；没锁时按当前是不是收起态给原来的文案。
  const collapseLabel = heightLocked
    ? t('queue.heightLock.collapseDisabled')
    : queueCollapsed ? t('queue.expand') : t('queue.collapse')

  return (
    <>
      {/* 手柄在面板外面，是详情与队列之间的兄弟节点——原型 .right 里就是这个顺序（第 840 行）。
          收起时也在（拍板 2026-11-02）：收起那条跟着拖出的高度变厚，所以不能再把它藏掉。 */}
      {height !== null && (
        <div
          className={`${styles.rowResizer} ${heightResize.dragging ? styles.rowResizerDragging : ''} ${heightLocked ? styles.rowResizerLocked : ''}`.trim()}
          role="separator"
          aria-orientation="horizontal"
          aria-disabled={heightLocked}
          title={heightLocked ? t('queue.heightResizer.lockedTitle') : t('queue.heightResizer.title')}
          onPointerDown={heightResize.onPointerDown}
          onPointerMove={heightResize.onPointerMove}
          onPointerUp={heightResize.onPointerUp}
          onPointerCancel={heightResize.onPointerUp}
        />
      )}
      <section
        ref={heightResize.targetRef}
        className={`${styles.panel} ${queueCollapsed ? styles.panelCollapsed : ''}`}
        style={{ height: `${shownHeight}px`, minHeight: `${QUEUE_HEIGHT_MIN}px` }}
        aria-label={t('queue.title')}
      >
        <header className={styles.head}>
          <h2 className={styles.title}>{t('queue.title')}</h2>
          <span className={styles.summary}>{t('queue.summary', { active: activeCount, total: state.tasks.length })}</span>
          <span className={styles.spacer} />
          {/* 队列高度锁：名字把「锁什么」写全，锁上后主色底；鼠标停上去说清锁定期间和重启后的行为。 */}
          <button
            type="button"
            className={`${styles.heightLockBtn} ${heightLocked ? styles.heightLockBtnLocked : ''}`.trim()}
            aria-pressed={heightLocked}
            title={heightLocked ? t('queue.heightLock.lockedTip', { height: Math.round(shownHeight) }) : t('queue.heightLock.unlockedTip')}
            aria-label={heightLocked ? t('queue.heightLock.unlock') : t('queue.heightLock.lock')}
            onClick={() => onToggleHeightLock(!heightLocked, Math.round(shownHeight))}
          >
            {heightLocked ? t('queue.heightLock.unlock') : t('queue.heightLock.lock')}
          </button>
          {lockSaveError ? <span className={styles.lockError} title={lockSaveError}>{lockSaveError}</span> : null}
          <button type="button" onClick={onClearFinished} disabled={!state.loaded || state.loadError}>
            {t('queue.clearFinished')}
          </button>
          {/* 收起/展开：原型队列头最后那颗 toggle-btn。它会改高度，锁定时整颗禁用，避免点了没反应。 */}
          <button
            type="button"
            className={styles.toggleBtn}
            aria-expanded={!queueCollapsed}
            disabled={heightLocked}
            title={collapseLabel}
            aria-label={collapseLabel}
            onClick={toggleQueue}
          >
            {queueCollapsed ? '▴' : '▾'}
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
                    </tr>
                  </thead>
                  <tbody>
                    {kids.map((task, i) => {
                      const result = task.itemId ? state.results[task.itemId] : undefined
                      const tone = projectState(hostsOf.get(task.itemId ?? ''), state.tasks, task.itemId ?? '', result).tone
                      const focused = task.id === kids[focus]?.id
                      const isSel = Boolean(task.itemId && task.itemId === activeItemId)
                      const rowClass = (rowTone: Tone, bar: boolean) =>
                        [TASK_ROW_CLASS[rowTone], bar && focused ? styles.focus : '', isSel ? styles.sel : ''].filter(Boolean).join(' ') || undefined
                      const dot = <span className={`${styles.dot} ${styles['dot-' + task.status]}`} />
                      // 落定（完成／已还原）且有逐条域名 → 一行一条域名，底色跟这一行自己的下落。
                      // 排队、跑着、失败、跳过仍是一行一个项目：那几档要说的是项目级别的一句实话
                      // （当前阶段或失败原因），拆到域名上反而把这句话弄丢了。
                      const lines = task.status === 'done' || task.status === 'restored' ? lineRows[i] ?? [] : []
                      if (lines.length > 0) {
                        return (
                          <Fragment key={task.id}>
                            {lines.map((line, idx) => (
                              <tr
                                key={line.hostname}
                                data-task-id={task.id}
                                className={rowClass(line.tone, idx === 0)}
                                onClick={() => pickItem(task.itemId)}
                              >
                                <td>
                                  {dot}
                                  <span title={task.name}>{task.name}</span>
                                </td>
                                <td>
                                  {t('queue.detail.rowEntry', { state: line.state, index: idx + 1, total: lines.length })}
                                  <span className={styles.dim}>；</span>
                                  <span title={line.hostname}>{line.hostname}</span>
                                  {/* 地址和原因各说各的：写着的那颗地址也可能连不通，两句话要同排，
                                      不能因为带了地址就把「这条地址连不通」藏掉。 */}
                                  {line.ip ? <span className={styles.rowIp}> {line.ip}</span> : null}
                                  {line.reason ? <span className={styles.rowReason}> {line.reason}</span> : null}
                                </td>
                                <td className={styles.dim}>
                                  <span title={line.measuredAt ? t('queue.detail.measuredAt', { at: formatDateTime(line.measuredAt) }) : undefined}>
                                    {idx + 1}/{lines.length}；{line.latencyMs ? `${line.latencyMs}ms` : '—'}
                                  </span>
                                </td>
                              </tr>
                            ))}
                          </Fragment>
                        )
                      }
                      const prog =
                        task.progress > 0 || task.status === 'running'
                          ? `${task.progress}%${task.duration ? ' · ' + task.duration : ''}`
                          : task.duration || '—'
                      return (
                        <tr key={task.id} data-task-id={task.id} className={rowClass(tone, true)} onClick={() => pickItem(task.itemId)}>
                          <td>
                            {dot}
                            <span title={task.name}>{task.name}</span>
                          </td>
                          <td className={task.status === 'failed' ? styles.err : ''}>{stageResultOf(task)}</td>
                          <td className={styles.dim}>{prog}</td>
                        </tr>
                      )
                    })}
                    {kids.length === 0 && (
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
