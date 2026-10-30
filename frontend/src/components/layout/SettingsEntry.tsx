import { t } from '../../i18n'
import styles from './AppShell.module.css'

// 侧栏底部两颗按钮之一：打开设置中心；另一颗「同步状态」见 SyncEntry。
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
