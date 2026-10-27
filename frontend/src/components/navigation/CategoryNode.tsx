import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { CatalogCategory, CatalogItem } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { StatusBadge } from '../../components/ui/StatusBadge'
import styles from './CategoryTree.module.css'

export type StatusLookup = Map<string, HostsStatus>

// 分类行：整行可点，点一次展开或收起；caret 只是提示，不是第二个按钮。
export function CategoryNode({
  category,
  items,
  statusOf,
  expanded,
  selectedId,
  onToggle,
  onSelect,
}: {
  category: CatalogCategory
  items: CatalogItem[]
  statusOf: StatusLookup
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
                <span className={styles.name} title={item.hostnames.join(', ')}>
                  {item.name}
                </span>
                <StatusBadge status={statusOf.get(item.id) ?? HostsStatus.HostsUnknown} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </li>
  )
}
