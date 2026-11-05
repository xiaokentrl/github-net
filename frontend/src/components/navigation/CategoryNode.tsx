import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
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

// 能还原 = 这个项目的映射确实在本工具的管理区段里（已替换或部分生效），和详情页那枚「还原此项」同一个条件。
const RESTORABLE = new Set<HostsStatus>([HostsStatus.HostsApplied, HostsStatus.HostsPartial])

// 项目 ID → 后端这次读出来的 Hosts 状态。整条状态都留着：
// 树上那一行只用行底色和色点，状态词与冲突／读不到的原因收进悬停提示里。
export type HostsLookup = Map<string, HostsState>

// 项目 ID → 队列里正在对它做的事；没有这一项就不在 Map 里。
export type LiveLookup = Map<string, LiveKind>

// 分类行和项目行都是整行可点的容器，行尾那个 ↺ 是容器里的真按钮——
// 所以行本身用 div + role 而不是 button：button 套 button 是非法结构，键盘焦点也会分裂。
// 原型同一条规则：整行一个点击区，↺ 自己 stopPropagation（原型第 3527–3539 行）。
export function CategoryNode({
  category,
  items,
  hostsOf,
  liveOf,
  results,
  expanded,
  selectedId,
  checkedIds,
  onToggleChecked,
  onManualBrowse,
  onToggle,
  onSelect,
  onRestoreItems,
}: {
  category: CatalogCategory
  items: CatalogItem[]
  hostsOf: HostsLookup
  liveOf: LiveLookup
  results: Record<string, DetectResultView>
  expanded: boolean
  selectedId: string
  onToggle: (categoryID: string) => void
  onSelect: (itemID: string) => void
  checkedIds: ReadonlySet<string>
  onToggleChecked: (itemID: string, checked: boolean) => void
  onManualBrowse?: () => void
  onRestoreItems: (itemIDs: string[]) => void
}) {
  // 全量项：搜索时 items 只剩命中行，但分类总数、整行高亮和 ↺ 的范围都按全量算（原型第 2334-2339、2370 行用的都是 cat.items）。
  const all = category.items ?? []
  // 分类行只看底下这些项有没有「处理中」或「失败」，颜色跟着统一四档走。
  const states = all.map((item) => itemStatus(hostsOf.get(item.id), liveOf.get(item.id), results[item.id]))
  const hasWorking = states.some((s) => s.tone === 'working')
  const hasFailed = states.some((s) => s.tone === 'failed')
  const restorable = all.filter((item) => RESTORABLE.has(hostsOf.get(item.id)?.status ?? HostsStatus.$zero))

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
        <span className={styles.restoreSlot}>
          {restorable.length > 0 ? (
            <button
              type="button"
              className={styles.restoreBtn}
              title={t('tree.restore.category', { name: category.name, count: restorable.length })}
              aria-label={t('tree.restore.categoryAria', { name: category.name })}
              onClick={(event) => {
                event.stopPropagation()
                onRestoreItems(restorable.map((item) => item.id))
              }}
            >
              ↺
            </button>
          ) : null}
        </span>
      </div>

      {expanded && (
        <ul role="group" className={styles.children}>
          {items.map((item) => {
            const hosts = hostsOf.get(item.id)
            const status = itemStatus(hosts, liveOf.get(item.id), results[item.id])
            const active = item.id === selectedId
            return (
              <li key={item.id} role="none">
                <div
                  role="treeitem"
                  tabIndex={0}
                  data-item-id={item.id}
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
                  <span className={styles.restoreSlot}>
                    {RESTORABLE.has(hosts?.status ?? HostsStatus.$zero) ? (
                      <button
                        type="button"
                        className={styles.restoreBtn}
                        title={t('tree.restore.item')}
                        aria-label={t('tree.restore.itemAria', { name: item.name })}
                        onClick={(event) => {
                          event.stopPropagation()
                          onRestoreItems([item.id])
                        }}
                      >
                        ↺
                      </button>
                    ) : null}
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
