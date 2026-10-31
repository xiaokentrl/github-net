import type { AppStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { LICENSE_NAME, REPO_HOME_URL, REPO_ISSUES_URL, REPO_LICENSE_URL } from '../about/aboutInfo'
import { formatDateTime } from '../../i18n/formatters'
import { t } from '../../i18n'
import { useExternalLink } from '../../hooks/useExternalLink'
import styles from './SettingsDialog.module.css'

// 关于页只显示后端已经知道的事实：应用名和版本、配置真正落在哪个目录、
// 这份构建用的源码是哪一笔、什么时间。任何一项读不到就写「未提供」，不拿今天的时间或猜的路径顶上。
// 三个外链的地址来自 features/about/aboutInfo.ts 那一份，许可证名字对着仓库根的 LICENSE 核；
// 两边谁改了忘了另一处，internal/app/about_parity_test.go 会红。
export function AboutPane({ status }: { status: AppStatus | null }) {
  const link = useExternalLink()
  const value = (present: string | undefined) => (present ? present : t('about.value.none'))

  const links: { label: string; url: string }[] = [
    { label: t('about.link.home'), url: REPO_HOME_URL },
    { label: t('about.link.issues'), url: REPO_ISSUES_URL },
    { label: t('about.link.license'), url: REPO_LICENSE_URL },
  ]

  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.about.title')}</h3>

      <div className={styles.card}>
        <div className={styles.aboutHero}>
          {/* 品牌标识不跟主题走：原型第 25 行的 .logo 与第 471 行的 .about-logo-lg 本来就是同一对色值，
              六套主题都没覆盖它，所以这里继续写死，和顶栏那个小块保持一致。 */}
          <span className={styles.aboutMark} aria-hidden="true">
            GH
          </span>
          <div className={styles.aboutInfo}>
            <h3 className={styles.aboutName}>{value(status?.app)}</h3>
            <p className={styles.aboutTagline}>{t('about.tagline')}</p>
            <div className={styles.aboutLinks}>
              {links.map((entry) => (
                <button key={entry.url} type="button" className={styles.linkBtn} onClick={() => link.open(entry.url)}>
                  {entry.label}
                </button>
              ))}
            </div>
            {/* 打不开就当场说清，不留「点了没反应」这种最难查的状态。 */}
            {link.failed !== '' && (
              <p className={styles.linkFailed} role="alert">
                {link.failed}
              </p>
            )}
          </div>
        </div>
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⓘ
          </span>
          {t('about.versionTitle')}
        </div>
        <div className={styles.aboutRows}>
          <div className={styles.row}>
            <span className={styles.rowLabel}>{t('about.row.version')}</span>
            <b className={styles.rowValue}>{status ? `v${status.version}` : t('about.value.none')}</b>
          </div>
          <div className={styles.row}>
            <span className={styles.rowLabel}>{t('about.row.buildTime')}</span>
            <b className={styles.rowValue}>{status?.buildTime ? formatDateTime(status.buildTime) : t('about.value.none')}</b>
          </div>
          <div className={styles.row}>
            <span className={styles.rowLabel}>{t('about.row.buildRevision')}</span>
            <b className={styles.rowValue}>
              {status?.buildRevision ? `${status.buildRevision}${status.buildModified ? t('about.row.dirty') : ''}` : t('about.value.none')}
            </b>
          </div>
          <div className={styles.row}>
            <span className={styles.rowLabel}>{t('about.row.license')}</span>
            <b className={styles.rowValue}>{LICENSE_NAME}</b>
          </div>
          <div className={styles.row}>
            <span className={styles.rowLabel}>{t('about.row.dataDir')}</span>
            <code className={styles.rowCode}>{value(status?.configPath)}</code>
          </div>
        </div>
      </div>
    </>
  )
}
