import { useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Candidate } from '../../../bindings/github.com/xiaokentrl/github-net/internal/accelerator/models'
import { formatInteger, formatRelativeTime } from '../../i18n/formatters'
import type { CatalogCategory, CatalogItem, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { Dialog } from '../../components/ui/Dialog'
import { EmptyState } from '../../components/ui/EmptyState'
import { StatusBadge } from '../../components/ui/StatusBadge'
import uiStyles from '../../components/ui/ui.module.css'
import { pickBestCandidate } from '../detect/candidateChoice'
import { ServiceStrategySelector } from '../strategies/ServiceStrategySelector'
import { useTaskQueue } from '../tasks/taskStore'
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

// 八档分到原型那四种徽章色（原型 .tag 第 290–292 行只有三种，这里多出来的都归中性）。
const CANDIDATE_TONE: Record<string, string> = {
  available: detectStyles.tagOk,
  unavailable: detectStyles.tagFail,
  timeout: detectStyles.tagWarn,
  probing: detectStyles.tagBusy,
  queued: detectStyles.tagBusy,
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

  // 列宽：拖过一次就转固定布局（px），没拖过是 null（auto 自适应）。
  const tableRef = useRef<HTMLTableElement | null>(null)
  const [colWidths, setColWidths] = useState<number[] | null>(null)
  // 还原本菜单的公共弹框（拍板 2026-11-03）：确认文案在框里，动作走 onRestoreCategory。
  const [restoreMenuOpen, setRestoreMenuOpen] = useState(false)
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

  const status = hosts?.status ?? HostsStatus.HostsUnknown
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
            <StatusBadge status={status} reason={hosts?.message} />
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
              <th />
            </tr>
          </thead>
          <tbody>
            {(category.items ?? []).map((it) => {
              const hs = hostsOf.get(it.id)
              const res = state.results[it.id]
              const written = (hs?.entries ?? []).filter((entry) => entry.written && entry.ip)
              const cands = (res?.hosts ?? []).flatMap((host) => host.candidates ?? [])
              const best = pickBestCandidate(cands)
              const availableCount = cands.filter((c) => c.status === 'available').length
              const selectedRow = it.id === item.id
              return (
                <tr key={it.id} className={selectedRow ? styles.rowSel : undefined} onClick={() => onSelectItem(it.id)}>
                  <td className={styles.itemName}>
                    {selectedRow ? (
                      <span className={styles.rowMark} aria-hidden="true">
                        ▸
                      </span>
                    ) : null}
                    {it.name}
                  </td>
                  <td>
                    {written.length > 0 ? (
                      <span className={styles.writeStack}>
                        {written.map((entry) => (
                          <span key={entry.hostname} className={styles.writeItem}>
                            {entry.hostname}→{entry.ip}
                          </span>
                        ))}
                      </span>
                    ) : (
                      '—'
                    )}
                  </td>
                  <td className={styles.cellMono}>
                    {best ? (best.latencyMs > 0 ? `${best.ip} · ${best.latencyMs}ms` : best.ip) : '—'}
                  </td>
                  <td>{cands.length > 0 ? `${formatInteger(availableCount)}/${formatInteger(cands.length)}` : '—'}</td>
                  <td>{res?.finishedAt ? formatRelativeTime(res.finishedAt) : '—'}</td>
                  <td className={styles.rowAction}>
                    {restorableItems.some((r) => r.id === it.id) ? (
                      <button
                        type="button"
                        className={`${uiStyles.btn} ${uiStyles.small} ${uiStyles.restore}`}
                        title={t('hosts.restore')}
                        onClick={(event) => {
                          event.stopPropagation()
                          onRestore(it.id)
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
          <span className={styles.countPill}>{t('detail.candidates.count', { count: flatCandidates.length })}</span>
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

        {result && rows.length > 0 ? (
          <p className={styles.hint}>
            {t('detect.meta', { available: result.available, probes: result.probesUsed, dns: result.dnsQueries })}
            {result.truncated ? ` · ${t('detect.truncated')}` : ''}
            {result.deadlineHit ? ` · ${t('detect.deadlineHit')}` : ''}
            {result.cancelled ? ` · ${t('detect.cancelledBadge')}` : ''}
          </p>
        ) : null}

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
            {flatCandidates.length === 0 ? (
              <tr className={detectStyles.emptyRow}>
                <td colSpan={5}>{t('detect.empty')}</td>
              </tr>
            ) : (
              flatCandidates.map(({ candidate, host }, index) => (
                <tr key={candidate.id} className={candidate.applied ? detectStyles.currentRow : undefined}>
                  <td>{index + 1}</td>
                  <td className={detectStyles.ipCell}>
                    {candidate.ip}
                    {candidate.ipv6 ? <span className={detectStyles.v6}> · {t('detect.ipv6')}</span> : null}
                    {candidate.applied ? <span className={detectStyles.currentTag}>{t('detect.current')}</span> : null}
                  </td>
                  <td className={detectStyles.hostCell}>{host.hostname}</td>
                  <td>
                    <span className={`${detectStyles.tag} ${CANDIDATE_TONE[candidate.status] ?? ''}`}>
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

      {restoreMenuOpen ? (
        <Dialog
          title={t('hosts.restoreCategory')}
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
                {t('hosts.restoreCategory.confirmAction')}
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
