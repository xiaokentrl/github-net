import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { Segmented, type SegmentOption } from '../../components/ui/Segmented'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import { saveStateFor } from '../../hooks/useSettingsSave'
import styles from './SettingsDialog.module.css'

// 配置里存的是倍数（0.9 / 1 / 1.1 / 1.25，后端 internal/config 的 allowedZooms 只收这四个），
// 界面上说的是百分比，所以这里按百分比出预设、发出去再除回 100。
const ZOOM_PRESETS = (): SegmentOption[] => [
  { value: 90, label: '90%' },
  { value: 100, label: '100%' },
  { value: 110, label: '110%' },
  { value: 125, label: '125%' },
]

// 缩放：一档点一下即写盘并当场作用到整个界面（原型第 1627–1631 行就是对 body 设 CSS zoom）。
export function ZoomPane({
  settings,
  save,
  onZoom,
}: {
  settings: Config
  save: SaveState
  onZoom: (factor: number) => void
}) {
  // 状态条只认「界面缩放」这一条改动，别屏的提示不往这儿串。
  const zoomSave = saveStateFor(save, 'zoom')
  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.zoom.title')}</h3>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⤢
          </span>
          {t('settings.zoom.cardTitle')}
        </div>
        <div className={styles.row}>
          <Segmented
            label={t('settings.zoom.cardTitle')}
            value={Math.round(settings.appearance.zoom * 100)}
            options={ZOOM_PRESETS()}
            formatValue={(value) => `${value}%`}
            disabled={save.kind === 'pending'}
            onChange={(percent) => onZoom(percent / 100)}
          />
          <InlineStatus kind={zoomSave.kind} text={zoomSave.text} />
        </div>
      </div>
    </>
  )
}
