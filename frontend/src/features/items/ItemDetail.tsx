import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Candidate } from '../../../bindings/github.com/xiaokentrl/github-net/internal/accelerator/models'
import { formatInteger, formatRelativeTime } from '../../i18n/formatters'
import type { CatalogCategory, CatalogItem, DetectResultView, HostsRestorePreview, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { getErrorMessage } from '../../api/errors'
import { previewRestoreHostsItems, type NetworkProfile } from '../../api/client'
import { arrowKeys } from '../../hooks/arrowKeys'
import { Dialog } from '../../components/ui/Dialog'
import { EmptyState } from '../../components/ui/EmptyState'
import { StatusBadge } from '../../components/ui/StatusBadge'
import uiStyles from '../../components/ui/ui.module.css'
import { pickBestCandidate } from '../detect/candidateChoice'
import { DetectTables } from './DetectTables'
import { RestorePreviewTable } from './RestorePreviewTable'
import tableStyles from './RestorePreviewTable.module.css'
import { ServiceStrategySelector } from '../strategies/ServiceStrategySelector'
import { ensureLastResults, isTerminalTask, useTaskQueue } from '../tasks/taskStore'
import { invalidate } from '../../sync/syncHub'
import { addressUsable, projectState, toneForCandidate, toneForItem, type Tone } from '../tasks/statusTone'
import styles from './ItemDetail.module.css'
import detectStyles from './DetectResult.module.css'

// 候选状态的文案键。八档来自后端 CandidateStatus，界面按字符串显示，不自己推断成败。
const CANDIDATE_KEY: Record<string, string> = {
  queued: 'detect.candidate.queued',
  probing: 'detect.candidate.probing',
  available: 'detect.candidate.available',
  unavailable: 'detect.candidate.unavailable',
  timeout: 'detect.candidate.timeout',
  cancelled: 'detect.candidate.cancelled',
  notStarted: 'detect.candidate.notStarted',
  untested: 'detect.candidate.untested',
}

// 四档 → 本表的行底色类。Record<Tone, string> 保证四档都有落点。
const TONE_ROW_CLASS: Record<Tone, string> = {
  working: styles.rowWorking,
  failed: styles.rowFailed,
  written: styles.rowWritten,
  unwritten: styles.rowUnwritten,
}

// 候选表也用同一套四档颜色，明细文案仍说「可用/超时/没测上」。
const CANDIDATE_ROW_CLASS: Record<Tone, string> = {
  working: detectStyles.rowWorking,
  failed: detectStyles.rowFailed,
  written: detectStyles.rowWritten,
  unwritten: detectStyles.rowUnwritten,
}

const CANDIDATE_TAG_CLASS: Record<Tone, string> = {
  working: detectStyles.tagBusy,
  failed: detectStyles.tagFail,
  written: detectStyles.tagOk,
  unwritten: '',
}

// 全二级菜单表的表头，顺序和渲染一一对应；拖拽分隔线只在前四列的表头上。
const ITEM_TABLE_HEADERS = ['detail.table.item', 'detail.metric.appliedIp', 'detail.metric.bestIp', 'detail.metric.available', 'detail.metric.lastCheck']

// 后端标了「当前写入」的那几条，按域名摊成一行一条：域名取这一行候选自己的主机名，
// 地址取候选自己的 IP。同一域名同一地址只记一次（IPv4／IPv6 可能各被标一条）。
// 详情里每一行都来自后端 GetItems / GetDetectResult / 事件：没有的字段就说没有，不补演示数据。
// 六格指标全都有来源（拍板 2026-11-02）：写入地址和本轮最优都取候选自己标的字段，
// 耗时和可用数是候选现算，方法名与上次检测时间取结果字段。拿不到值就显示「—」，不摆恒为占位符的列。
export function ItemDetail({
  item,
  category,
  hosts,
  hostsOf,
  onSelectItem,
  detectError,
  hostsError,
  onDetectAvailability,
  onDetectRun,
  onApply,
  onRestore,
  onRestoreCategory,
  onStrategySaved,
}: {
  item: CatalogItem | null
  category: CatalogCategory | null
  hosts: HostsState | undefined
  hostsOf: Map<string, HostsState>
  onSelectItem: (itemId: string) => void
  detectError: string
  hostsError: string
  onDetectAvailability: () => void
  onDetectRun: (itemId: string, strategyId: string, profile: NetworkProfile) => void
  onApply: (itemId: string) => void
  onRestore: (itemId: string) => void
  onRestoreCategory: (itemIds: string[]) => void
  onStrategySaved: () => void
}) {
  const state = useTaskQueue()
  // 本项目是否正在检测；钩子要在早退之前挂，这里先算出来。
  const detecting = state.tasks.some(
    (task) => item !== null && task.itemId === item.id && task.kind === 'detect' && (task.status === 'running' || task.status === 'waiting'),
  )

  // 刚进详情页或换分类：把后端还留着的上一轮结论补读进来。
  // 刷新页面后 store 是空的，列表那三格（检测最优 IP／可用候选／上次检测）会全成「—」，
  // 但后端记得——不补读就是界面自己装看不见。这一步只读，不排任务、不动队列。
  const categoryItemKey = (category?.items ?? []).map((it) => it.id).join(',')
  useEffect(() => {
    ensureLastResults(categoryItemKey === '' ? [] : categoryItemKey.split(','))
  }, [categoryItemKey])

  // 列宽：拖过一次就转固定布局（px），没拖过是 null（auto 自适应）。
  const tableRef = useRef<HTMLTableElement | null>(null)
  const [colWidths, setColWidths] = useState<number[] | null>(null)
  const selfDetectable = item !== null && !item.userSupplied && (item.hostnames?.length ?? 0) > 0
  // 还原本菜单的公共弹框（拍板 2026-11-03）：确认文案在框里，动作走 onRestoreCategory。
  const [restoreMenuOpen, setRestoreMenuOpen] = useState(false)
  // 可还原的二级菜单：各自的 Hosts 状态是已应用或部分生效（拍板 2026-11-03）。
  // 放在早退之前，因为弹框的前后对照要按这批 ID 拉「一起还原」的联合预览。
  const restorableItems = (category?.items ?? []).filter((it) => {
    const st = hostsOf.get(it.id)?.status ?? HostsStatus.HostsUnknown
    return st === HostsStatus.HostsApplied || st === HostsStatus.HostsPartial
  })
  const menuIdsKey = restorableItems.map((it) => it.id).join('|')
  // 弹框对照表的分组列：每个可还原菜单一格，域名归属按目录 + Hosts 实写条目算；
  // 归属不上的行在表里落「—」组，不硬塞给某个菜单。
  const menuGroups = restorableItems.map((it) => ({
    name: it.name,
    hostnames: Array.from(new Set([
      ...(it.hostnames ?? []),
      ...(hostsOf.get(it.id)?.entries ?? []).map((e) => e.hostname),
    ])),
  }))
  const [menuPreview, setMenuPreview] = useState<HostsRestorePreview | null>(null)
  const [menuPreviewError, setMenuPreviewError] = useState('')
  useEffect(() => {
    if (!restoreMenuOpen || !menuIdsKey) return
    let live = true
    const ids = menuIdsKey.split('|')
    setMenuPreview(null)
    setMenuPreviewError('')
    previewRestoreHostsItems(ids)
      .then((res) => {
        if (!live) return
        // 联合预览最多释放本菜单这 N 个项目；更多说明对端还是旧版特权服务，
        // 把 itemIDs 忽略了、回了全量结果，拿它展示会把结论说错。
        if (res.items > ids.length) {
          setMenuPreview(null)
          setMenuPreviewError(t('detail.detectBox.previewStaleHelper'))
          return
        }
        setMenuPreview(res)
      })
      .catch((err: unknown) => { if (live) setMenuPreviewError(getErrorMessage(err)) })
    return () => { live = false }
  }, [restoreMenuOpen, menuIdsKey])
  // 行级「还原此项」公共弹框（拍板 2026-11-03，已简化）：点行内「还原」直接进检测那一屏，
  // 框只剩两屏——detect 看数据、restoring 给回执；「立刻还原」要看得见这一项的检测数据才可点。
  // writtenCount 是点开那一刻数出来的待还原条数——还原落定后 entries 会被刷新成空，
  // 回执要是跟着现数就会写成「已还原 0 条」。
  const [restoreBox, setRestoreBox] = useState<{ itemId: string; name: string; writtenCount: number; view: 'restore' | 'detect' } | null>(null)
  // 候选 IP 表的本地焦点行：这张表没有「选中某个项目」的语义，只记上下移动的位置。
  const [candFocus, setCandFocus] = useState(0)
  // 这个框是几点几分打开的：用来分清「本框点重新检测跑出来的结论」和「后端留着的上一轮结论」。
  const boxOpenedAt = useRef(0)
  // 框要跟的是「这一项、这一类最近的一条任务」。任务数组只往后追加，所以取筛完的最后一条；
  // 用 find 拿第一条会盯住上一次跑完的那条，框就卡在旧状态上。
  const lastBoxTask = (kind: 'detect' | 'restore') =>
    restoreBox ? state.tasks.filter((tk) => tk.itemId === restoreBox.itemId && tk.kind === kind).pop() : undefined
  const boxDetectTask = lastBoxTask('detect')
  const boxRestoreTask = lastBoxTask('restore')
  const boxRunning = (task: typeof boxDetectTask) => task?.status === 'waiting' || task?.status === 'running'
  const boxDetectRunning = boxRunning(boxDetectTask)
  const boxRestoreRunning = boxRunning(boxRestoreTask)
  // 写入 Hosts 的任务在跑时，第二张表的「写入可用IP」要跟着灰掉，不能重复发。
  const boxApplyRunning = restoreBox
    ? state.tasks.some((tk) => tk.itemId === restoreBox.itemId && tk.kind === 'apply' && (tk.status === 'waiting' || tk.status === 'running'))
    : false
  // 还原落定分两态：成功（restored）→「立刻还原」禁用（已还原无可再还原）；
  // 失败/已回滚 → 允许原位重试，按钮不锁死（审计修复：原来任何终态都禁，失败后无法重试）。
  const boxRestoreSettledOk = boxRestoreTask !== undefined && !boxRestoreRunning && boxRestoreTask.status === 'restored'
  const boxRestoreFailed = boxRestoreTask !== undefined && !boxRestoreRunning && (boxRestoreTask.status === 'failed' || boxRestoreTask.status === 'rolledback')
  const boxHosts = restoreBox ? hostsOf.get(restoreBox.itemId) : undefined
  // 检测弹框两张表都只列这一个二级菜单的真实数据：域名以目录为准，逐条状态取后端读回来的 Hosts。
  const boxItem = restoreBox ? (category?.items ?? []).find((it) => it.id === restoreBox.itemId) : undefined
  const boxAllEntries = boxHosts?.entries ?? []
  // 目录里的域名为准，再把 Hosts 中实际出现、目录没列的域名补上（用户自填项就是这种）。
  const boxHostnames = Array.from(new Set([
    ...(boxItem?.hostnames ?? []),
    ...boxAllEntries.map((entry) => entry.hostname),
  ]))
  // 这一项的任务状态一变就当刷新信号：检测/还原/写入落定后，缓存表跟着重拉。
  const boxRefreshKey = restoreBox
    ? state.tasks
        .filter((tk) => tk.itemId === restoreBox.itemId)
        .map((tk) => `${tk.id}:${tk.status}`)
        .join('|')
    : ''
  const boxLive = restoreBox ? state.live[restoreBox.itemId] : undefined
  // 框里就看 store 那一份：它有两路喂——进详情页时的补读（后端留着的上一轮），和检测事件。
  // 检测跑动中这一项会被清空，正好不拿旧数据糊人；列表和框读同一份，两边不会各说一套。
  const boxResult = restoreBox ? state.results[restoreBox.itemId] : undefined
  // 速率与进度有两份来源，按「拿得到就优先细的」走：
  //   候选级事件 state.live 给了 done/total → 能算「约 N 项/秒」；
  //   单条检测这类链路只有任务级事件（phaseText + progress 百分比）→ 退回显示百分比和已用秒。
  // 两份都不编造：拿不到数字就只说状态，不写 0/0 假装知道。
  const boxElapsedSec = boxDetectTask?.startedAt ? (Date.now() - new Date(boxDetectTask.startedAt).getTime()) / 1000 : null
  const boxRate = boxDetectRunning && boxLive && boxLive.done > 0 && boxElapsedSec !== null && boxElapsedSec > 0.4
    ? boxLive.done / boxElapsedSec
    : null
  // 检测落定 = 有这条任务、不在跑、且已经是终态。
  const boxDetectSettled = boxDetectTask !== undefined && !boxDetectRunning && isTerminalTask(boxDetectTask.status)
  // 框里摊得开检测数据吗（有域名行）。摊得开才允许点「立刻还原」——跑检测时数据被清空，那颗自然跟着灰。
  const boxHasData = (boxResult?.hosts?.length ?? 0) > 0
  const boxConfirmReady = boxHasData

  // 点行内「还原」＝只开框，不排任务：用户没点「立刻还原」之前，列表和队列一格都不该被这一步改动。
  // 框里要看的检测数据在这屏加载时就补读进 store 了（见上面的 ensureLastResults），这里不再单独发请求。
  const openRestoreBox = (itemId: string, name: string, writtenCount: number, view: 'restore' | 'detect') => {
    boxOpenedAt.current = Date.now()
    setRestoreBox({ itemId, name, writtenCount, view })
  }

  // 检测那一段的一行字：本框跑的检测给「进行中／结论／后端原因」；
  // 这一条检测任务是本框打开之后才跑完的吗？是，才可以说「检测完成／检测失败」——
  // 刷新后队列快照里那条历史任务不能被他框的结果冒充本框刚跑的。
  const boxTaskFresh = boxDetectTask?.finishedAt ? new Date(boxDetectTask.finishedAt).getTime() >= boxOpenedAt.current : false
  // 「上一轮」那行：这份结论比这个框还老，就标清是上一轮的。
  const lastRoundText = (view: DetectResultView) =>
    view.finishedAt
      ? t('detail.box.lastRound', {
          available: view.available,
          total: view.hosts?.length ?? 0,
          time: formatRelativeTime(view.finishedAt),
        })
      : t('detail.box.detectDone', { available: view.available, total: view.hosts?.length ?? 0 })
  let boxDetectText = ''
  if (boxDetectRunning) {
    const parts = [boxDetectTask?.phaseText || (boxDetectTask?.status === 'waiting' ? t('detail.box.waiting') : t('detail.box.detecting'))]
    if (boxLive) {
      parts.push(t('detail.box.progress', { done: boxLive.done, total: boxLive.total }))
      if (boxRate !== null) parts.push(t('detail.box.rate', { rate: boxRate.toFixed(1) }))
    } else if (typeof boxDetectTask?.progress === 'number') {
      parts.push(`${boxDetectTask.progress}%`)
      if (boxElapsedSec !== null) parts.push(t('detail.box.elapsed', { sec: boxElapsedSec.toFixed(1) }))
    }
    boxDetectText = parts.join(' · ')
  } else if (boxDetectSettled && boxTaskFresh && boxDetectTask) {
    if (boxDetectTask.errorMsg) {
      boxDetectText = t('detail.box.detectFailed', { reason: boxDetectTask.errorMsg })
    } else if (boxHasData && boxResult) {
      boxDetectText = t('detail.box.detectDone', {
        available: boxResult.available,
        total: boxResult.hosts?.length ?? 0,
      })
    } else {
      boxDetectText = boxDetectTask.phaseText || t('detail.box.detectDonePlain')
    }
  } else if (boxHasData && boxResult) {
    // 本框没跑检测（或跑的那条还没落定）：把补读回来的上一轮结论摊出来。
    boxDetectText = lastRoundText(boxResult)
  }

  // 还原那一段的一行字：进行中、失败原因、已还原几条。落定后不自动关框，回执留在框里由用户自己关。
  let boxRestoreText = ''
  if (boxRestoreTask) {
    if (boxRestoreRunning) {
      boxRestoreText = t('detail.box.restoring')
    } else if (boxRestoreTask.errorMsg) {
      boxRestoreText = t('detail.box.restoreFailed', { reason: boxRestoreTask.errorMsg })
    } else {
      // 已还原就一句「已还原」（拍板 2026-11-03）：条数和耗时是内部细节，不摆给用户。
      boxRestoreText = t('detail.box.restored')
    }
  }
  const colDrag = useRef<{ pointerId: number; index: number; startX: number; startWidths: number[] } | null>(null)

  const startColDrag = (index: number) => (event: ReactPointerEvent<HTMLElement>) => {
    if (event.button !== 0) return
    event.preventDefault()
    event.stopPropagation()
    const row = tableRef.current?.rows[0]
    if (!row) return
    const startWidths = Array.from(row.cells).map((cell) => cell.offsetWidth)
    colDrag.current = { pointerId: event.pointerId, index, startX: event.clientX, startWidths }
    event.currentTarget.setPointerCapture(event.pointerId)
    setColWidths(startWidths)
  }
  const onColDragMove = (event: ReactPointerEvent<HTMLElement>) => {
    const state = colDrag.current
    if (!state || state.pointerId !== event.pointerId) return
    const dx = event.clientX - state.startX
    const MIN = 70
    const next = [...state.startWidths]
    next[state.index] = Math.max(MIN, state.startWidths[state.index] + dx)
    next[state.index + 1] = Math.max(MIN, state.startWidths[state.index + 1] - dx)
    setColWidths(next)
  }
  const onColDragUp = (event: ReactPointerEvent<HTMLElement>) => {
    const state = colDrag.current
    if (!state || state.pointerId !== event.pointerId) return
    colDrag.current = null
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
  }
  if (!item || !category) return <EmptyState message={t('detail.empty')} />

  const result = state.results[item.id]

  const rows = result?.hosts ?? []
  // 当前写入 IP 只有一个来源：后端在这轮候选里标了 applied 的那几条。界面不自己认。
  // 一条域名一行摊全（拍板 2026-11-02）：一个地址被几条域名共用就各写一行，
  // 不再收成「N 个域名各有各的写入」那种要人回头数的说法。
  // 当前写入的取数源是 hosts.entries（后端这次读回来的真值）：内存轮的 applied 标记
  // 重启后就没了，用它会出现「同一页面两个当前写入还互相矛盾」。只列已写入的，
  // 未写入的域名在下面那行域名清单里已经有。

  // 候选表只装当前二级菜单自己的 IP（拍板 2026-10-31）：所有候选共用一张表、域名是一列。
  // 排序先分档再比延迟（拍板 2026-11-02）：可用的一律排在不可用／超时／没测的前面，
  // 档内按延迟升序，没测出延迟（0）的排在该档最后，同档同延迟按原顺序稳定保持。
  // 只按延迟排会出这种结果：一条 30ms 但连不通的排在 90ms 且能用的前面，看着像选错了。
  const flatCandidates = rows
    .flatMap((host) => (host.candidates ?? []).map((candidate) => ({ host, candidate })))
    .map((row, order) => ({ ...row, order }))
    .sort((a, b) => {
      const usableRank = (candidate: Candidate) => (candidate.status === 'available' ? 0 : 1)
      const latencyOf = (candidate: Candidate) => (candidate.latencyMs > 0 ? candidate.latencyMs : Number.MAX_SAFE_INTEGER)
      return usableRank(a.candidate) - usableRank(b.candidate) || latencyOf(a.candidate) - latencyOf(b.candidate) || a.order - b.order
    })
  // 候选表不列已经测出「连不通」的那几条（拍板 2026-11-06 晚：不显示不能加速的候选 IP，
  // 一个项目全都不能加速时这张表就是空的）。还没结论的那几档照列——
  // 排队／在测／没排上测／被取消都不是「不能加速」，把它们抹掉等于替它们说连不通。
  const listedCandidates = flatCandidates.filter((row) => addressUsable(row.candidate.status))
  // 解析失败的域名没有候选行，单独在表上方说一句，不混进表里冒充候选。
  const failedHosts = rows.filter((h) => !h.resolved)

  // 方向键：详情表沿用 App 的选中项，候选表只在这张表里记一个本地焦点行。
  const detailItems = category.items ?? []
  const detailIndex = detailItems.findIndex((it) => it.id === item.id)
  const onDetailArrowKeys = arrowKeys(detailItems.length, detailIndex, (next) => {
    const target = detailItems[next]
    if (target) onSelectItem(target.id)
  })
  // 重新检测后行数可能变少，把旧焦点夹回最后一行，别留下一个指不到行的下标。
  const safeCandFocus = listedCandidates.length === 0 ? -1 : Math.min(candFocus, listedCandidates.length - 1)
  const onCandArrowKeys = arrowKeys(listedCandidates.length, safeCandFocus, setCandFocus)


  return (
    <div className={styles.wrap}>
      {/* 第一张卡：这是哪个服务、用什么方法加速、当前 Hosts 是什么结论。 */}
      <div className={styles.card}>
        <div className={`${styles.cardHead} ${styles.headGrid}`}>
          <h3 className={styles.title}>
            {category.name}
            <span className={styles.crumb}> / {item.name}</span>
          </h3>
          <ServiceStrategySelector
            scopeId={category.scopeId}
            scopeName={category.name}
            onSaved={onStrategySaved}
          />
          <div className={styles.headActions}>
            <StatusBadge state={projectState(hosts, state.tasks, item.id, result)} />
            {/* 检测可用性（拍板 2026-11-12）：用已保存的默认方法，检测本主菜单下全部项目，
                先弹档位弹框选档；一次性动作，不改默认设置。 */}
            <button
              type="button"
              className={`${uiStyles.btn} ${uiStyles.small} ${uiStyles.detect}`}
              title={t('detail.detectAvailability.title')}
              onClick={onDetectAvailability}
            >
              {t('detail.detectAvailability')}
            </button>
            {restorableItems.length > 0 ? (
              <button
                type="button"
                className={`${uiStyles.btn} ${uiStyles.small} ${uiStyles.restore}`}
                disabled={detecting}
                title={t('hosts.restoreCategoryHint', { count: restorableItems.length })}
                onClick={() => setRestoreMenuOpen(true)}
              >
                {t('hosts.restoreCategory')}
              </button>
            ) : null}
          </div>
        </div>

        {/* 全二级菜单表（拍板 2026-11-03）：当前主菜单下每一项一行，数据各从各的来源现取；
            切换二级菜单只换选中行的底色和标识，表本身不动。列宽自适应（auto 布局）。 */}
        <table
          ref={tableRef}
          className={styles.itemTable}
          tabIndex={0}
          onKeyDown={onDetailArrowKeys}
          style={colWidths ? { tableLayout: 'fixed', width: '100%' } : undefined}
        >
          {colWidths ? (
            <colgroup>
              {colWidths.map((width, i) => (
                <col key={i} style={{ width: `${width}px` }} />
              ))}
            </colgroup>
          ) : null}
          <thead>
            <tr>
              {ITEM_TABLE_HEADERS.map((header, i) => (
                <th key={header}>
                  {t(header)}
                  {i < ITEM_TABLE_HEADERS.length - 1 ? (
                    <span
                      className={styles.colGrip}
                      title={t('layout.queueListResizer.title')}
                      onPointerDown={startColDrag(i)}
                      onPointerMove={onColDragMove}
                      onPointerUp={onColDragUp}
                      onPointerCancel={onColDragUp}
                    />
                  ) : null}
                </th>
              ))}
              <th>{t('detail.table.action')}</th>
            </tr>
          </thead>
          <tbody>
            {detailItems.map((it, index) => {
              const hs = hostsOf.get(it.id)
              const res = state.results[it.id]
              // 「可写Hosts」摊成每个域名一行，行首带「已写/未写」短标签（拍板 2026-11-06）：
              // 标签先看在不在区段里（entry.ip），再看这一条自己最后一次实测通不通——
              // 写着但连不通的那条压成「未写」（拍板 2026-11-06 晚），地址照原样摊出来供还原对账，只是不算写成。
              // 认领关系管的是档位和那颗「还原」，两件事不混。没写的行只说「域名 → —」，原因在下面候选表里。
              // 逐条事实后端只在账对得上时才给；整段都不存在时给空数组，可这时文件的事实是确定的（一段都没有），
              // 所以按目录里的域名摊成「未写」。判读不了、还没读过（conflict/unknown）不能反过来说「未写」，那一格仍留 —。
              const entryLines = hs?.entries ?? []
              const lines =
                entryLines.length > 0
                  ? entryLines.map((entry) => {
                      // 标签看两件事：区段里这行有没有地址，以及这一条自己最后一次实测通不通。
                      // 写着但连不通的那条只能算「未写」——那一格说Hosts写成了没有，一条连不通的地址没写成。
                      const hasAddress = Boolean(entry.ip)
                      const dead = hasAddress && !addressUsable(entry.probeStatus)
                      return { hostname: entry.hostname, ip: entry.ip || '', live: hasAddress && !dead, dead }
                    })
                  : hs?.status === HostsStatus.HostsNone
                    ? (it.hostnames ?? []).map((hostname) => ({ hostname, ip: '', live: false, dead: false }))
                    : []
              // 待还原条数另算，只数本项目现在认领的那几条：还原落定的回执要说退了几条，不是文件里有几行。
              const claimed = (hs?.entries ?? []).filter((entry) => entry.written && entry.ip).length
              const cands = (res?.hosts ?? []).flatMap((host) => host.candidates ?? [])
              const best = pickBestCandidate(cands)
              const availableCount = cands.filter((c) => c.status === 'available').length
              const selectedRow = it.id === item.id
              // 行底色跟统一四档走：状态源每次渲染重算，一变整行颜色立刻跟着变。
              // 本轮检测零可用的项目由 projectState 判进失败档，整行跟着红；旧映射失效的退回灰档。
              const rowTone = TONE_ROW_CLASS[toneForItem(hs, state.tasks, it.id, res)]
              const rowClass = [rowTone, selectedRow ? styles.rowSel : ''].filter(Boolean).join(' ')
              return (
                <tr
                  key={it.id}
                  data-arrow-index={index}
                  className={rowClass || undefined}
                  onClick={(event) => {
                    event.currentTarget.closest('table')?.focus()
                    onSelectItem(it.id)
                  }}
                >
                  <td className={styles.itemName}>
                    {selectedRow ? (
                      <span className={styles.rowMark} aria-hidden="true">
                        ▸
                      </span>
                    ) : null}
                    {it.name}
                  </td>
                  <td>
                    {lines.length > 0 ? (
                      <span className={styles.writeStack}>
                        {lines.map((line) => (
                          <span
                            key={line.hostname}
                            className={`${styles.writeItem} ${line.live ? styles.writeItemWritten : ''}`}
                          >
                            <span
                              className={`${styles.writeTag} ${line.live ? styles.writeTagOk : styles.writeTagNo}`}
                              title={line.dead ? t('queue.detail.rowUnreachable') : undefined}
                            >
                              {t(line.live ? 'detail.metric.tagWritten' : 'detail.metric.tagUnwritten')}
                            </span>
                            {/* 区段里写着地址就照原样摊出来（还原要对账这颗地址），标签只说算不算写成。 */}
                            {line.hostname}→{line.ip || '—'}
                          </span>
                        ))}
                      </span>
                    ) : '—'}
                  </td>
                  <td className={`${styles.cellMono} ${best ? styles.hasAvailable : ''}`.trim()}>
                    {best ? (best.latencyMs > 0 ? `${best.ip} · ${best.latencyMs}ms` : best.ip) : '—'}
                  </td>
                  <td className={cands.length > 0 ? (availableCount > 0 ? styles.hasAvailable : styles.noAvailable) : undefined}>
                    {cands.length > 0 ? `${formatInteger(availableCount)}/${formatInteger(cands.length)}` : '—'}
                  </td>
                  <td>{res?.finishedAt ? formatRelativeTime(res.finishedAt) : '—'}</td>
                  <td className={styles.rowAction}>
                    {/* 两颗固定入口（拍板 2026-11-04）：「还原」开还原表（不可还原的行置灰），「检测」开检测与写入表。
                        弹框里先看数据，主按钮在弹框内。 */}
                    <button
                      type="button"
                      className={`${uiStyles.btn} ${uiStyles.small} ${uiStyles.restore}`}
                      disabled={!restorableItems.some((r) => r.id === it.id)}
                      title={t('hosts.restoreRow')}
                      onClick={(event) => {
                        event.stopPropagation()
                        openRestoreBox(it.id, it.name, claimed, 'restore')
                      }}
                    >
                      {t('hosts.restoreRow')}
                    </button>
                    <button
                      type="button"
                      className={`${uiStyles.btn} ${uiStyles.small}`}
                      disabled={!selfDetectable}
                      title={t('hosts.detectEntryHint')}
                      onClick={(event) => {
                        event.stopPropagation()
                        openRestoreBox(it.id, it.name, claimed, 'detect')
                      }}
                    >
                      {t('hosts.detectEntry')}
                    </button>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>

        {/* 域名这一行摊成逐条：每条域名现在写到哪个 IP、还是没写，全用后端这次读回来的事实。
            判不出归属（冲突、读不到）时后端给空数组，退回原来的域名清单，不自己编 IP。 */}

        {hostsError ? (
          <p className={styles.errorBox} role="alert">
            <span className={styles.boxIco} aria-hidden="true">!</span>
            <span>{t('hosts.error', { reason: hostsError })}</span>
          </p>
        ) : null}
        {detectError ? (
          <p className={styles.errorBox} role="alert">
            <span className={styles.boxIco} aria-hidden="true">!</span>
            <span>{t('detect.error', { reason: detectError })}</span>
          </p>
        ) : null}
      </div>

      {/* 第二张卡：这一轮查到的候选。按钮全在原型 .candidate-actions 的位置（第 298 行）。 */}
      <div className={styles.card}>

        {failedHosts.length > 0 ? (
          <p className={styles.hint}>
            {failedHosts.map((host) => (
              <span key={host.hostname} className={detectStyles.blockFailed}>
                {host.hostname}
                {t('detect.resolveFailed', { reason: host.detail ?? '' })}
                {'　'}
              </span>
            ))}
          </p>
        ) : null}
        {/* 表头常驻（拍板 2026-10-31）：还没检测时空一行占位，列的样子先立住。 */}
        <table className={detectStyles.cands} tabIndex={0} onKeyDown={onCandArrowKeys}>
          <thead>
            <tr>
              <th scope="col">{t('detect.col.index')}</th>
              <th scope="col">{t('detect.col.ip')}</th>
              <th scope="col">{t('detect.col.hostname')}</th>
              <th scope="col">{t('detect.col.mark')}</th>
              <th scope="col">{t('detect.col.latency')}</th>
            </tr>
          </thead>
          <tbody>
            {listedCandidates.length === 0 ? (
              <tr className={detectStyles.emptyRow}>
                {/* 测过但全都不通 ≠ 还没检测：这一格不能拿「还没有检测结果」糊过去，
                    否则用户会以为再来一轮检测就有货，其实是这一批地址都不行。 */}
                <td colSpan={5}>{t(flatCandidates.length > 0 ? 'detect.empty.unusable' : 'detect.empty')}</td>
              </tr>
            ) : (
              listedCandidates.map(({ candidate, host }, index) => (
                <tr
                  key={candidate.id}
                  data-arrow-index={index}
                  className={`${CANDIDATE_ROW_CLASS[toneForCandidate(candidate.status)]} ${candidate.applied ? detectStyles.currentRow : ''} ${index === safeCandFocus ? detectStyles.candFocus : ''}`.trim() || undefined}
                  onClick={(event) => {
                    event.currentTarget.closest('table')?.focus()
                    setCandFocus(index)
                  }}
                >
                  <td>{index + 1}</td>
                  <td className={detectStyles.ipCell}>
                    {candidate.ip}
                    {candidate.ipv6 ? <span className={detectStyles.v6}> · {t('detect.ipv6')}</span> : null}
                    {candidate.applied ? <span className={detectStyles.currentTag}>{t('detect.current')}</span> : null}
                  </td>
                  <td className={detectStyles.hostCell}>{host.hostname}</td>
                  <td>
                    <span className={`${detectStyles.tag} ${CANDIDATE_TAG_CLASS[toneForCandidate(candidate.status)]}`}>
                      {t(CANDIDATE_KEY[candidate.status] ?? 'detect.candidate.unknown')}
                    </span>
                    {candidate.reason ? <span className={detectStyles.reason}>{candidate.reason}</span> : null}
                  </td>
                  <td className={candidate.latencyMs > 0 ? detectStyles.latency : detectStyles.latencyNone}>
                    {candidate.latencyMs > 0 ? t('detect.latency', { ms: candidate.latencyMs }) : '—'}
                  </td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>

      {restoreBox ? (
        <Dialog
          title={
            restoreBox.view === 'restore'
              ? t('detail.detectBox.firstTitle')
              : t('detail.detectBox.secondTitle')
          }
          onClose={() => setRestoreBox(null)}
          width={760}
          footer={
            <button type="button" className={uiStyles.btn} onClick={() => setRestoreBox(null)}>
              {t('detail.detectBox.close')}
            </button>
          }
        >
          <DetectTables
            name={restoreBox.name}
            itemId={restoreBox.itemId}
            hostnames={boxHostnames}
            entries={boxAllEntries}
            only={restoreBox.view}
            actions={{
              restoreIp: () => onRestore(restoreBox.itemId),
              detectRun: (strategyId: string, profile: NetworkProfile) =>
                onDetectRun(restoreBox.itemId, strategyId, profile),
              writeIp: () => onApply(restoreBox.itemId),
            }}
            busy={{
              detect: boxDetectRunning,
              restore: boxRestoreRunning,
              apply: boxApplyRunning,
              restoreSettledOk: boxRestoreSettledOk,
              restoreFailed: boxRestoreFailed,
            }}
            ready={{
              canDetectAll: boxItem !== undefined,
              hasDetectResult: boxConfirmReady,
            }}
            refreshKey={boxRefreshKey}
            onReprobed={() => invalidate('results', { itemIds: restoreBox ? [restoreBox.itemId] : undefined })}
            scopeId={category?.scopeId ?? ''}
            detectRun={(strategyId, profile) => {
              onDetectRun(restoreBox.itemId, strategyId, profile)
              // 新候选的结果在「检测与写入」表：点了就切过去，现场看逐条实测
              setRestoreBox((prev) => (prev ? { ...prev, view: 'detect' } : prev))
            }}
          />

          {/* 检测/还原的进度与回执照旧摆在表下方，跟着视图走。 */}
          {restoreBox.view === 'detect' && (boxDetectText || boxDetectRunning) ? (
            <div className={styles.boxDetect}>
              <p className={styles.boxStage}>{boxDetectText || t('detail.box.noResult')}</p>
            </div>
          ) : null}
          {restoreBox.view === 'restore' && boxRestoreTask ? (
            <p className={styles.boxStage}>{boxRestoreText}</p>
          ) : null}
        </Dialog>
      ) : null}

      {restoreMenuOpen ? (
        <Dialog
          title={t('hosts.restoreCategory.title', { name: category.name, count: restorableItems.length })}
          onClose={() => setRestoreMenuOpen(false)}
          width={720}
          height={560}
          footer={
            <>
              <button type="button" className={uiStyles.btn} onClick={() => setRestoreMenuOpen(false)}>
                {t('hosts.restoreCategory.cancel')}
              </button>
              <button
                type="button"
                className={`${uiStyles.btn} ${uiStyles.primary}`}
                // 对照表读出来了而且是空的，就没有任何要还原的东西：置灰并说明原因，点了也只会空跑一趟。
                disabled={menuPreview !== null && (menuPreview.rows ?? []).length === 0}
                title={menuPreview !== null && (menuPreview.rows ?? []).length === 0
                  ? t('hosts.restoreCategory.nothingToRestore')
                  : undefined}
                onClick={() => {
                  setRestoreMenuOpen(false)
                  onRestoreCategory(restorableItems.map((it) => it.id))
                }}
              >
                {t('hosts.restoreCategory.confirmAction', { count: restorableItems.length })}
              </button>
            </>
          }
        >
          <p className={tableStyles.lead}>
            {t('hosts.restoreCategory.body', { count: restorableItems.length })}
          </p>
          <RestorePreviewTable
            rows={menuPreview?.rows ?? null}
            groups={menuGroups}
            loading={menuPreview === null && menuPreviewError === ''}
            error={menuPreviewError}
          />
        </Dialog>
      ) : null}
    </div>
  )
}
