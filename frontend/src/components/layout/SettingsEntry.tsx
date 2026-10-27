import { t } from '../../i18n'
import styles from './AppShell.module.css'

// 侧栏底部的唯一入口：打开设置中心。原型这里还有一颗「重算演示状态」，
// 那是演示用的假数据来源，没有搬进工程。
export function SettingsEntry({ onOpen }: { onOpen: () => void }) {
  return (
    <button type="button" className={styles.sfBtn} title={t('settings.entry.hint')} onClick={onOpen}>
      <span className={styles.ico} aria-hidden="true">
        ⚙
      </span>
      {t('settings.entry')}
    </button>
  )
}
