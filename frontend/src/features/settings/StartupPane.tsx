import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { MultiSelect, type MultiSelectOption } from '../../components/ui/MultiSelect'
import uiStyles from '../../components/ui/ui.module.css'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import { saveStateFor } from '../../hooks/useSettingsSave'
import styles from './SettingsDialog.module.css'

// 开机自启动屏：开关改配置，真正的桌面入口由后端平台层写/删。
// 配置保存成功而入口没改成时，后端会明确报错，这里把那句话原样亮出来。
//
// 下面三张卡是启动后那条自动链（检测 → 应用 → 退出）。可用性由配置本身决定，不由点击顺序决定：
// 「自动应用」要勾了主菜单才可用，「跑完退出」要开了自动应用才可用。
// 上游被清空时同一次保存就把下游一起落回关闭，界面上不会出现「灰着却还开着」的配置。
export function StartupPane({
  settings,
  save,
  menus,
  onAutoStart,
  onAutoDetectMenus,
  onAutoApply,
  onAutoClose,
}: {
  settings: Config
  save: SaveState
  menus: MultiSelectOption[]
  onAutoStart: (enabled: boolean) => void
  onAutoDetectMenus: (menus: string[]) => void
  onAutoApply: (enabled: boolean) => void
  onAutoClose: (enabled: boolean) => void
}) {
  const busy = save.kind === 'pending'
  // 状态条只认自己那张卡的改动，别屏的提示不往这儿串。
  const autoStartSave = saveStateFor(save, 'autoStart')
  const detectSave = saveStateFor(save, 'autoDetectMenus')
  const applySave = saveStateFor(save, 'autoApplyOnStartup')
  const closeSave = saveStateFor(save, 'autoCloseAfterApply')

  const selected = settings.startup.autoDetectMenus ?? []
  const applyUsable = selected.length > 0
  const closeUsable = applyUsable && settings.startup.autoApplyOnStartup

  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.startup.title')}</h3>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⏻
          </span>
          {t('settings.startup.autoTitle')}
        </div>
        <label className={styles.checkRow}>
          <input
            type="checkbox"
            checked={settings.startup.autoStart}
            disabled={busy}
            onChange={(e) => onAutoStart(e.target.checked)}
          />
          {t('settings.startup.autoToggle')}
        </label>
        <InlineStatus kind={autoStartSave.kind} text={autoStartSave.text} />
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ◎
          </span>
          {t('settings.startup.detectTitle')}
        </div>
        <MultiSelect
          options={menus}
          value={selected}
          labelText={t('settings.startup.detectTitle')}
          emptyText={t('settings.startup.detectNone')}
          selectedText={t('settings.startup.detectSelected')}
          disabled={busy || menus.length === 0}
          disabledHint={menus.length === 0 ? t('settings.startup.detectNoCatalog') : undefined}
          onChange={onAutoDetectMenus}
        />
        <InlineStatus kind={detectSave.kind} text={detectSave.text} />
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ▶
          </span>
          {t('settings.startup.applyTitle')}
        </div>
        <label className={styles.checkRow}>
          <input
            type="checkbox"
            checked={settings.startup.autoApplyOnStartup}
            disabled={busy || !applyUsable}
            onChange={(e) => onAutoApply(e.target.checked)}
          />
          {t('settings.startup.applyToggle')}
        </label>
        {!applyUsable && <p className={uiStyles.hint}>{t('settings.startup.applyNeedsMenus')}</p>}
        <InlineStatus kind={applySave.kind} text={applySave.text} />
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⏻
          </span>
          {t('settings.startup.closeTitle')}
        </div>
        <label className={styles.checkRow}>
          <input
            type="checkbox"
            checked={settings.startup.autoCloseAfterApply}
            disabled={busy || !closeUsable}
            onChange={(e) => onAutoClose(e.target.checked)}
          />
          {t('settings.startup.closeToggle')}
        </label>
        {!closeUsable && (
          <p className={uiStyles.hint}>
            {t(applyUsable ? 'settings.startup.closeNeedsApply' : 'settings.startup.closeNeedsMenus')}
          </p>
        )}
        <InlineStatus kind={closeSave.kind} text={closeSave.text} />
      </div>
    </>
  )
}
