import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { CatalogCategory, CatalogItem, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { formatDateTime, formatInteger } from '../../i18n/formatters'
import { EmptyState } from '../../components/ui/EmptyState'
import { StatusBadge } from '../../components/ui/StatusBadge'
import { useTaskQueue } from '../tasks/taskStore'
import styles from './ItemDetail.module.css'
import detectStyles from './DetectResult.module.css'

// 每种状态一句解释：后端只给档位，这句话不能由前端猜出来的结果撑着。
// partial 和 applied 要等 Hosts 写入接上才会由后端产出，这里先把话说清楚，不提前显示。
const HINT_KEY: Record<HostsStatus, string> = {
  [HostsStatus.$zero]: 'detail.hint.unknown',
  [HostsStatus.HostsUnknown]: 'detail.hint.unknown',
  [HostsStatus.HostsNone]: 'detail.hint.none',
  [HostsStatus.HostsPartial]: 'detail.hint.partial',
  [HostsStatus.HostsApplied]: 'detail.hint.applied',
  [HostsStatus.HostsConflict]: 'detail.hint.conflict',
  [HostsStatus.HostsReadFailed]: 'detail.hint.readFailed',
}

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

// 详情里每一行都来自后端 GetItems / GetDetectResult / 事件：没有的字段就说没有，不补演示数据。
export function ItemDetail({
  item,
  category,
  hosts,
  detectReady,
  detectError,
  onDetect,
}: {
  item: CatalogItem | null
  category: CatalogCategory | null
  hosts: HostsState | undefined
  detectReady: boolean
  detectError: string
  onDetect: (itemId: string) => void
}) {
  const state = useTaskQueue()
  if (!item || !category) return <EmptyState message={t('detail.empty')} />

  const status = hosts?.status ?? HostsStatus.HostsUnknown
  const hostnames = item.hostnames ?? []
  const result = state.results[item.id]
  const live = state.live[item.id]
  const detecting = state.tasks.some((task) => task.itemId === item.id && task.kind === 'detect' && (task.status === 'running' || task.status === 'waiting'))

  return (
    <div className={styles.wrap}>
      <h2 className={styles.title}>{item.name}</h2>
      <dl className={styles.fields}>
        <div>
          <dt>{t('detail.field.id')}</dt>
          <dd>{item.id}</dd>
        </div>
        <div>
          <dt>{t('detail.field.category')}</dt>
          <dd>{category.name}</dd>
        </div>
        <div>
          <dt>{t('detail.field.scope')}</dt>
          <dd>{category.scopeId}</dd>
        </div>
        <div>
          <dt>{t('detail.field.status')}</dt>
          <dd>
            <StatusBadge status={status} reason={hosts?.message} />
          </dd>
        </div>
        <div>
          <dt>{t('detail.field.readAt')}</dt>
          <dd>{hosts ? t('detail.readAt.value', { time: formatDateTime(hosts.readAt), count: formatInteger(hosts.revision) }) : t('detail.field.readAt.none')}</dd>
        </div>
      </dl>

      <p className={styles.hint}>{t(HINT_KEY[status])}</p>
      {hosts?.message ? <p className={styles.plain}>{t('detail.reason', { reason: hosts.message })}</p> : null}

      <h3 className={styles.subTitle}>{t('detail.field.hostnames')}</h3>
      {item.userSupplied ? (
        <p className={styles.plain}>{t('detail.hostnames.userSupplied')}</p>
      ) : hostnames.length === 0 ? (
        <p className={styles.plain}>{t('detail.hostnames.none')}</p>
      ) : (
        <ul className={styles.hosts}>
          {hostnames.map((hostname) => (
            <li key={hostname}>{hostname}</li>
          ))}
        </ul>
      )}

      <h3 className={styles.subTitle}>{t('detect.section')}</h3>
      {hostnames.length === 0 || item.userSupplied ? (
        <p className={detectStyles.note}>{t('detect.unavailable')}</p>
      ) : detectReady ? (
        <div className={detectStyles.actions}>
          <button
            type="button"
            className={detectStyles.detect}
            disabled={detecting}
            onClick={() => onDetect(item.id)}
          >
            {detecting ? t('detect.running') : t('detect.start')}
          </button>
          {detecting && live ? (
            <span className={detectStyles.live}>
              {t('detect.live', { done: live.done, total: live.total, active: live.active })}
            </span>
          ) : null}
        </div>
      ) : (
        <p className={detectStyles.note}>{t('detect.notReady')}</p>
      )}
      {detectError ? (
        <p className={detectStyles.detectError} role="alert">
          {t('detect.error', { reason: detectError })}
        </p>
      ) : null}

      {result && (result.hosts?.length ?? 0) > 0 ? (
        <div className={detectStyles.result}>
          <p className={detectStyles.meta}>
            {t('detect.meta', {
              available: result.available,
              probes: result.probesUsed,
              dns: result.dnsQueries,
            })}
            {result.truncated ? ` · ${t('detect.truncated')}` : ''}
            {result.deadlineHit ? ` · ${t('detect.deadlineHit')}` : ''}
            {result.cancelled ? ` · ${t('detect.cancelledBadge')}` : ''}
          </p>
          {(result.hosts ?? []).map((host) => (
            <div key={host.hostname} className={detectStyles.host}>
              <p className={detectStyles.hostname}>
                {host.hostname}
                {!host.resolved ? ` · ${t('detect.resolveFailed', { reason: host.detail ?? '' })}` : ''}
              </p>
              {(host.candidates ?? []).length === 0 ? (
                <p className={detectStyles.note}>{t('detect.noCandidates')}</p>
              ) : (
                <table className={detectStyles.table}>
                  <thead>
                    <tr>
                      <th scope="col">{t('detect.col.ip')}</th>
                      <th scope="col">{t('detect.col.status')}</th>
                      <th scope="col">{t('detect.col.latency')}</th>
                      <th scope="col">{t('detect.col.reason')}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(host.candidates ?? []).map((candidate) => (
                      <tr key={candidate.id}>
                        <td>
                          {candidate.ip}
                          {candidate.ipv6 ? <span className={detectStyles.v6}> · {t('detect.ipv6')}</span> : null}
                        </td>
                        <td>{t(CANDIDATE_KEY[candidate.status] ?? 'detect.candidate.unknown')}</td>
                        <td>{candidate.latencyMs > 0 ? t('detect.latency', { ms: candidate.latencyMs }) : '—'}</td>
                        <td className={detectStyles.reason}>{candidate.reason ?? ''}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          ))}
        </div>
      ) : (
        <p className={detectStyles.note}>{t('detect.empty')}</p>
      )}
    </div>
  )
}
