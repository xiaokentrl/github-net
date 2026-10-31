import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import { saveStateFor } from '../../hooks/useSettingsSave'
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
  // 两个开关两张卡：状态条只跟被自己改的那次走。
  const closeSave = saveStateFor(save, 'closeToTray')
  const startMinSave = saveStateFor(save, 'startMinimized')
  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.tray.title')}</h3>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⊜
          </span>
          {t('settings.tray.closeTitle')}
        </div>
        <label className={styles.checkRow}>
          <input
            type="checkbox"
            checked={settings.startup.closeToTray}
            disabled={busy}
            onChange={(e) => onCloseToTray(e.target.checked)}
          />
          {t('settings.tray.closeToggle')}
        </label>
        <InlineStatus kind={closeSave.kind} text={closeSave.text} />
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⤓
          </span>
          {t('settings.tray.startMinTitle')}
        </div>
        <label className={styles.checkRow}>
          <input
            type="checkbox"
            checked={settings.startup.startMinimized}
            disabled={busy}
            onChange={(e) => onStartMinimized(e.target.checked)}
          />
          {t('settings.tray.startMinToggle')}
        </label>
        <InlineStatus kind={startMinSave.kind} text={startMinSave.text} />
      </div>
    </>
  )
}
