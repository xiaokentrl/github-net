import { t } from '../../i18n'
import type { HostsRestorePreview } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import styles from './RestorePreviewCards.module.css'

// 「还原本菜单」的前后对照：判据与「清除已写入的 Hosts」预览一致——
// after 空＝整条删掉、和 before 相同＝不变、其余＝变回应用前的 IP。
// 这里喂进来的是「这批二级菜单一起还原」的联合预览，不能由单项目预览拼出来。
type PreviewRow = NonNullable<HostsRestorePreview['rows']>[number]
type Kind = 'restore' | 'remove' | 'keep'

const GROUP_KEY: Record<Kind, { title: string; hint: string; sum: string }> = {
  restore: {
    title: 'hosts.restoreCategory.groupRestore',
    hint: 'hosts.restoreCategory.groupRestoreHint',
    sum: 'hosts.restoreCategory.sumRestore',
  },
  remove: {
    title: 'hosts.restoreCategory.groupRemove',
    hint: 'hosts.restoreCategory.groupRemoveHint',
    sum: 'hosts.restoreCategory.sumRemove',
  },
  keep: {
    title: 'hosts.restoreCategory.groupKeep',
    hint: 'hosts.restoreCategory.groupKeepHint',
    sum: 'hosts.restoreCategory.sumKeep',
  },
}

const SUM_CLASS: Record<Kind, string> = {
  restore: styles.sumRestore,
  remove: styles.sumRemove,
  keep: styles.sumKeep,
}
const CARD_CLASS: Record<Kind, string> = {
  restore: styles.cardRestore,
  remove: styles.cardRemove,
  keep: styles.cardKeep,
}

function groups(rows: PreviewRow[]): { kind: Kind; rows: PreviewRow[] }[] {
  const by: Record<Kind, PreviewRow[]> = { restore: [], remove: [], keep: [] }
  for (const r of rows) {
    if (!r.after) by.remove.push(r)
    else if (r.after === r.before) by.keep.push(r)
    else by.restore.push(r)
  }
  return (['restore', 'remove', 'keep'] as const)
    .map((kind) => ({ kind, rows: by[kind] }))
    .filter((g) => g.rows.length > 0)
}

export function RestorePreviewCards({ rows, loading, error }: {
  rows: PreviewRow[] | null
  loading: boolean
  error: string
}) {
  if (loading) {
    return <p className={styles.note}>{t('hosts.restoreCategory.previewLoading')}</p>
  }
  if (error) {
    return <p className={styles.alert}>{t('hosts.restoreCategory.previewFailed', { reason: error })}</p>
  }
  const list = rows ?? []
  if (list.length === 0) {
    return <p className={styles.note}>{t('hosts.restoreCategory.previewEmpty')}</p>
  }
  const gs = groups(list)
  return (
    <div className={styles.wrap}>
      <h4 className={styles.title}>{t('hosts.restoreCategory.previewTitle')}</h4>
      <div className={styles.summary}>
        {gs.map((g) => (
          <span key={g.kind} className={`${styles.sum} ${SUM_CLASS[g.kind]}`}>
            {t(GROUP_KEY[g.kind].sum, { count: g.rows.length })}
          </span>
        ))}
      </div>
      {gs.map((g) => (
        <section key={g.kind} className={styles.group}>
          <div className={styles.groupHead}>
            <h5 className={styles.groupTitle}>{t(GROUP_KEY[g.kind].title)}</h5>
            <span className={styles.groupCount}>{t('hosts.restoreCategory.unitMappings', { count: g.rows.length })}</span>
          </div>
          <p className={styles.groupHint}>{t(GROUP_KEY[g.kind].hint)}</p>
          <div className={styles.cards}>
            {g.rows.map((r) => (
              <div key={r.hostname} className={`${styles.card} ${CARD_CLASS[g.kind]}`}>
                <div className={styles.domain}>{r.hostname}</div>
                <div className={styles.flow}>
                  <div className={styles.side}>
                    <span className={styles.sideLabel}>{t('hosts.restoreCategory.now')}</span>
                    <span className={styles.line}>
                      <span className={styles.ip}>{r.before}</span> <span className={styles.host}>{r.hostname}</span>
                    </span>
                  </div>
                  <span className={styles.arrow}>→</span>
                  <div className={styles.side}>
                    <span className={styles.sideLabel}>{t('hosts.restoreCategory.then')}</span>
                    {r.after ? (
                      <span className={styles.line}>
                        <span className={`${styles.ip} ${styles.afterIp}`}>{r.after}</span>{' '}
                        <span className={styles.host}>{r.hostname}</span>
                      </span>
                    ) : (
                      <span className={styles.afterNone}>{t('hosts.restoreCategory.thenRemove')}</span>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}
