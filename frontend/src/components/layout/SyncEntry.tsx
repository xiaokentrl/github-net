import { t } from '../../i18n'
import styles from './AppShell.module.css'

// 侧栏底部第二颗按钮：同步状态。原型里那颗叫「重算演示状态」（演示假数据专用），
// 工程里对应的是「把界面状态和后端重新对齐」。本轮只出按钮界面，同步动作下一轮接
// （接的时候走 loadCatalog + loadStatus 那两条既有链路，不新建通道）。
export function SyncEntry() {
  return (
    <button type="button" className={styles.sfBtn} title={t('sync.entry.hint')} onClick={() => undefined}>
      <span className={styles.ico} aria-hidden="true">
        ⟳
      </span>
      {t('sync.entry')}
    </button>
  )
}
