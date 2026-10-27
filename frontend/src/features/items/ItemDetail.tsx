import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { CatalogCategory, CatalogItem } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { EmptyState } from '../../components/ui/EmptyState'
import { StatusBadge } from '../../components/ui/StatusBadge'
import styles from './ItemDetail.module.css'

// 详情里每一行都来自后端 GetItems：没有的字段就说没有，不补演示数据。
export function ItemDetail({
  item,
  category,
  status,
}: {
  item: CatalogItem | null
  category: CatalogCategory | null
  status: HostsStatus | undefined
}) {
  if (!item || !category) return <EmptyState message={t('detail.empty')} />

  const shown = status ?? HostsStatus.HostsUnknown

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
            <StatusBadge status={shown} />
          </dd>
        </div>
      </dl>

      <p className={styles.hint}>{t('detail.statusHint')}</p>

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
