import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { Select } from '../../components/ui/Select'
import { ThemeGrid } from '../theme/ThemeGrid'
import { themeName } from '../theme/themes'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import styles from './SettingsDialog.module.css'

// 外观：主题点一下就换肤并写盘；语言这一项整组禁用，因为后端只收 zh-CN，
// 繁体中文的界面翻译还没补齐，给一个点了必被拒的入口等于骗人。
export function AppearancePane({
  settings,
  save,
  onTheme,
}: {
  settings: Config
  save: SaveState
  onTheme: (id: string) => void
}) {
  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.appearance.title')}</h3>
      <p className={styles.paneDesc}>{t('settings.appearance.paneDesc')}</p>

      <div className={`${styles.card} ${styles.cardOff}`}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ◐
          </span>
          {t('settings.appearance.langTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.appearance.langDesc')}</p>
        <div className={styles.row}>
          <Select
            label={t('settings.appearance.langTitle')}
            value={settings.appearance.language}
            options={[{ value: 'zh-CN', label: t('settings.appearance.lang.zhCN') }]}
            disabled
            disabledHint={t('settings.appearance.langOff')}
          />
        </div>
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ◑
          </span>
          {t('settings.appearance.themeTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.appearance.themeDesc')}</p>
        <div className={styles.row}>
          <span className={styles.currentTheme}>
            {t('settings.appearance.themeCurrent', { name: themeName(settings.appearance.theme) })}
          </span>
          <InlineStatus kind={save.kind} text={save.text} />
        </div>
        <ThemeGrid
          current={settings.appearance.theme}
          disabled={save.kind === 'pending'}
          onSelect={onTheme}
        />
      </div>
    </>
  )
}
