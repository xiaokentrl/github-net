import type { CSSProperties, ReactNode } from 'react'
import { HostsAccess } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { AppStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH, useSidebarResize } from './useSidebarResize'
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

// 骨架：顶栏 + 工具栏槽 + 左树右详情 + 队列槽 + 底部状态栏槽。
// 「检查更新」按钮和自定义窗口按钮要等 CheckForUpdates 接好才出现，这里不放假入口。
export function AppShell({
  appName,
  status,
  statusLoad,
  statusError,
  onRetryStatus,
  sidebarWidth,
  onSidebarResize,
  toolbar,
  sidebar,
  sidebarFooter,
  detail,
  queue,
  statusbar,
  updateLatest,
  onOpenUpdate,
}: {
  appName: string
  status: AppStatus | null
  statusLoad: 'loading' | 'ready' | 'error'
  statusError: string
  onRetryStatus: () => void
  sidebarWidth: number | null
  onSidebarResize: (width: number) => void
  toolbar: ReactNode
  sidebar: ReactNode
  sidebarFooter: ReactNode
  detail: ReactNode
  queue: ReactNode
  statusbar: ReactNode
  updateLatest: string
  onOpenUpdate: () => void
}) {
  const resize = useSidebarResize(sidebarWidth, onSidebarResize)
  // 拖动期间用本地预览值，松手提交后由配置快照接管。
  const shownWidth = resize.liveWidth ?? sidebarWidth
  // 可拖范围的唯一前端来源是 useSidebarResize 那两个常量，这里当场写成变量给 CSS 用，
  // 免得同一对数字在 CSS 里再抄一遍（抄了就会漂；漂了由后端的相等断言拦）。
  // 后端没给宽度时不写 --sidebar-w，让 tokens.css 的默认值生效，避免第二份「默认 240」。
  // 宽度变量挂在应用根上：左树和下方任务队列的左栏共用同一个 --sidebar-w，
  // 两根分隔线永远垂直对齐（否则队列左栏和菜单错位）。
  const appStyle = {
    '--sidebar-min-w': `${SIDEBAR_MIN_WIDTH}px`,
    '--sidebar-max-w': `${SIDEBAR_MAX_WIDTH}px`,
  } as CSSProperties
  if (shownWidth !== null) (appStyle as Record<string, string>)['--sidebar-w'] = `${shownWidth}px`
  const sidebarStyle = {} as CSSProperties

  return (
    <div className={styles.app} style={appStyle}>
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
        {updateLatest !== '' && (
          <button type="button" className={styles.updateBadge} title={t('topbar.update.hint')} onClick={onOpenUpdate}>
            {t('topbar.update.badge', { version: updateLatest })}
          </button>
        )}
      </header>

      <div className={styles.toolbar}>{toolbar}</div>

      <main className={styles.main}>
        <aside ref={resize.targetRef} className={styles.sidebar} style={sidebarStyle}>
          <div className={styles.treeScroll}>{sidebar}</div>
          <div className={styles.sideFoot}>{sidebarFooter}</div>
        </aside>
        {sidebarWidth !== null && (
          <div
            className={`${styles.resizer} ${resize.dragging ? styles.resizerDragging : ''}`}
            role="separator"
            aria-orientation="vertical"
            title={t('layout.resizer.title')}
            onPointerDown={resize.onPointerDown}
            onPointerMove={resize.onPointerMove}
            onPointerUp={resize.onPointerUp}
            onPointerCancel={resize.onPointerUp}
          />
        )}
        <section className={styles.detail}>{detail}</section>
      </main>

      {queue}
      <footer className={styles.statusbar}>{statusbar}</footer>
    </div>
  )
}
