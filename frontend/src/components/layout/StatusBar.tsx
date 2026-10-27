import type { AppStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { profileLabel } from '../../features/settings/profileOptions'
import { formatInteger } from '../../i18n/formatters'
import { t } from '../../i18n'
import styles from './AppShell.module.css'

export type CatalogCounts = { categories: number; items: number; hostnames: number }

// 底部状态栏只放后端真给出来的东西；没有来源的字段显示「未检测」，不编数字。
export function StatusBar({
  status,
  settings,
  counts,
  sequence,
  dropped,
  themeNotice,
}: {
  status: AppStatus | null
  settings: Config | null
  counts: CatalogCounts
  sequence: number
  dropped: number
  themeNotice: string
}) {
  return (
    <>
      {settings && <span className={styles.item}>{t('statusbar.mode', { name: profileLabel(settings.network.profile) })}</span>}
      {status && (
        <>
          <span className={styles.item}>{t('statusbar.hostsPath', { path: status.hostsPath })}</span>
          <span className={styles.item}>
            {t('statusbar.dns', {
              resolver: status.dnsResolver,
              state: status.dnsDetectionReady ? t('statusbar.dns.ready') : t('statusbar.dns.waiting'),
            })}
          </span>
        </>
      )}
      {/* 检测任务还没接入，所以「上次检测」只能是未检测，不能显示时间。 */}
      <span className={styles.item}>{t('statusbar.lastDetect', { value: t('statusbar.lastDetect.none') })}</span>
      <span className={styles.item}>{t('statusbar.sequence', { sequence, dropped })}</span>
      <span className={styles.spacer} />
      {status?.configNotice && (
        <span className={styles.item} role="alert">
          {t('statusbar.notice', { notice: status.configNotice })}
        </span>
      )}
      {themeNotice && (
        <span className={styles.item} role="status">
          {themeNotice}
        </span>
      )}
      <span className={styles.item}>
        {t('statusbar.counts', {
          categories: formatInteger(counts.categories),
          items: formatInteger(counts.items),
          hostnames: formatInteger(counts.hostnames),
        })}
      </span>
    </>
  )
}
