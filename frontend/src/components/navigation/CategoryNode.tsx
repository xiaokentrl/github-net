import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { CatalogCategory, CatalogItem, DetectResultView, HostsState } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import { Highlight } from './Highlight'
import { itemStatus, type LiveKind, type Tone } from './treeStatus'
import styles from './CategoryTree.module.css'

// 六档色点、五档状态字颜色：tone 由 treeStatus 算出来，这里只做「哪一档挂哪个类」。
const DOT_CLASS: Record<Tone, string> = {
  unset: styles.dotUnset,
  applied: styles.dotApplied,
  stale: styles.dotStale,
  error: styles.dotError,
  pending: styles.dotPending,
  detected: styles.dotDetected,
}

const TEXT_CLASS: Partial<Record<Tone, string>> = {
  error: styles.stextError,
  pending: styles.stextPending,
  stale: styles.stextStale,
  detected: styles.stextDetected,
}

// 能还原 = 这个项目的映射确实在本工具的管理区段里（已替换或部分生效），和详情页那枚「还原此项」同一个条件。
const RESTORABLE = new Set<HostsStatus>([HostsStatus.HostsApplied, HostsStatus.HostsPartial])

// 项目 ID → 后端这次读出来的 Hosts 状态。整条状态都留着：
// 树上那一行只用状态字和色点，冲突和读不到的原因文本要能就地看到（挂在状态那一段的 title 上）。
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
  needle,
  expanded,
  selectedId,
  onToggle,
  onSelect,
  onRestoreItems,
}: {
  category: CatalogCategory
  items: CatalogItem[]
  hostsOf: HostsLookup
  liveOf: LiveLookup
  results: Record<string, DetectResultView>
  needle: string
  expanded: boolean
  selectedId: string
  onToggle: (categoryID: string) => void
  onSelect: (itemID: string) => void
  onRestoreItems: (itemIDs: string[]) => void
}) {
  // 全量项：搜索时 items 只剩命中行，但分类总数、整行高亮和 ↺ 的范围都按全量算（原型第 2334-2339、2370 行用的都是 cat.items）。
  const all = category.items ?? []
  // 分类行的高亮只看它底下这些项：有一项在检测就整行泛金，在写 Hosts 或还原就整行泛主色。
  const live = all.map((item) => liveOf.get(item.id)).filter((v): v is LiveKind => v !== undefined)
  const hasDetecting = live.includes('detecting')
  const hasApplying = live.some((v) => v === 'applying' || v === 'restoring')
  const restorable = all.filter((item) => RESTORABLE.has(hostsOf.get(item.id)?.status ?? HostsStatus.$zero))

  return (
    <li role="none" className={styles.group}>
      <div
        role="treeitem"
        tabIndex={0}
        aria-expanded={expanded}
        className={`${styles.node} ${styles.cat} ${hasDetecting ? styles.hasDetecting : hasApplying ? styles.hasApplying : ''}`}
        onClick={() => onToggle(category.id)}
        onKeyDown={(event) => {
          if (event.key !== 'Enter' && event.key !== ' ') return
          event.preventDefault()
          onToggle(category.id)
        }}
      >
        <span className={`${styles.caret} ${expanded ? '' : styles.collapsed}`} aria-hidden="true">
          ▼
        </span>
        <span className={styles.name}>
          <Highlight text={category.name} needle={needle} />
        </span>
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
            const liveKind = liveOf.get(item.id)
            const status = itemStatus(hosts?.status ?? HostsStatus.$zero, liveKind, results[item.id])
            const active = item.id === selectedId
            return (
              <li key={item.id} role="none">
                <div
                  role="treeitem"
                  tabIndex={0}
                  aria-selected={active}
                  className={`${styles.node} ${styles.item} ${active ? styles.active : ''} ${
                    liveKind === 'detecting' ? styles.detecting : liveKind === 'applying' || liveKind === 'restoring' ? styles.applying : ''
                  }`}
                  onClick={() => onSelect(item.id)}
                  onKeyDown={(event) => {
                    if (event.key !== 'Enter' && event.key !== ' ') return
                    event.preventDefault()
                    onSelect(item.id)
                  }}
                >
                  <span className={styles.caretSlot} aria-hidden="true" />
                  <span className={styles.name} title={(item.hostnames ?? []).join(', ')}>
                    <Highlight text={item.name} needle={needle} />
                  </span>
                  <span className={`${styles.infoArea} ${styles.infoStatus}`} title={hosts?.message}>
                    <span className={`${styles.dot} ${DOT_CLASS[status.tone]}`} aria-hidden="true" />
                    <span className={`${styles.stext} ${TEXT_CLASS[status.tone]}`}>{t(status.key)}</span>
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
