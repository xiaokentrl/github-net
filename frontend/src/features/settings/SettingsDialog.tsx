import { useEffect, useRef } from 'react'
import type { AppStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { NetworkProfile } from '../../api/client'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import { AboutPane } from './AboutPane'
import { AppearancePane } from './AppearancePane'
import { LayoutPane } from './LayoutPane'
import { NetworkPane } from './NetworkPane'
import { UpdatesPane } from './UpdatesPane'
import { ZoomPane } from './ZoomPane'
import styles from './SettingsDialog.module.css'

export type SettingsPaneID = 'layout' | 'zoom' | 'appearance' | 'network' | 'updates' | 'about'

// 真正做出界面来的屏才进导航：布局、界面缩放、外观、网络与流量、检测升级、关于我们。
// 托盘和开机自启两屏还没迁（它们的行为属托盘/自启动那一步），这里不放点了没反应的入口。
const PANES: { id: SettingsPaneID; group: string; label: string; ico: string }[] = [
  { id: 'layout', group: 'settings.nav.groupPref', label: 'settings.nav.layout', ico: '▦' },
  { id: 'zoom', group: 'settings.nav.groupPref', label: 'settings.nav.zoom', ico: '⤢' },
  { id: 'appearance', group: 'settings.nav.groupPref', label: 'settings.nav.appearance', ico: '◐' },
  { id: 'network', group: 'settings.nav.groupNetwork', label: 'settings.nav.network', ico: '⌁' },
  { id: 'updates', group: 'settings.nav.groupInfo', label: 'settings.nav.updates', ico: '⇪' },
  { id: 'about', group: 'settings.nav.groupInfo', label: 'settings.nav.about', ico: 'ⓘ' },
]

const PANE_SUB: Record<SettingsPaneID, string> = {
  layout: 'settings.layout.sub',
  zoom: 'settings.zoom.sub',
  appearance: 'settings.appearance.sub',
  network: 'settings.network.sub',
  updates: 'settings.updates.sub',
  about: 'settings.about.sub',
}

export function SettingsDialog({
  pane,
  settings,
  hostnames,
  status,
  save,
  onPaneChange,
  onClose,
  onProfile,
  onBudget,
  onSidebarWidth,
  onZoom,
  onTheme,
}: {
  pane: SettingsPaneID
  settings: Config
  hostnames: number
  status: AppStatus | null
  save: SaveState
  onPaneChange: (pane: SettingsPaneID) => void
  onClose: () => void
  onProfile: (profile: NetworkProfile) => void
  onBudget: (value: number) => void
  onSidebarWidth: (width: number) => void
  onZoom: (factor: number) => void
  onTheme: (id: string) => void
}) {
  const closeRef = useRef<HTMLButtonElement>(null)

  // 打开即把焦点放进对话框（聚焦关闭按钮，实测聚焦面板本身在 WebView 里落不住）；
  // Esc 听在 document 上，这样焦点没进对话框时键盘也能退出。
  useEffect(() => {
    closeRef.current?.focus()
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className={styles.mask} onClick={(event) => { if (event.target === event.currentTarget) onClose() }}>
      <div
        className={styles.panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-title"
        tabIndex={-1}
      >
        <div className={styles.head}>
          <h2 id="settings-title">{t('settings.dialog.title')}</h2>
          <span className={styles.sub}>{t(PANE_SUB[pane])}</span>
          <span className={styles.spacer} />
          <button ref={closeRef} type="button" className={styles.close} aria-label={t('settings.close')} title={t('settings.close')} onClick={onClose}>
            ×
          </button>
        </div>

        <div className={styles.body}>
          <nav className={styles.nav} aria-label={t('settings.dialog.title')}>
            {PANES.map((entry, index) => (
              <div key={entry.id}>
                {(index === 0 || PANES[index - 1].group !== entry.group) && (
                  <div className={styles.navGroup}>{t(entry.group)}</div>
                )}
                <button
                  type="button"
                  className={`${styles.navItem} ${pane === entry.id ? styles.navOn : ''}`}
                  aria-current={pane === entry.id ? 'page' : undefined}
                  onClick={() => onPaneChange(entry.id)}
                >
                  <span className={styles.ico} aria-hidden="true">
                    {entry.ico}
                  </span>
                  {t(entry.label)}
                </button>
              </div>
            ))}
          </nav>

          <div className={styles.content}>
            {pane === 'network' && (
              <NetworkPane settings={settings} hostnames={hostnames} save={save} onProfile={onProfile} onBudget={onBudget} />
            )}
            {pane === 'layout' && <LayoutPane settings={settings} save={save} onSidebarWidth={onSidebarWidth} />}
            {pane === 'zoom' && <ZoomPane settings={settings} save={save} onZoom={onZoom} />}
            {pane === 'appearance' && <AppearancePane settings={settings} save={save} onTheme={onTheme} />}
            {pane === 'updates' && <UpdatesPane status={status} />}
            {pane === 'about' && <AboutPane status={status} />}
          </div>
        </div>

        <div className={styles.foot}>
          {/* 每个控件改完就保存了，所以这里只有关闭，没有「保存」——留着会误导成还没生效。 */}
          <button type="button" className={styles.footBtn} onClick={onClose}>
            {t('settings.close')}
          </button>
        </div>
      </div>
    </div>
  )
}
