import type { CSSProperties, ReactNode } from 'react'
import { Window } from '@wailsio/runtime'
import { HostsAccess } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { AppStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { SearchBox } from '../navigation/SearchBox'
import { SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH, useSidebarResize } from './useSidebarResize'
import styles from './AppShell.module.css'
import treeStyles from '../navigation/CategoryTree.module.css'

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

// 浏览器预览里没有 Wails 运行时，窗口操作会失败；安静接住。原生窗口里这是正常路径。
function runWindowAction(action: () => Promise<void>) {
  action().catch(() => {})
}

// 骨架：自绘标题栏 + 工具栏槽 + 左树右详情 + 队列槽 + 底部状态栏槽。
// 窗口去掉了系统标题栏（main.go 的 Frameless），顶栏右侧这三颗按钮就是真的最小化／最大化／关闭；
// 「检查更新」按钮要等 CheckForUpdates 接好才出现，这里不放假入口。
export function AppShell({
  appName,
  status,
  statusLoad,
  statusError,
  onRetryStatus,
  sidebarWidth,
  onSidebarResize,
  searchQuery,
  onSearchQueryChange,
  toolbar,
  sidebar,
  sidebarTools,
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
  searchQuery: string
  onSearchQueryChange: (value: string) => void
  toolbar: ReactNode
  sidebar: ReactNode
  sidebarTools: ReactNode
  sidebarFooter: ReactNode
  detail: ReactNode
  queue: ReactNode
  statusbar: ReactNode
  updateLatest: string
  onOpenUpdate: () => void
}) {
  const resize = useSidebarResize(sidebarWidth, onSidebarResize)
  // 拖动期间用本地预览值，松手提交后由配置快照接管。
  const shownWidth = resize.liveValue ?? sidebarWidth
  // 可拖范围的唯一前端来源是 useSidebarResize 那两个常量，这里当场写成变量给 CSS 用，
  // 免得同一对数字在 CSS 里再抄一遍（抄了就会漂；漂了由后端的相等断言拦）。
  // 后端没给宽度时不写 --sidebar-w，让 tokens.css 的默认值生效，避免第二份「默认 240」。
  // 宽度变量挂在应用根上：只给左侧类目树用。
  // 队列面板里「任务列表」那一栏另有自己的配置宽度（queueListWidth，TaskPanel 内部拖），
  // 两者不再共用，所以这两根分隔线不保证垂直对齐——原型也是这样分的（.queue-left 按队列区宽度算上限）。
  const appStyle = {
    '--sidebar-min-w': `${SIDEBAR_MIN_WIDTH}px`,
    '--sidebar-max-w': `${SIDEBAR_MAX_WIDTH}px`,
  } as CSSProperties
  if (shownWidth !== null) (appStyle as Record<string, string>)['--sidebar-w'] = `${shownWidth}px`
  // 侧栏三档（紧促/标准/宽松）不只是宽度数字：行高、左右内边距、字号一起变，
  // 每一档都是一个真实的紧凑度（拍板 2026-10-31）。拖到任意宽度时按落在哪一段取哪一档的密度；
  // 后端没给宽度时不写这几个变量，让 tokens.css 的默认值（标准档）生效。
  // 树的样式只消费变量（CategoryTree.module.css），两边的默认值各留一份会漂——所以这里写满三档、不靠 CSS 兜底。
  if (shownWidth !== null) {
    const density =
      shownWidth <= 280
        ? { '--node-h': '26px', '--tree-pad-x': '8px', '--tree-font': '12px' }
        : shownWidth <= 420
          ? { '--node-h': '32px', '--tree-pad-x': '12px', '--tree-font': '13px' }
          : { '--node-h': '38px', '--tree-pad-x': '16px', '--tree-font': '14px' }
    Object.assign(appStyle as Record<string, string>, density)
  }
  const sidebarStyle = {} as CSSProperties

  return (
    <div className={styles.app} style={appStyle}>
      <header
        className={styles.topbar}
        onDoubleClick={(e) => {
          if ((e.target as HTMLElement).closest('button')) return
          runWindowAction(() => Window.ToggleMaximise())
        }}
      >
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
        <button
          type="button"
          className={`${styles.updateBtn} ${updateLatest !== '' ? styles.hasUpdate : ''}`}
          title={updateLatest !== '' ? t('topbar.update.badge', { version: updateLatest }) : t('topbar.update.checkHint')}
          onClick={onOpenUpdate}
        >
          <span className={styles.updIco} aria-hidden="true">⟳</span>
          <span className={styles.updLbl}>{t('topbar.update.check')}</span>
          <span className={styles.updDot} aria-hidden="true" />
        </button>
        <div className={styles.winBtns}>
          <button
            type="button"
            className={styles.winBtn}
            title={t('topbar.window.minimise')}
            aria-label={t('topbar.window.minimise')}
            onClick={() => runWindowAction(() => Window.Minimise())}
          >
            —
          </button>
          <button
            type="button"
            className={styles.winBtn}
            title={t('topbar.window.maximise')}
            aria-label={t('topbar.window.maximise')}
            onClick={() => runWindowAction(() => Window.ToggleMaximise())}
          >
            □
          </button>
          <button
            type="button"
            className={styles.winBtn}
            title={t('topbar.window.close')}
            aria-label={t('topbar.window.close')}
            onClick={() => runWindowAction(() => Window.Close())}
          >
            ×
          </button>
        </div>
      </header>

      <div className={styles.toolbar}>{toolbar}</div>

      <main className={styles.main}>
        <aside ref={resize.targetRef} className={styles.sidebar} style={sidebarStyle}>
          {/* 搜索框住在菜单里、占满菜单宽：拖分隔条或换档时它跟着变宽变窄（拍板 2026-10-31）。 */}
          <div className={treeStyles.sideSearch}>
            <SearchBox value={searchQuery} onChange={onSearchQueryChange} />
          </div>
          <div className={styles.sidebarTools}>{sidebarTools}</div>
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
        <div className={styles.right}>
          <section className={styles.detail}>{detail}</section>
          {/* 队列槽就在详情下面、侧栏右侧——原型 .right(229) 的竖向顺序：详情、高度分隔条、队列。
              分隔条和收起是队列面板自己的事，所以由 queue 这一份内容自己渲染，这里不套多余的盒子。 */}
          {queue}
        </div>
      </main>

      <footer className={styles.statusbar}>{statusbar}</footer>
    </div>
  )
}
