import type { CatalogCategory, CatalogItem } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { EmptyState } from '../../components/ui/EmptyState'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { CategoryNode, type HostsLookup } from './CategoryNode'
import styles from './CategoryTree.module.css'

export type TreeLoad = 'loading' | 'ready' | 'error'

function matches(category: CatalogCategory, needle: string): CatalogItem[] {
  if (needle === '') return category.items
  return category.items.filter(
    (item) =>
      item.name.toLowerCase().includes(needle) ||
      item.hostnames.some((hostname) => hostname.toLowerCase().includes(needle)),
  )
}

// 左侧目录树：数据全部来自后端 GetItems，这里不保存第二份清单。
export function CategoryTree({
  categories,
  hostsOf,
  query,
  expanded,
  selectedId,
  load,
  error,
  onRetry,
  onToggle,
  onSelect,
}: {
  categories: CatalogCategory[]
  hostsOf: HostsLookup
  query: string
  expanded: ReadonlySet<string>
  selectedId: string
  load: TreeLoad
  error: string
  onRetry: () => void
  onToggle: (categoryID: string) => void
  onSelect: (itemID: string) => void
}) {
  if (load === 'loading') return <EmptyState message={t('tree.loading')} />
  if (load === 'error') return <ErrorMessage reason={t('tree.error', { reason: error })} onRetry={onRetry} />

  const needle = query.trim().toLowerCase()
  // 搜索时按命中结果显示分类，退出的瞬间恢复用户自己的展开状态。
  const visible = categories
    .map((category) => ({ category, items: matches(category, needle) }))
    .filter((entry) => entry.items.length > 0)

  if (visible.length === 0) return <EmptyState message={t('tree.empty')} />

  return (
    <ul role="tree" aria-label={t('tree.ariaLabel')} className={styles.tree}>
      {visible.map(({ category, items }) => (
        <CategoryNode
          key={category.id}
          category={category}
          items={items}
          hostsOf={hostsOf}
          expanded={needle !== '' || expanded.has(category.id)}
          selectedId={selectedId}
          onToggle={onToggle}
          onSelect={onSelect}
        />
      ))}
    </ul>
  )
}
