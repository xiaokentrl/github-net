import { t } from '../../i18n'
import styles from './AppShell.module.css'

// 侧栏顶部三颗按钮：全选/全不选、反选、折叠/展开所有类目。
// 原型第 821–825 行把它们放在 .sidebar 里、类目树上面；外层那道壳是 AppShell 的 sidebarTools 槽，
// 这个组件只出按钮，不再套一层，否则 8px 内边距会叠成两圈。
export function SidebarTools({
  allChecked,
  busy,
  onCheckAll,
  onInvertChecked,
  onFoldAll,
}: {
  allChecked: boolean
  busy: boolean
  onCheckAll: (checked: boolean) => void
  onInvertChecked: () => void
  onFoldAll: () => void
}) {
  return (
    <>
      <button
        type="button"
        className={styles.action}
        disabled={busy}
        title={t('toolbar.checkAllTitle')}
        onClick={() => onCheckAll(!allChecked)}
      >
        {allChecked ? t('toolbar.uncheckAll') : t('toolbar.checkAll')}
      </button>
      <button type="button" className={styles.action} disabled={busy} onClick={onInvertChecked}>
        {t('toolbar.invertChecked')}
      </button>
      <button type="button" className={styles.action} disabled={busy} title={t('toolbar.foldTitle')} onClick={onFoldAll}>
        {t('toolbar.fold')}
      </button>
    </>
  )
}
