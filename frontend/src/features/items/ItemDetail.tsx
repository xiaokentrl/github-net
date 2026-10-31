import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { formatInteger, formatRelativeTime } from '../../i18n/formatters'
import type { CatalogCategory, CatalogItem, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { EmptyState } from '../../components/ui/EmptyState'
import { StatusBadge } from '../../components/ui/StatusBadge'
import uiStyles from '../../components/ui/ui.module.css'
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

// 「哪些项目能进批量检测」只有一个主人：这里算，App 拿到的是已经算好的那批。
// 挡在外面的是两类——域名由用户自填的手动映射，和目录里没给主机名的项：都没有可解析的目标。
export function detectableItems(items: CatalogItem[]): CatalogItem[] {
  return items.filter((it) => !it.userSupplied && (it.hostnames?.length ?? 0) > 0)
}

// 详情里每一行都来自后端 GetItems / GetDetectResult / 事件：没有的字段就说没有，不补演示数据。
// 六格指标里「检测最优 IP」「综合评分」「IP 来源」后端还没给，一律显示未提供；要填它们得先加后端字段。
export function ItemDetail({
  item,
  category,
  hosts,
  detectReady,
  detectError,
  hostsError,
  onDetect,
  onDetectAll,
  onApply,
  onRestore,
  onStrategySaved,
}: {
  item: CatalogItem | null
  category: CatalogCategory | null
  hosts: HostsState | undefined
  detectReady: boolean
  detectError: string
  hostsError: string
  onDetect: (itemId: string, strategyId?: string) => void
  onDetectAll: (items: CatalogItem[]) => void
  onApply: (itemId: string) => void
  onRestore: (itemId: string) => void
  onStrategySaved: () => void
}) {
  const state = useTaskQueue()
  // 本项目是否正在检测；钩子要在早退之前挂，这里先算出来。
  const detecting = state.tasks.some(
    (task) => item !== null && task.itemId === item.id && task.kind === 'detect' && (task.status === 'running' || task.status === 'waiting'),
  )
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
  const appliedIps = [...new Set(candidates.filter((c) => c.applied).map((c) => c.ip))]
  const restorable = status === HostsStatus.HostsApplied || status === HostsStatus.HostsPartial
  const canApply = candidates.some((c) => c.status === 'available')

  // 候选表只装当前二级菜单自己的 IP（拍板 2026-10-31）：所有候选共用一张表、
  // 域名是一列；排序按延迟升序，没测出延迟（0）的按原顺序排在后面。
  const flatCandidates = rows
    .flatMap((host) => (host.candidates ?? []).map((candidate) => ({ host, candidate })))
    .map((row, order) => ({ ...row, order }))
    .sort((a, b) => {
      const la = a.candidate.latencyMs > 0 ? a.candidate.latencyMs : Number.MAX_SAFE_INTEGER
      const lb = b.candidate.latencyMs > 0 ? b.candidate.latencyMs : Number.MAX_SAFE_INTEGER
      return la - lb || a.order - b.order
    })
  // 解析失败的域名没有候选行，单独在表上方说一句，不混进表里冒充候选。
  const failedHosts = rows.filter((h) => !h.resolved)

  // 指标六格的数据（拍板 2026-10-31）：全部来自本轮候选，前端现算。
  // 最优可用 = 延迟最低（并列时 IPv4 优先），和后端写 Hosts 的选址规则同一口径。
  const bestCandidate = candidates.reduce<((typeof candidates)[number]) | null>((acc, c) => {
    if (c.status !== 'available') return acc
    const better = acc === null || c.latencyMs < acc.latencyMs || (c.latencyMs === acc.latencyMs && acc.ipv6 && !c.ipv6)
    return better ? c : acc
  }, null)
  const availableCount = candidates.filter((c) => c.status === 'available').length
  const strategyLabel = result?.strategyName || result?.strategyId || ''

  // 一格指标：检测中整块换微光条；值没来源时用 dim 那档字色，和原型的 b.dim 同一处理。
  const metricCell = (labelKey: string, value: string) => (
    <div key={labelKey} className={styles.metric}>
      <span className={styles.metricLabel}>{t(labelKey)}</span>
      {detecting ? (
        <span className={styles.skeleton} aria-hidden="true" />
      ) : (
        <span className={`${styles.metricValue} ${labelKey === 'detail.metric.appliedIp' || labelKey === 'detail.metric.bestIp' ? styles.mono : ''}`}>{value}</span>
      )}
    </div>
  )

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
            {selfDetectable && restorable ? (
              <button
                type="button"
                className={`${uiStyles.btn} ${uiStyles.small} ${uiStyles.restore}`}
                disabled={detecting}
                title={t('hosts.restoreHint')}
                onClick={() => onRestore(item.id)}
              >
                {t('hosts.restore')}
              </button>
            ) : null}
          </div>
        </div>

        {/* 六格指标是布局骨架，常驻：没值显示「—」，全部有数据来源（拍板 2026-10-31）。 */}
        <div className={styles.metrics}>
          {metricCell(
            'detail.metric.appliedIp',
            appliedIps.length === 0
              ? '—'
              : appliedIps.length === 1
                ? appliedIps[0]
                : t('detail.metric.appliedIp.many', { count: appliedIps.length }),
          )}
          {metricCell('detail.metric.bestIp', bestCandidate?.ip ?? '—')}
          {metricCell(
            'detail.metric.latency',
            bestCandidate && bestCandidate.latencyMs > 0 ? t('detect.latency', { ms: bestCandidate.latencyMs }) : '—',
          )}
          {metricCell('detail.metric.available', candidates.length > 0 ? `${formatInteger(availableCount)}/${formatInteger(candidates.length)}` : '—')}
          {metricCell('detail.metric.strategy', strategyLabel || '—')}
          {metricCell('detail.metric.lastCheck', result?.finishedAt ? formatRelativeTime(result.finishedAt) : '—')}
        </div>

        <div className={styles.domainsLine}>
          <span className={styles.lbl}>{t('detail.domains')}</span>
          {item.userSupplied ? (
            <span className={styles.dim}>{t('detail.hostnames.userSupplied')}</span>
          ) : hostnames.length === 0 ? (
            <span className={styles.dim}>{t('detail.hostnames.none')}</span>
          ) : (
            hostnames.map((hostname) => (
              <span key={hostname} className={styles.chip}>
                {hostname}
              </span>
            ))
          )}
        </div>

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
              <th scope="col">{t('detect.col.score')}</th>
            </tr>
          </thead>
          <tbody>
            {flatCandidates.length === 0 ? (
              <tr className={detectStyles.emptyRow}>
                <td colSpan={6}>{t('detect.empty')}</td>
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
                  <td className={detectStyles.none}>—</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}
