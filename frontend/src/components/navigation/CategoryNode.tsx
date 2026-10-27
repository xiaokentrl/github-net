import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { CatalogCategory, CatalogItem, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { StatusBadge } from '../../components/ui/StatusBadge'
import styles from './CategoryTree.module.css'

// 项目 ID → 后端这次读出来的 Hosts 状态。整条状态都留着：
// 树上的徽章只用 status，冲突和读不到的原因文本要能就地看到。
export type HostsLookup = Map<string, HostsState>

// 分类行：整行可点，点一次展开或收起；caret 只是提示，不是第二个按钮。
export function CategoryNode({
  category,
  items,
  hostsOf,
  expanded,
  selectedId,
  onToggle,
  onSelect,
}: {
  category: CatalogCategory
  items: CatalogItem[]
  hostsOf: HostsLookup
  expanded: boolean
  selectedId: string
  onToggle: (categoryID: string) => void
  onSelect: (itemID: string) => void
}) {
  return (
    <li role="none" className={styles.group}>
      <button
        type="button"
        role="treeitem"
        aria-expanded={expanded}
        className={`${styles.node} ${styles.cat}`}
        onClick={() => onToggle(category.id)}
      >
        <span className={`${styles.caret} ${expanded ? '' : styles.collapsed}`} aria-hidden="true">
          ▾
        </span>
        <span className={styles.name}>{category.name}</span>
        <span className={styles.count}>{items.length}</span>
      </button>

      {expanded && (
        <ul role="group" className={styles.children}>
          {items.map((item) => (
            <li key={item.id} role="none">
              <button
                type="button"
                role="treeitem"
                aria-selected={item.id === selectedId}
                className={`${styles.node} ${styles.item} ${item.id === selectedId ? styles.active : ''}`}
                onClick={() => onSelect(item.id)}
              >
                <span className={styles.name} title={(item.hostnames ?? []).join(', ')}>
                  {item.name}
                </span>
                <StatusBadge
                  status={hostsOf.get(item.id)?.status ?? HostsStatus.HostsUnknown}
                  reason={hostsOf.get(item.id)?.message}
                />
              </button>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}
