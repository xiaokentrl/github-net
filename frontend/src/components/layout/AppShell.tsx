import type { CSSProperties, ReactNode } from 'react'
import { HostsAccess } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { AppStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import styles from './AppShell.module.css'

// 存文案键而不是成品句子：语言来自后端设置，切语言时下一屏要跟着变。
const PERM_KEY: Record<HostsAccess, string> = {
  [HostsAccess.$zero]: 'topbar.perm.unknown',
  [HostsAccess.HostsAccessWritable]: 'topbar.perm.writable',
  [HostsAccess.HostsAccessReadOnly]: 'topbar.perm.readOnly',
  [HostsAccess.HostsAccessUnknown]: 'topbar.perm.unknown',
}

const PERM_HINT_KEY: Record<HostsAccess, string> = {
  [HostsAccess.$zero]: 'topbar.perm.hint.unknown',
  [HostsAccess.HostsAccessWritable]: 'topbar.perm.hint.writable',
  [HostsAccess.HostsAccessReadOnly]: 'topbar.perm.hint.readOnly',
  [HostsAccess.HostsAccessUnknown]: 'topbar.perm.hint.unknown',
}

// 骨架：顶栏 + 工具栏槽 + 左树右详情 + 底部状态栏槽。
// 「检查更新」按钮和自定义窗口按钮要等 CheckForUpdates 接好才出现，这里不放假入口。
export function AppShell({
  appName,
  status,
  statusLoad,
  statusError,
  onRetryStatus,
  sidebarWidth,
  toolbar,
  sidebar,
  detail,
  statusbar,
}: {
  appName: string
  status: AppStatus | null
  statusLoad: 'loading' | 'ready' | 'error'
  statusError: string
  onRetryStatus: () => void
  sidebarWidth: number | null
  toolbar: ReactNode
  sidebar: ReactNode
  detail: ReactNode
  statusbar: ReactNode
}) {
  // 后端没给布局数值时不写这个变量，让 tokens.css 的默认值生效，避免第二份「默认 240」。
  const sidebarStyle = sidebarWidth === null ? undefined : ({ '--sidebar-w': `${sidebarWidth}px` } as CSSProperties)

  return (
    <div className={styles.app}>
      <header className={styles.topbar}>
        <span className={styles.logo} aria-hidden="true">
          GN
        </span>
        <h1 className={styles.title}>{appName}</h1>
        <span className={styles.spacer} />
        {statusLoad === 'loading' && <span className={styles.perm}>{t('detail.loading')}</span>}
        {statusLoad === 'error' && (
          <span className={`${styles.perm} ${styles.warnPerm}`}>
            {t('topbar.statusError', { reason: statusError })}
            <button type="button" onClick={onRetryStatus}>
              {t('common.retry')}
            </button>
          </span>
        )}
        {statusLoad === 'ready' && status && (
          <span
            className={`${styles.perm} ${status.hostsAccess === HostsAccess.HostsAccessWritable ? '' : styles.warn}`}
            title={t(PERM_HINT_KEY[status.hostsAccess])}
          >
            <i className={styles.dot} aria-hidden="true" />
            {t('topbar.perm.prefix', { access: t(PERM_KEY[status.hostsAccess]) })}
          </span>
        )}
      </header>

      <div className={styles.toolbar}>{toolbar}</div>

      <main className={styles.main}>
        <aside className={styles.sidebar} style={sidebarStyle}>
          {sidebar}
        </aside>
        <section className={styles.detail}>{detail}</section>
      </main>

      <footer className={styles.statusbar}>{statusbar}</footer>
    </div>
  )
}
