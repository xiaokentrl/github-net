import { t } from '../../i18n'
import styles from './AppShell.module.css'

// 侧栏底部两颗按钮之一：同步状态——把树、详情和队列的状态与后端重新对齐。
// 动作走 App 传进来的 onSync（内部就是既有的 loadCatalog + loadStatus 两条链路，
// 不新建通道）；同步中按钮禁用并显示「同步中…」，失败由既有的错误槽位显示。
export function SyncEntry({ syncing, onSync }: { syncing: boolean; onSync: () => void }) {
  return (
    <button
      type="button"
      className={styles.sfBtn}
      title={t('sync.entry.hint')}
      disabled={syncing}
      onClick={onSync}
    >
      <span className={styles.ico} aria-hidden="true" style={syncing ? { animation: 'spin 0.8s linear infinite', display: 'inline-block' } : undefined}>
        ⟳
      </span>
      {syncing ? t('sync.entry.doing') : t('sync.entry')}
    </button>
  )
}
