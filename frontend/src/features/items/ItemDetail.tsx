import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { CatalogCategory, CatalogItem, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { formatDateTime, formatInteger } from '../../i18n/formatters'
import { EmptyState } from '../../components/ui/EmptyState'
import { StatusBadge } from '../../components/ui/StatusBadge'
import styles from './ItemDetail.module.css'

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

// 详情里每一行都来自后端 GetItems：没有的字段就说没有，不补演示数据。
export function ItemDetail({
  item,
  category,
  hosts,
}: {
  item: CatalogItem | null
  category: CatalogCategory | null
  hosts: HostsState | undefined
}) {
  if (!item || !category) return <EmptyState message={t('detail.empty')} />

  const status = hosts?.status ?? HostsStatus.HostsUnknown

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
      ) : item.hostnames.length === 0 ? (
        <p className={styles.plain}>{t('detail.hostnames.none')}</p>
      ) : (
        <ul className={styles.hosts}>
          {item.hostnames.map((hostname) => (
            <li key={hostname}>{hostname}</li>
          ))}
        </ul>
      )}
    </div>
  )
}
