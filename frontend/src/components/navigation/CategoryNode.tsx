import type { CatalogCategory, CatalogItem, DetectResultView, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import type { Tone } from '../../features/tasks/statusTone'
import { itemStatus, type LiveKind } from './treeStatus'
import styles from './CategoryTree.module.css'

// 四档色点，和表格、徽章、队列同一套颜色。二级菜单行只靠行底色和色点表意，不摆状态字。
const DOT_CLASS: Record<Tone, string> = {
  unwritten: styles.dotUnset,
  written: styles.dotApplied,
  failed: styles.dotError,
  working: styles.dotPending,
}

// 项目 ID → 后端这次读出来的 Hosts 状态。整条状态都留着：
// 树上那一行只用行底色和色点，状态词与冲突／读不到的原因收进悬停提示里。
export type HostsLookup = Map<string, HostsState>

// 项目 ID → 队列里正在对它做的事；没有这一项就不在 Map 里。
export type LiveLookup = Map<string, LiveKind>

// 分类行和项目行都是整行可点的容器——
// 行本身用 div + role 而不是 button：原型同一条规则，整行一个点击区。
export function CategoryNode({
  category,
  items,
  hostsOf,
  liveOf,
  results,
  expanded,
  selectedId,
  startIndex,
  checkedIds,
  onToggleChecked,
  onManualBrowse,
  onToggle,
  onSelect,
}: {
  category: CatalogCategory
  items: CatalogItem[]
  hostsOf: HostsLookup
  liveOf: LiveLookup
  results: Record<string, DetectResultView>
  expanded: boolean
  selectedId: string
  // 这个分类在「当前展开的全部二级菜单」平坦清单里的起点，方向键跨分类导航用。
  startIndex: number
  onToggle: (categoryID: string) => void
  onSelect: (itemID: string) => void
  checkedIds: ReadonlySet<string>
  onToggleChecked: (itemID: string, checked: boolean) => void
  onManualBrowse?: () => void
}) {
  // 全量项：搜索时 items 只剩命中行，但分类总数和整行高亮都按全量算（原型第 2334-2339、2370 行用的都是 cat.items）。
  const all = category.items ?? []
  // 分类行只看底下这些项有没有「处理中」或「失败」，颜色跟着统一四档走。
  const states = all.map((item) => itemStatus(hostsOf.get(item.id), liveOf.get(item.id), results[item.id]))
  const hasWorking = states.some((s) => s.tone === 'working')
  const hasFailed = states.some((s) => s.tone === 'failed')

  return (
    <li role="none" className={styles.group}>
      <div
        role="treeitem"
        tabIndex={0}
        aria-expanded={expanded}
        className={`${styles.node} ${styles.cat} ${hasWorking ? styles.hasWorking : hasFailed ? styles.hasFailed : ''}`}
        onClick={() => onToggle(category.id)}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' && event.key !== ' ') return
          event.preventDefault()
          onToggle(category.id)
        }}
      >
        <input
          type="checkbox"
          className={styles.check}
          checked={all.every((item) => checkedIds.has(item.id)) && all.length > 0}
          ref={(el) => {
            if (el) el.indeterminate = all.length > 0 && !all.every((item) => checkedIds.has(item.id)) && all.some((item) => checkedIds.has(item.id))
          }}
          aria-label={category.name}
          onClick={(e) => e.stopPropagation()}
          onChange={(e) => {
            const catChecked = all.length > 0 && all.every((item) => checkedIds.has(item.id))
            for (const it of all) onToggleChecked(it.id, !catChecked)
          }}
        />
        <span className={`${styles.caret} ${expanded ? '' : styles.collapsed}`} aria-hidden="true">
          ▼
        </span>
        <span className={styles.name}>{category.name}</span>
        <span className={`${styles.infoArea} ${styles.infoCount}`}>
          <span className={styles.count}>{all.length}</span>
        </span>
      </div>

      {expanded && (
        <ul role="group" className={styles.children}>
          {items.map((item, index) => {
            const hosts = hostsOf.get(item.id)
            const status = itemStatus(hosts, liveOf.get(item.id), results[item.id])
            const active = item.id === selectedId
            return (
              <li key={item.id} role="none">
                <div
                  role="treeitem"
                  tabIndex={0}
                  data-item-id={item.id}
                  data-arrow-index={startIndex + index}
                  aria-selected={active}
                  className={`${styles.node} ${styles.item} ${active ? styles.active : ''} ${styles[status.tone]}`}
                  onClick={() => {
                    onManualBrowse?.()
                    onSelect(item.id)
                  }}
                  onKeyDown={(event) => {
                    if (event.key !== 'Enter' && event.key !== ' ') return
                    event.preventDefault()
                    onSelect(item.id)
                  }}
                >
                  <input
                    type="checkbox"
                    className={styles.check}
                    checked={checkedIds.has(item.id)}
                    aria-label={item.name}
                    onClick={(e) => e.stopPropagation()}
                    onChange={(e) => onToggleChecked(item.id, e.target.checked)}
                  />
                  <span className={styles.caretSlot} aria-hidden="true" />
                  <span className={styles.name} title={(item.hostnames ?? []).join(', ')}>
                    {item.name}
                  </span>
                  <span
                    className={`${styles.infoArea} ${styles.infoStatus}`}
                    title={[t(status.key), hosts?.message].filter(Boolean).join(' · ')}
                    aria-label={t(status.key)}
                  >
                    <span className={`${styles.dot} ${DOT_CLASS[status.tone]}`} aria-hidden="true" />
                  </span>
                </div>
              </li>
            )
          })}
        </ul>
      )}
    </li>
  )
}
