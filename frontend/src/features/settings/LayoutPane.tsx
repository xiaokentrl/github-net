import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { Segmented, type SegmentOption } from '../../components/ui/Segmented'
import { t } from '../../i18n'
import type { SaveState } from '../../hooks/useSettingsSave'
import { saveStateFor } from '../../hooks/useSettingsSave'
import styles from './SettingsDialog.module.css'

// 侧栏三档抄原型 :885–901，队列高度三档抄 :971–979，任务列表三档抄 :983–995；
// 每档都必须在后端范围内，这条由 internal/config/layout_bounds_parity_test.go 扫键名数档数钉住。
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

// 任务列表列宽预设抄原型 :983–995。文案键用 list* 而不是 narrow/mid/wide：
// 侧栏那三档由 internal/config/layout_bounds_parity_test.go 按键名扫，混名会让它数到 6 档。
const LIST_PRESETS = (): SegmentOption[] => [
  { value: 180, label: t('settings.layout.listNarrow') },
  { value: 220, label: t('settings.layout.listMid') },
  { value: 300, label: t('settings.layout.listWide') },
]

// 布局：主菜单宽度、队列高度、任务列表宽度都是点一下即写盘并当场反映到界面（也都能拖分隔条取任意值）。
export function LayoutPane({
  settings,
  save,
  onSidebarWidth,
  onQueueHeight,
  onQueueListWidth,
}: {
  settings: Config
  save: SaveState
  onSidebarWidth: (width: number) => void
  onQueueHeight: (height: number) => void
  onQueueListWidth: (width: number) => void
}) {
  // 一屏三张能改的卡：状态条各认各的改动，「禁用」仍看全局是否有请求在路上。
  const sidebarSave = saveStateFor(save, 'sidebar')
  const queueSave = saveStateFor(save, 'queueHeight')
  const listSave = saveStateFor(save, 'queueList')
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
          <InlineStatus kind={sidebarSave.kind} text={sidebarSave.text} />
        </div>
      </div>

      <div className={styles.card}>
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
            disabled={save.kind === 'pending'}
            onChange={onQueueHeight}
          />
          <InlineStatus kind={queueSave.kind} text={queueSave.text} />
        </div>
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ▦
          </span>
          {t('settings.layout.listTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.layout.listDesc')}</p>
        <div className={styles.row}>
          <Segmented
            label={t('settings.layout.listTitle')}
            value={settings.layout.queueListWidth}
            options={LIST_PRESETS()}
            disabled={save.kind === 'pending'}
            onChange={onQueueListWidth}
          />
          <InlineStatus kind={listSave.kind} text={listSave.text} />
        </div>
      </div>
    </>
  )
}
