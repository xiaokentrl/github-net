import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import styles from './SettingsDialog.module.css'

// 开机自启动屏：开关改配置，真正的桌面入口由后端平台层写/删。
// 配置保存成功而入口没改成时，后端会明确报错，这里把那句话原样亮出来。
export function StartupPane({
  settings,
  save,
  onAutoStart,
}: {
  settings: Config
  save: SaveState
  onAutoStart: (enabled: boolean) => void
}) {
  const busy = save.kind === 'pending'
  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.startup.title')}</h3>
      <p className={styles.paneDesc}>{t('settings.startup.paneDesc')}</p>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⏻
          </span>
          {t('settings.startup.autoTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.startup.autoDesc')}</p>
        <label className={styles.checkRow}>
          <input
            type="checkbox"
            checked={settings.startup.autoStart}
            disabled={busy}
            onChange={(e) => onAutoStart(e.target.checked)}
          />
          {t('settings.startup.autoToggle')}
        </label>
        <InlineStatus kind={save.kind} text={save.text} />
      </div>
    </>
  )
}
