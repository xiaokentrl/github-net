import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import styles from './SettingsDialog.module.css'

// 托盘屏：关窗行为和启动后最小化。行为本体在 main.go（关窗钩子、Hidden 启动），
// 这里只改配置；开关状态来自后端配置快照，保存失败不会显示成已保存。
export function TrayPane({
  settings,
  save,
  onCloseToTray,
  onStartMinimized,
}: {
  settings: Config
  save: SaveState
  onCloseToTray: (enabled: boolean) => void
  onStartMinimized: (enabled: boolean) => void
}) {
  const busy = save.kind === 'pending'
  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.tray.title')}</h3>
      <p className={styles.paneDesc}>{t('settings.tray.paneDesc')}</p>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⊜
          </span>
          {t('settings.tray.closeTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.tray.closeDesc')}</p>
        <label className={styles.checkRow}>
          <input
            type="checkbox"
            checked={settings.startup.closeToTray}
            disabled={busy}
            onChange={(e) => onCloseToTray(e.target.checked)}
          />
          {t('settings.tray.closeToggle')}
        </label>
        <InlineStatus kind={save.kind} text={save.text} />
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⤓
          </span>
          {t('settings.tray.startMinTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.tray.startMinDesc')}</p>
        <label className={styles.checkRow}>
          <input
            type="checkbox"
            checked={settings.startup.startMinimized}
            disabled={busy}
            onChange={(e) => onStartMinimized(e.target.checked)}
          />
          {t('settings.tray.startMinToggle')}
        </label>
        <InlineStatus kind={save.kind} text={save.text} />
      </div>
    </>
  )
}
