import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Candidate } from '../../../bindings/github.com/xiaokentrl/github-net/internal/accelerator/models'
import { formatInteger, formatRelativeTime } from '../../i18n/formatters'
import type { CatalogCategory, CatalogItem, DetectResultView, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { Dialog } from '../../components/ui/Dialog'
import { EmptyState } from '../../components/ui/EmptyState'
import { StatusBadge } from '../../components/ui/StatusBadge'
import uiStyles from '../../components/ui/ui.module.css'
import { pickBestCandidate } from '../detect/candidateChoice'
import { ServiceStrategySelector } from '../strategies/ServiceStrategySelector'
import { ensureLastResults, isTerminalTask, useTaskQueue } from '../tasks/taskStore'
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

// 「哪些项目能进批量检测」只有一个主人：这里算，App 拿到的是已经算好的那批。
// 挡在外面的是两类——域名由用户自填的手动映射，和目录里没给主机名的项：都没有可解析的目标。
export function detectableItems(items: CatalogItem[]): CatalogItem[] {
  return items.filter((it) => !it.userSupplied && (it.hostnames?.length ?? 0) > 0)
}

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
  detectReady,
  detectError,
  hostsError,
  onDetect,
  onDetectAll,
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
  detectReady: boolean
  detectError: string
  hostsError: string
  onDetect: (itemId: string, strategyId?: string) => void
  onDetectAll: (items: CatalogItem[]) => void
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
  // 还原本菜单的公共弹框（拍板 2026-11-03）：确认文案在框里，动作走 onRestoreCategory。
  const [restoreMenuOpen, setRestoreMenuOpen] = useState(false)
  // 行级「还原此项」公共弹框（拍板 2026-11-03，已简化）：点行内「还原」直接进检测那一屏，
  // 框只剩两屏——detect 看数据、restoring 给回执；「立刻还原」要看得见这一项的检测数据才可点。
  // writtenCount 是点开那一刻数出来的待还原条数——还原落定后 entries 会被刷新成空，
  // 回执要是跟着现数就会写成「已还原 0 条」。
  const [restoreBox, setRestoreBox] = useState<{ itemId: string; name: string; writtenCount: number } | null>(null)
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
  const boxRestoreSettled = boxRestoreTask !== undefined && !boxRestoreRunning && isTerminalTask(boxRestoreTask.status)
  const boxHosts = restoreBox ? hostsOf.get(restoreBox.itemId) : undefined
  const boxEntries = (boxHosts?.entries ?? []).filter((entry) => entry.written)
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
  // 框停在哪一屏：这一项有还原任务就是回执屏，否则就是检测屏。
  const boxStage: 'detect' | 'restoring' = boxRestoreTask ? 'restoring' : 'detect'

  // 点行内「还原」＝只开框，不排任务：用户没点「立刻还原」之前，列表和队列一格都不该被这一步改动。
  // 框里要看的检测数据在这屏加载时就补读进 store 了（见上面的 ensureLastResults），这里不再单独发请求。
  const openRestoreBox = (itemId: string, name: string, writtenCount: number) => {
    boxOpenedAt.current = Date.now()
    setRestoreBox({ itemId, name, writtenCount })
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

  const hostnames = item.hostnames ?? []
  // 批量按钮的口径是「这一分类里能测的那几项」，不是分类里的全部条目：文案、提示和真正发出去的批次必须同一个数。
  const batch = detectableItems(category.items ?? [])
  const selfDetectable = !item.userSupplied && hostnames.length > 0
  const result = state.results[item.id]
  const live = state.live[item.id]

  const rows = result?.hosts ?? []
  const candidates = rows.flatMap((host) => host.candidates ?? [])
  // 当前写入 IP 只有一个来源：后端在这轮候选里标了 applied 的那几条。界面不自己认。
  // 一条域名一行摊全（拍板 2026-11-02）：一个地址被几条域名共用就各写一行，
  // 不再收成「N 个域名各有各的写入」那种要人回头数的说法。
  // 当前写入的取数源是 hosts.entries（后端这次读回来的真值）：内存轮的 applied 标记
  // 重启后就没了，用它会出现「同一页面两个当前写入还互相矛盾」。只列已写入的，
  // 未写入的域名在下面那行域名清单里已经有。
  const canApply = candidates.some((c) => c.status === 'available')

  // 可还原的二级菜单：各自的 Hosts 状态是已应用或部分生效（拍板 2026-11-03）。
  const restorableItems = (category.items ?? []).filter((it) => {
    const st = hostsOf.get(it.id)?.status ?? HostsStatus.HostsUnknown
    return st === HostsStatus.HostsApplied || st === HostsStatus.HostsPartial
  })

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
            detecting={detecting}
            onDetectSelected={(strategyId) => onDetect(item.id, strategyId)}
          />
          <div className={styles.headActions}>
            <StatusBadge state={projectState(hosts, state.tasks, item.id, result)} />
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
            {(category.items ?? []).map((it) => {
              const hs = hostsOf.get(it.id)
              const res = state.results[it.id]
              // 「【Hosts】当前写入 IP」摊成每个域名一行，行首带写入短标签（拍板 2026-11-06）：
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
              // 全都连不通的项目由 projectState 压进未写档，整行跟着灰（拍板 2026-11-06 晚）。
              const rowTone = TONE_ROW_CLASS[toneForItem(hs, state.tasks, it.id, res)]
              const rowClass = [rowTone, selectedRow ? styles.rowSel : ''].filter(Boolean).join(' ')
              return (
                <tr key={it.id} className={rowClass || undefined} onClick={() => onSelectItem(it.id)}>
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
                    {restorableItems.some((r) => r.id === it.id) ? (
                      <button
                        type="button"
                        className={`${uiStyles.btn} ${uiStyles.small} ${uiStyles.restore}`}
                        title={t('hosts.restore')}
                        onClick={(event) => {
                          event.stopPropagation()
                          openRestoreBox(it.id, it.name, claimed)
                        }}
                      >
                        {t('hosts.restoreRow')}
                      </button>
                    ) : null}
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
        <div className={styles.cardHead}>
          <h3 className={styles.title}>{t('detail.candidates.title')}</h3>
          <span className={styles.countPill}>{t('detail.candidates.count', { count: listedCandidates.length })}</span>
          {/* 这一轮的账（可用／探测／DNS）跟在「N 个候选」后面同一行：它说的是这张表的来路，
              单独占一行摆在表上方会把标题行和表拉散。 */}
          {result && rows.length > 0 ? (
            <span className={styles.headMeta}>
              {t('detect.meta', { available: result.available, probes: result.probesUsed, dns: result.dnsQueries })}
              {result.truncated ? ` · ${t('detect.truncated')}` : ''}
              {result.deadlineHit ? ` · ${t('detect.deadlineHit')}` : ''}
              {result.cancelled ? ` · ${t('detect.cancelledBadge')}` : ''}
            </span>
          ) : null}
          <div className={detectStyles.candidateActions}>
            {/* 只留两颗（拍板 2026-10-31）：「检测」检测当前菜单下全部能测的项目
                （原「检测全部」，和单项检测重复所以合并），「应用到 Hosts」只管当前项目。
                单项检测由上方方法下拉旁那颗「检测」负责（用下拉选中的方法测当前项目）。 */}
            {detectReady && batch.length > 0 ? (
              <button
                type="button"
                className={`${uiStyles.btn} ${uiStyles.small}`}
                disabled={detecting}
                onClick={() => onDetectAll(batch)}
                title={t('detect.allHint', { count: batch.length })}
              >
                {detecting ? t('detect.running') : t('detect.start')}
              </button>
            ) : null}
            {detectReady && selfDetectable ? (
              <button
                type="button"
                className={`${uiStyles.btn} ${uiStyles.small} ${uiStyles.primary}`}
                disabled={detecting || !canApply}
                onClick={() => onApply(item.id)}
              >
                {t('hosts.apply')}
              </button>
            ) : null}
            {detecting && live ? <span className={styles.hint}>{t('detect.live', { done: live.done, total: live.total, active: live.active })}</span> : null}
            {!detectReady ? <span className={styles.hint}>{t('detect.notReady')}</span> : null}
            {detectReady && !selfDetectable ? <span className={styles.hint}>{t('detect.unavailable')}</span> : null}
          </div>
        </div>

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
        <table className={detectStyles.cands}>
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
                  className={`${CANDIDATE_ROW_CLASS[toneForCandidate(candidate.status)]} ${candidate.applied ? detectStyles.currentRow : ''}`.trim() || undefined}
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
          title={t('detail.box.title', { name: restoreBox.name })}
          onClose={() => setRestoreBox(null)}
          footer={
            boxStage === 'detect' ? (
              <>
                <button type="button" className={uiStyles.btn} disabled={boxDetectRunning} onClick={() => onDetect(restoreBox.itemId)}>
                  {boxDetectRunning ? t('detail.box.detecting') : t('detail.box.redetect')}
                </button>
                <button
                  type="button"
                  className={`${uiStyles.btn} ${uiStyles.primary}`}
                  disabled={!boxConfirmReady || boxEntries.length === 0 || boxRestoreRunning || boxRestoreSettled}
                  onClick={() => onRestore(restoreBox.itemId)}
                >
                  {t('detail.box.restoreNow')}
                </button>
                <button type="button" className={uiStyles.btn} onClick={() => setRestoreBox(null)}>
                  {t('detail.box.abandon')}
                </button>
              </>
            ) : (
              <button type="button" className={uiStyles.btn} onClick={() => setRestoreBox(null)}>
                {t('detail.box.close')}
              </button>
            )
          }
        >
          {/* 待还原清单两屏常驻：无论停在哪一屏，用户都看得见要退的是哪几条 */}
          {boxEntries.length > 0 ? (
            <>
              <p className={styles.boxStage}>{t('detail.box.summary', { count: boxEntries.length })}</p>
              <ul className={styles.boxLines}>
                {boxEntries.map((entry) => (
                  <li key={entry.hostname} className={styles.boxLine}>
                    <span>{entry.hostname}</span>
                    <span className={styles.cellMono}>{entry.ip}</span>
                  </li>
                ))}
              </ul>
            </>
          ) : (
            <p className={styles.boxStage}>{t('detail.box.none')}</p>
          )}

          <div className={styles.boxDetect}>
            <p className={styles.boxStage}>{boxDetectText || t('detail.box.noResult')}</p>
            {boxResult?.hosts && boxResult.hosts.length > 0 ? (
              <ul className={`${styles.boxLines} ${(boxResult.available ?? 0) > 0 ? styles.boxLinesOk : ''}`.trim()}>
                {boxResult.hosts.map((host) => {
                  const best = pickBestCandidate(host.candidates ?? [])
                  return (
                    <li key={host.hostname} className={styles.boxLine}>
                      <span>{host.hostname}</span>
                      <span className={best ? styles.cellMono : styles.dim}>
                        {best
                          ? best.latencyMs > 0
                            ? `${best.ip} · ${best.latencyMs}ms`
                            : best.ip
                          : host.resolved
                            ? t('queue.detail.noUsableAddress')
                            : t('queue.detail.resolveFailed')}
                      </span>
                    </li>
                  )
                })}
              </ul>
            ) : null}
          </div>

          {boxStage === 'detect' ? (
            <p className={styles.boxHint}>
              {boxConfirmReady
                ? t('detail.box.confirmTip')
                : boxDetectRunning
                  ? t('detail.box.confirmWaiting')
                  : t('detail.box.confirmNeedDetect')}
            </p>
          ) : null}

          {boxRestoreTask ? <p className={styles.boxStage}>{boxRestoreText}</p> : null}
        </Dialog>
      ) : null}

      {restoreMenuOpen ? (
        <Dialog
          title={t('hosts.restoreCategory.title', { name: category.name, count: restorableItems.length })}
          onClose={() => setRestoreMenuOpen(false)}
          footer={
            <>
              <button type="button" className={uiStyles.btn} onClick={() => setRestoreMenuOpen(false)}>
                {t('hosts.restoreCategory.cancel')}
              </button>
              <button
                type="button"
                className={`${uiStyles.btn} ${uiStyles.primary}`}
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
          <p className={styles.flowDesc}>
            {t('hosts.restoreCategory.body', {
              count: restorableItems.length,
              names: restorableItems.map((it) => it.name).join('、'),
            })}
          </p>
        </Dialog>
      ) : null}
    </div>
  )
}
