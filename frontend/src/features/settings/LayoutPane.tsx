import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { Segmented, type SegmentOption } from '../../components/ui/Segmented'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import styles from './SettingsDialog.module.css'

// 预设值抄原型 :885–901；可拖范围由后端 internal/config 的 SidebarWidthMin/Max 兜底。
const SIDEBAR_PRESETS = (): SegmentOption[] => [
  { value: 240, label: t('settings.layout.narrow') },
  { value: 340, label: t('settings.layout.mid') },
  { value: 480, label: t('settings.layout.wide') },
]

const QUEUE_PRESETS = (): SegmentOption[] => [
  { value: 180, label: t('settings.layout.small') },
  { value: 252, label: t('settings.layout.middle') },
  { value: 360, label: t('settings.layout.large') },
]

// 布局：主菜单宽度改完立刻反映到左栏；队列高度先进配置，等队列那一屏来消费。
export function LayoutPane({
  settings,
  save,
  onSidebarWidth,
}: {
  settings: Config
  save: SaveState
  onSidebarWidth: (width: number) => void
}) {
  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.layout.title')}</h3>
      <p className={styles.paneDesc}>{t('settings.layout.paneDesc')}</p>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ▤
          </span>
          {t('settings.layout.sidebarTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.layout.sidebarDesc')}</p>
        <div className={styles.row}>
          <Segmented
            label={t('settings.layout.sidebarTitle')}
            value={settings.layout.sidebarWidth}
            options={SIDEBAR_PRESETS()}
            disabled={save.kind === 'pending'}
            onChange={onSidebarWidth}
          />
          <InlineStatus kind={save.kind} text={save.text} />
        </div>
      </div>

      <div className={`${styles.card} ${styles.cardOff}`}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ▥
          </span>
          {t('settings.layout.queueTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.layout.queueDesc')}</p>
        <div className={styles.row}>
          <Segmented
            label={t('settings.layout.queueTitle')}
            value={settings.layout.queueHeight}
            options={QUEUE_PRESETS()}
            disabled
            disabledHint={t('settings.layout.queueOff')}
          />
        </div>
      </div>
    </>
  )
}
