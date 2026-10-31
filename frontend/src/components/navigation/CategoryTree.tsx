import type { CatalogCategory, DetectResultView } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { EmptyState } from '../../components/ui/EmptyState'
import { ErrorMessage } from '../../components/ui/ErrorMessage'
import { CategoryNode, type HostsLookup, type LiveLookup } from './CategoryNode'
import styles from './CategoryTree.module.css'

export type TreeLoad = 'loading' | 'ready' | 'error'

// 左侧目录树：数据全部来自后端 GetItems，这里不保存第二份清单。
// 队列状态（liveOf）和最近检测结果（results）由上层传进来，树只负责把它们画成原型那套色点、状态字和整行高亮。
// 搜索框已删（拍板 2026-10-31）：52 个项目一屏分类装得下，不需要筛选器。
export function CategoryTree({
  categories,
  hostsOf,
  liveOf,
  results,
  expanded,
  selectedId,
  checkedIds,
  onToggleChecked,
  onManualBrowse,
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
  expanded: ReadonlySet<string>
  selectedId: string
  load: TreeLoad
  error: string
  onRetry: () => void
  onToggle: (categoryID: string) => void
  onSelect: (itemID: string) => void
  checkedIds: ReadonlySet<string>
  onToggleChecked: (itemID: string, checked: boolean) => void
  onManualBrowse?: () => void
  onRestoreItems: (itemIDs: string[]) => void
}) {
  if (load === 'loading') return <EmptyState message={t('tree.loading')} />
  if (load === 'error') return <ErrorMessage reason={t('tree.error', { reason: error })} onRetry={onRetry} />

  if (categories.length === 0) return <EmptyState message={t('tree.empty')} />

  return (
    <ul role="tree" aria-label={t('tree.ariaLabel')} className={styles.tree}>
      {categories.map((category) => (
        <CategoryNode
          key={category.id}
          category={category}
          items={category.items ?? []}
          hostsOf={hostsOf}
          liveOf={liveOf}
          results={results}
          expanded={expanded.has(category.id)}
          selectedId={selectedId}
          onToggle={onToggle}
          onSelect={onSelect}
          checkedIds={checkedIds}
          onToggleChecked={onToggleChecked}
          onManualBrowse={onManualBrowse}
          onRestoreItems={onRestoreItems}
        />
      ))}
    </ul>
  )
}
