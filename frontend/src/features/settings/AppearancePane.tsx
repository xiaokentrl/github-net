import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { Segmented } from '../../components/ui/Segmented'
import { ThemeGrid } from '../theme/ThemeGrid'
import { themeName } from '../theme/themes'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import { saveStateFor } from '../../hooks/useSettingsSave'
import styles from './SettingsDialog.module.css'

// 外观：语言两颗按钮（简/繁，拍板 2026-10-31），主题点一下就换肤并写盘。
export function AppearancePane({
  settings,
  save,
  onTheme,
  onLanguage,
}: {
  settings: Config
  save: SaveState
  onTheme: (id: string) => void
  onLanguage: (language: string) => void
}) {
  // 状态条只认「主题」这一条改动，别屏的提示不往这儿串。
  const themeSave = saveStateFor(save, 'theme')
  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.appearance.title')}</h3>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ◐
          </span>
          {t('settings.appearance.langTitle')}
        </div>
        <div className={styles.row}>
          <Segmented
            label={t('settings.appearance.langTitle')}
            value={settings.appearance.language}
            options={[
              { value: 'zh-CN', label: t('settings.appearance.lang.zhCN') },
              { value: 'zh-TW', label: t('settings.appearance.lang.zhTW') },
            ]}
            disabled={save.kind === 'pending'}
            onChange={onLanguage}
          />
          <InlineStatus kind={save.kind} text={save.text} />
        </div>
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ◑
          </span>
          {t('settings.appearance.themeTitle')}
        </div>
        <div className={styles.row}>
          <span className={styles.currentTheme}>
            {t('settings.appearance.themeCurrent', { name: themeName(settings.appearance.theme) })}
          </span>
          <InlineStatus kind={themeSave.kind} text={themeSave.text} />
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
