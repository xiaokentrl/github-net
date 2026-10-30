import type { CatalogCategory, CatalogItem, DetectResultView } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { EmptyState } from '../../components/ui/EmptyState'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { CategoryNode, type HostsLookup, type LiveLookup } from './CategoryNode'
import styles from './CategoryTree.module.css'

export type TreeLoad = 'loading' | 'ready' | 'error'

// 原型的搜索口径（第 2326-2330 行）：命中分类名就整类露出来，没命中分类名才逐条筛项目名和域名；
// 两边都不沾的分类整块不显示。数量徽章始终显示这一类的总项数，不跟着筛选变（第 2370 行用的是 cat.items.length）。
function visibleItems(category: CatalogCategory, needle: string, catMatched: boolean): CatalogItem[] {
  const items = category.items ?? []
  if (needle === '' || catMatched) return items
  return items.filter(
    (item) =>
      item.name.toLowerCase().includes(needle) ||
      (item.hostnames ?? []).some((hostname) => hostname.toLowerCase().includes(needle)),
  )
}

// 左侧目录树：数据全部来自后端 GetItems，这里不保存第二份清单。
// 队列状态（liveOf）和最近检测结果（results）由上层传进来，树只负责把它们画成原型那套色点、状态字和整行高亮。
export function CategoryTree({
  categories,
  hostsOf,
  liveOf,
  results,
  query,
  expanded,
  selectedId,
  load,
  error,
  onRetry,
  onToggle,
  onSelect,
  onRestoreItems,
}: {
  categories: CatalogCategory[]
  hostsOf: HostsLookup
  liveOf: LiveLookup
  results: Record<string, DetectResultView>
  query: string
  expanded: ReadonlySet<string>
  selectedId: string
  load: TreeLoad
  error: string
  onRetry: () => void
  onToggle: (categoryID: string) => void
  onSelect: (itemID: string) => void
  onRestoreItems: (itemIDs: string[]) => void
}) {
  if (load === 'loading') return <EmptyState message={t('tree.loading')} />
  if (load === 'error') return <ErrorMessage reason={t('tree.error', { reason: error })} onRetry={onRetry} />

  const needle = query.trim().toLowerCase()
  // 搜索时按命中结果显示分类，退出的瞬间恢复用户自己的展开状态。
  // items 是筛选后要显示的行；分类自己的总数、整行高亮和 ↺ 都按全量算，和原型一致。
  const visible = categories
    .map((category) => ({ category, items: visibleItems(category, needle, category.name.toLowerCase().includes(needle)) }))
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
          liveOf={liveOf}
          results={results}
          needle={needle}
          expanded={needle !== '' || expanded.has(category.id)}
          selectedId={selectedId}
          onToggle={onToggle}
          onSelect={onSelect}
          onRestoreItems={onRestoreItems}
        />
      ))}
    </ul>
  )
}
