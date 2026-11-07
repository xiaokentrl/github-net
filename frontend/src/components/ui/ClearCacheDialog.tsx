import { useEffect, useState } from 'react'
import { t } from '../../i18n'
import { getErrorMessage } from '../../api/errors'
import { Dialog } from './Dialog'
import uiStyles from './ui.module.css'
import styles from './ClearCacheDialog.module.css'
import { previewIPCache, previewRestoreHosts, previewSourcesCache, reprobeIP } from '../../api/client'
import type { HostsRestorePreview, IPCachePreview, PoolEntryView, SourceCachePreview } from '../../api/client'

export type ClearCacheChoice = { sources: boolean; allIP: boolean; hosts: boolean }

type PanelKey = 'cache' | 'ip' | 'hosts' | null

// 候选状态的文案键，和详情页同一套八档；不认识的状态落到「状态未知」，不自己编。
const STATUS_KEY: Record<string, string> = {
  queued: 'detect.candidate.queued',
  probing: 'detect.candidate.probing',
  available: 'detect.candidate.available',
  unavailable: 'detect.candidate.unavailable',
  timeout: 'detect.candidate.timeout',
  cancelled: 'detect.candidate.cancelled',
  notStarted: 'detect.candidate.notStarted',
  untested: 'detect.candidate.untested',
}

function statusLabel(status: string): string {
  return t(STATUS_KEY[status] ?? 'detect.candidate.unknown')
}

// 还原预览按「点下去会变成什么」分成三种具体结果，不让新用户自己对着两列猜。
// after 为空＝应用前本来没有这条，整条删掉；after 和 before 相同＝还被别的项目用着，不变；
// 其余＝现在指向的 IP 会被换回应用前那个。
type HostsRow = NonNullable<HostsRestorePreview['rows']>[number]
type HostsGroupKind = 'restore' | 'remove' | 'keep'
type HostsGroup = { kind: HostsGroupKind; rows: HostsRow[] }

const HOSTS_GROUP_KEY: Record<HostsGroupKind, { title: string; hint: string; sum: string }> = {
  restore: {
    title: 'toolbar.clearCache.hosts.groupRestore',
    hint: 'toolbar.clearCache.hosts.groupRestoreHint',
    sum: 'toolbar.clearCache.hosts.sumRestore',
  },
  remove: {
    title: 'toolbar.clearCache.hosts.groupRemove',
    hint: 'toolbar.clearCache.hosts.groupRemoveHint',
    sum: 'toolbar.clearCache.hosts.sumRemove',
  },
  keep: {
    title: 'toolbar.clearCache.hosts.groupKeep',
    hint: 'toolbar.clearCache.hosts.groupKeepHint',
    sum: 'toolbar.clearCache.hosts.sumKeep',
  },
}

function hostsGroups(rows: HostsRow[]): HostsGroup[] {
  const by: Record<HostsGroupKind, HostsRow[]> = { restore: [], remove: [], keep: [] }
  for (const r of rows) {
    if (!r.after) by.remove.push(r)
    else if (r.after === r.before) by.keep.push(r)
    else by.restore.push(r)
  }
  return (['restore', 'remove', 'keep'] as const)
    .map((kind) => ({ kind, rows: by[kind] }))
    .filter((g) => g.rows.length > 0)
}

// 三种结果各自的颜色类，写死映射而不是拼字符串，样式表改类名时类型检查能抓到。
const HOSTS_SUM_CLASS: Record<HostsGroupKind, string> = {
  restore: styles.restoreSumRestore,
  remove: styles.restoreSumRemove,
  keep: styles.restoreSumKeep,
}
const HOSTS_CARD_CLASS: Record<HostsGroupKind, string> = {
  restore: styles.restoreCardRestore,
  remove: styles.restoreCardRemove,
  keep: styles.restoreCardKeep,
}

// 上次检测：按距今多久说人话。时间没带或读到未来时间都按「刚刚」，读不懂给「—」。
function formatAgo(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso).getTime()
  if (Number.isNaN(d)) return '—'
  const minutes = Math.floor((Date.now() - d) / 60000)
  if (minutes < 1) return t('toolbar.clearCache.allIP.justNow')
  if (minutes < 60) return t('toolbar.clearCache.allIP.minutesAgo', { count: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return t('toolbar.clearCache.allIP.hoursAgo', { count: hours })
  return t('toolbar.clearCache.allIP.daysAgo', { count: Math.floor(hours / 24) })
}

// 二级菜单分组：一组就是一个二级菜单，组名直接用目录里的菜单名。
// 一个域名可能挂在多个菜单下（如 github.com 同属「主站与 API」和「china-hosts」），
// 这里按目录顺序取主归属 itemNames[0]，一条缓存只进一组，不把多个名字拼成伪分组。
// 分组顺序照目录里菜单的先后顺序，不按候选池延迟；目录里没有的「未归属」放最后。
type IPEntry = PoolEntryView
type IPGroup = { name: string; rows: IPEntry[] }

function groupByMenu(entries: IPEntry[] | null, menuOrder: string[]): IPGroup[] {
  const out: IPGroup[] = []
  const index = new Map<string, number>()
  for (const e of entries ?? []) {
    const name = e.itemNames?.[0] || t('toolbar.clearCache.allIP.noGroup')
    const at = index.get(name)
    if (at === undefined) {
      index.set(name, out.length)
      out.push({ name, rows: [e] })
    } else {
      out[at].rows.push(e)
    }
  }
  const rank = new Map<string, number>()
  menuOrder.forEach((name, i) => { if (!rank.has(name)) rank.set(name, i) })
  const tail = menuOrder.length
  return out.sort((a, b) => (rank.get(a.name) ?? tail) - (rank.get(b.name) ?? tail))
}

// 清理与还原弹框：左列三张可多选卡片（勾选决定执行什么），右侧「详情」展开对应项的预览。
// 三项预览进入即预拉；预览失败要明说，不能降级成「0 项」骗人。
// 勾选与「看详情」是两件事：勾选只决定底部「立即清理」是否可用，详情按钮才在右侧展示数据。
export function ClearCacheDialog({
  sourceIds,
  menuOrder,
  hostsDisabled,
  onCancel,
  onConfirm,
}: {
  sourceIds: string[]
  menuOrder: string[]
  hostsDisabled: boolean
  onCancel: () => void
  onConfirm: (choice: ClearCacheChoice) => void
}) {
  const [sources, setSources] = useState(false)
  const [allIP, setAllIP] = useState(false)
  const [hosts, setHosts] = useState(false)
  const [activePanel, setActivePanel] = useState<PanelKey>(null)
  const sourceCount = sourceIds.length
  const noCheckedItems = sourceCount === 0
  const hostsOn = hosts && !hostsDisabled
  const any = sources || allIP || hostsOn

  const [sourceRows, setSourceRows] = useState<SourceCachePreview[] | null>(null)
  const [ipPreview, setIpPreview] = useState<IPCachePreview | null>(null)
  const [hostsPreview, setHostsPreview] = useState<HostsRestorePreview | null>(null)
  // 每个面板各自的预览错误。读失败和「真的没有数据」是两件事，界面要说清是哪件。
  const [previewErrors, setPreviewErrors] = useState<Partial<Record<Exclude<PanelKey, null>, string>>>({})
  const [reprobingKey, setReprobingKey] = useState<string | null>(null)
  const [actionError, setActionError] = useState('')

  useEffect(() => {
    let alive = true
    previewSourcesCache(sourceIds)
      .then((rows) => { if (alive) { setSourceRows(rows); setPreviewErrors((p) => ({ ...p, cache: undefined })) } })
      .catch((err: unknown) => { if (alive) setPreviewErrors((p) => ({ ...p, cache: getErrorMessage(err) })) })
    return () => { alive = false }
  }, [sourceIds])

  useEffect(() => {
    let alive = true
    previewIPCache()
      .then((res) => { if (alive) { setIpPreview(res); setPreviewErrors((p) => ({ ...p, ip: undefined })) } })
      .catch((err: unknown) => { if (alive) setPreviewErrors((p) => ({ ...p, ip: getErrorMessage(err) })) })
    return () => { alive = false }
  }, [])

  useEffect(() => {
    if (hostsDisabled) {
      setHostsPreview(null)
      setPreviewErrors((p) => ({ ...p, hosts: undefined }))
      return
    }
    let alive = true
    previewRestoreHosts()
      .then((res) => { if (alive) { setHostsPreview(res); setPreviewErrors((p) => ({ ...p, hosts: undefined })) } })
      .catch((err: unknown) => { if (alive) setPreviewErrors((p) => ({ ...p, hosts: getErrorMessage(err) })) })
    return () => { alive = false }
  }, [hostsDisabled])

  const togglePanel = (key: Exclude<PanelKey, null>) => {
    setActionError('')
    setActivePanel((p) => (p === key ? null : key))
  }

  // 重新检测：真发后端命令，成功后只替换那一行，失败在详情面板里给出原因。
  const runReprobe = (hostname: string, ip: string) => {
    const key = `${hostname}|${ip}`
    setActionError('')
    setReprobingKey(key)
    reprobeIP(hostname, ip)
      .then((row) => {
        setIpPreview((prev) => prev ? {
          ...prev,
          entries: (prev.entries ?? []).map((e) => e.hostname === hostname && e.ip === ip ? row : e),
        } : prev)
      })
      .catch((err: unknown) => setActionError(t('toolbar.clearCache.allIP.reprobeFailed', { reason: getErrorMessage(err) })))
      .finally(() => setReprobingKey((k) => (k === key ? null : k)))
  }

  const pickedCount = [sources, allIP, hostsOn].filter(Boolean).length
  const activeError = activePanel ? previewErrors[activePanel] : undefined
  const hostsRows = hostsPreview?.rows ?? []
  const detailExpand = t('toolbar.clearCache.detailExpand')

  // 详情面板共用的两条提示：预览读失败、重新检测失败。都摆在面板里，不靠会消失的提示。
  const notices = (
    <>
      {activeError ? <p className={styles.alert}>{t('toolbar.clearCache.previewFailed', { reason: activeError })}</p> : null}
      {actionError ? <p className={styles.alert}>{actionError}</p> : null}
    </>
  )

  return (
    <Dialog
      title={t('toolbar.clearCache.ask.title')}
      onClose={onCancel}
      width={820}
      height={600}
      footer={
        <div className={styles.foot}>
          <span className={`${styles.hint} ${any ? styles.hintActive : ''}`}>
            {any
              ? t('toolbar.clearCache.footActive', { count: pickedCount })
              : t('toolbar.clearCache.footEmpty')}
          </span>
          <div className={styles.actions}>
            <button type="button" className={`${uiStyles.btn} ${uiStyles.ghost}`} onClick={onCancel}>
              {t('toolbar.clearCache.ask.cancel')}
            </button>
            <button
              type="button"
              className={`${uiStyles.btn} ${uiStyles.primary}`}
              disabled={!any}
              onClick={() => onConfirm({ sources, allIP, hosts: hostsOn })}
            >
              {t('toolbar.clearCache.ask.action')}
            </button>
          </div>
        </div>
      }
    >
      <div className={styles.wrap}>
        <p className={styles.subtitle}>{t('toolbar.clearCache.ask.hint')}</p>
        <div className={styles.layout}>
          {/* 左列：三张卡片（勾选决定执行，详情按钮决定右侧展示） */}
          <div className={styles.list}>
            <section className={`${styles.opt} ${activePanel === 'cache' ? styles.isActive : ''} ${sources ? styles.checked : ''}`}>
              <div className={styles.optHead}>
                <label className={styles.cb} htmlFor="opt-cache">
                  <input
                    id="opt-cache"
                    type="checkbox"
                    className={styles.cbInput}
                    checked={sources}
                    disabled={noCheckedItems}
                    onChange={(e) => setSources(e.target.checked)}
                  />
                  <svg className={styles.cbMark} viewBox="0 0 16 16" fill="none">
                    <path d="M3.4 8.4l3 3 6.2-7" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </label>
                <label className={styles.optInfo} htmlFor="opt-cache">
                  <span className={styles.optTitle}>
                    {t('toolbar.clearCache.source')}
                    <span className={styles.tag}>{t('toolbar.clearCache.unitSources', { count: sourceRows?.filter((r) => r.downloaded).length ?? 0 })}</span>
                  </span>
                  <span className={styles.optDesc}>{t('toolbar.clearCache.source.hint', { count: sourceCount })}</span>
                </label>
                <button type="button" className={styles.optExpand} onClick={() => togglePanel('cache')} aria-label={`${detailExpand}：${t('toolbar.clearCache.source')}`}>
                  {detailExpand}
                  <svg viewBox="0 0 16 16" fill="none"><path d="M6 4l4 4-4 4" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>
                </button>
              </div>
            </section>

            <section className={`${styles.opt} ${activePanel === 'ip' ? styles.isActive : ''} ${allIP ? styles.checked : ''}`}>
              <div className={styles.optHead}>
                <label className={styles.cb} htmlFor="opt-ip">
                  <input
                    id="opt-ip"
                    type="checkbox"
                    className={styles.cbInput}
                    checked={allIP}
                    onChange={(e) => setAllIP(e.target.checked)}
                  />
                  <svg className={styles.cbMark} viewBox="0 0 16 16" fill="none">
                    <path d="M3.4 8.4l3 3 6.2-7" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </label>
                <label className={styles.optInfo} htmlFor="opt-ip">
                  <span className={styles.optTitle}>
                    {t('toolbar.clearCache.allIP')}
                    <span className={styles.tag}>{t('toolbar.clearCache.unitDomains', { count: ipPreview?.domains ?? 0 })}</span>
                  </span>
                  <span className={styles.optDesc}>{t('toolbar.clearCache.allIP.hint')}</span>
                </label>
                <button type="button" className={styles.optExpand} onClick={() => togglePanel('ip')} aria-label={`${detailExpand}：${t('toolbar.clearCache.allIP')}`}>
                  {detailExpand}
                  <svg viewBox="0 0 16 16" fill="none"><path d="M6 4l4 4-4 4" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>
                </button>
              </div>
            </section>

            <section className={`${styles.opt} ${activePanel === 'hosts' ? styles.isActive : ''} ${hostsOn ? styles.checked : ''} ${hostsDisabled ? styles.disabled : ''}`}>
              <div className={styles.optHead}>
                <label className={styles.cb} htmlFor="opt-hosts">
                  <input
                    id="opt-hosts"
                    type="checkbox"
                    className={styles.cbInput}
                    checked={hostsOn}
                    disabled={hostsDisabled}
                    onChange={(e) => setHosts(e.target.checked)}
                  />
                  <svg className={styles.cbMark} viewBox="0 0 16 16" fill="none">
                    <path d="M3.4 8.4l3 3 6.2-7" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                </label>
                <label className={styles.optInfo} htmlFor="opt-hosts">
                  <span className={styles.optTitle}>
                    {t('toolbar.clearCache.hosts')}
                    <span className={styles.tag}>{t('toolbar.clearCache.unitMappings', { count: hostsPreview?.count ?? 0 })}</span>
                  </span>
                  <span className={styles.optDesc}>
                    {hostsDisabled ? t('toolbar.clearCache.hosts.busy') : t('toolbar.clearCache.hosts.hint')}
                  </span>
                </label>
                <button
                  type="button"
                  className={styles.optExpand}
                  disabled={hostsDisabled}
                  onClick={() => togglePanel('hosts')}
                  aria-label={`${detailExpand}：${t('toolbar.clearCache.hosts')}`}
                >
                  {detailExpand}
                  <svg viewBox="0 0 16 16" fill="none"><path d="M6 4l4 4-4 4" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>
                </button>
              </div>
            </section>
          </div>

          {/* 右详情面板：点左侧「详情」在右侧展示对应项 */}
          <div className={styles.detail}>
            {activePanel === null ? (
              <div className={styles.detailEmpty}>
                <div className={styles.emptyIcon}>
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none">
                    <rect x="3.5" y="4.5" width="17" height="15" rx="2" stroke="currentColor" strokeWidth="1.5" />
                    <path d="M14 4.5v15" stroke="currentColor" strokeWidth="1.5" />
                  </svg>
                </div>
                <p>{t('toolbar.clearCache.detailEmpty')}</p>
              </div>
            ) : activePanel === 'cache' ? (
              <div className={styles.detailBody}>
                {notices}
                <div className={styles.detailHead}>
                  <h3>{t('toolbar.clearCache.source')}</h3>
                  <span className={styles.detailSub}>{t('toolbar.clearCache.source.previewTitle', { count: sourceRows?.length ?? 0 })}</span>
                </div>
                {(sourceRows ?? []).length === 0 ? (
                  <p className={styles.emptyRow}>{t('toolbar.clearCache.source.previewEmpty')}</p>
                ) : (
                  <table className={styles.tbl}>
                    <thead>
                      <tr>
                        <th style={{ width: 40 }}>#</th>
                        <th>{t('toolbar.clearCache.source.colName')}</th>
                        <th className={styles.centered}>{t('toolbar.clearCache.source.colDownloaded')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {(sourceRows ?? []).map((r, i) => (
                        <tr key={r.name}>
                          <td className={styles.num}>{i + 1}</td>
                          <td>{r.name}</td>
                          <td className={styles.centered}>
                            <span className={`${styles.flag} ${r.downloaded ? styles.flagYes : styles.flagNo}`}>
                              {r.downloaded ? t('toolbar.clearCache.source.downloadedYes') : t('toolbar.clearCache.source.downloadedNo')}
                            </span>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                )}
                <p className={styles.detailNote}>{t('toolbar.clearCache.source.noteDownloaded')}</p>
                <p className={styles.detailNote}>{t('toolbar.clearCache.source.note', { count: sourceCount })}</p>
              </div>
            ) : activePanel === 'ip' ? (
              <div className={styles.detailBody}>
                {notices}
                <div className={styles.detailHead}>
                  <h3>{t('toolbar.clearCache.allIP')}</h3>
                  <span className={styles.detailSub}>{t('toolbar.clearCache.allIP.previewSummary', {
                    rounds: ipPreview?.rounds ?? 0,
                    addresses: ipPreview?.addresses ?? 0,
                    domains: ipPreview?.domains ?? 0,
                    candidateRows: ipPreview?.candidateRows ?? 0,
                    memoryResults: ipPreview?.memoryResults ?? 0,
                  })}</span>
                </div>
                {(ipPreview?.entries ?? []).length === 0 ? (
                  <p className={styles.emptyRow}>{t('toolbar.clearCache.allIP.previewEmpty')}</p>
                ) : (
                  <table className={`${styles.tbl} ${styles.ipTable}`}>
                    <colgroup>
                      <col className={styles.cGroup} />
                      <col className={styles.cHost} />
                      <col className={styles.cLast} />
                      <col className={styles.cAct} />
                    </colgroup>
                    <thead>
                      <tr>
                        <th>{t('toolbar.clearCache.allIP.colGroup')}</th>
                        <th>{t('toolbar.clearCache.allIP.colHosts')}</th>
                        <th className={styles.centered}>{t('toolbar.clearCache.allIP.colLast')}</th>
                        <th className={styles.centered}>{t('toolbar.clearCache.allIP.colAct')}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {groupByMenu(ipPreview?.entries ?? null, menuOrder).map((group) =>
                        group.rows.map((e, i) => {
                          const key = `${e.hostname}|${e.ip}`
                          const probing = reprobingKey === key
                          const usable = e.status === 'available'
                          return (
                            <tr key={key}>
                              {i === 0 && (
                                <td className={styles.groupCell} rowSpan={group.rows.length}>
                                  {group.name}
                                </td>
                              )}
                              <td className={styles.hostCell}>
                                <div className={styles.hostLine1}>
                                  <span className={`${styles.status} ${usable ? styles.statusOk : styles.statusBad}`}>
                                    {statusLabel(e.status)}
                                  </span>
                                  <span className={styles.hostDomain}>{e.hostname}</span>
                                </div>
                                <div className={`${styles.hostLine2} ${e.ip ? '' : styles.hostLineEmpty}`}>
                                  → {e.ip || '—'}{e.latencyMs > 0 ? ` · ${e.latencyMs}ms` : ''}
                                </div>
                              </td>
                              <td className={styles.lastCol}>{formatAgo(e.updatedAt)}</td>
                              <td className={styles.actCol}>
                                <button
                                  type="button"
                                  className={`${styles.btnDetect} ${usable ? styles.btnDetectOk : styles.btnDetectBad}`}
                                  disabled={probing}
                                  title={t('toolbar.clearCache.allIP.detectTitle')}
                                  onClick={() => runReprobe(e.hostname, e.ip)}
                                >
                                  {probing ? t('toolbar.clearCache.allIP.detecting') : t('toolbar.clearCache.allIP.detect')}
                                </button>
                              </td>
                            </tr>
                          )
                        }),
                      )}
                    </tbody>
                  </table>
                )}
                <p className={styles.detailNote}>{t('toolbar.clearCache.allIP.note')}</p>
              </div>
            ) : (
              <div className={styles.detailBody}>
                {notices}
                <div className={styles.detailHead}>
                  <h3>{t('toolbar.clearCache.hosts')}</h3>
                  <span className={styles.detailSub}>{t('toolbar.clearCache.hosts.previewTitle', {
                    count: hostsPreview?.count ?? 0,
                    items: hostsPreview?.items ?? 0,
                  })}</span>
                </div>
                {hostsRows.length === 0 ? (
                  <p className={styles.emptyRow}>{t('toolbar.clearCache.hosts.previewEmpty')}</p>
                ) : (
                  <>
                    <p className={styles.restoreIntro}>{t('toolbar.clearCache.hosts.intro', { count: hostsRows.length })}</p>
                    <div className={styles.restoreSummary}>
                      {hostsGroups(hostsRows).map((g) => (
                        <span key={g.kind} className={`${styles.restoreSum} ${HOSTS_SUM_CLASS[g.kind]}`}>
                          {t(HOSTS_GROUP_KEY[g.kind].sum, { count: g.rows.length })}
                        </span>
                      ))}
                    </div>
                    {hostsGroups(hostsRows).map((g) => (
                      <section key={g.kind} className={styles.restoreGroup}>
                        <div className={styles.restoreGroupHead}>
                          <h4>{t(HOSTS_GROUP_KEY[g.kind].title)}</h4>
                          <span className={styles.restoreGroupCount}>{t('toolbar.clearCache.unitMappings', { count: g.rows.length })}</span>
                        </div>
                        <p className={styles.restoreGroupHint}>{t(HOSTS_GROUP_KEY[g.kind].hint)}</p>
                        <div className={styles.restoreCards}>
                          {g.rows.map((r) => (
                            <div key={r.hostname} className={`${styles.restoreCard} ${HOSTS_CARD_CLASS[g.kind]}`}>
                              <div className={styles.restoreDomain}>{r.hostname}</div>
                              <div className={styles.restoreFlow}>
                                <div className={styles.restoreSide}>
                                  <span className={styles.restoreSideLabel}>{t('toolbar.clearCache.hosts.now')}</span>
                                  <span className={styles.restoreLine}>
                                    <span className={styles.mono}>{r.before}</span> <span className={styles.restoreLineHost}>{r.hostname}</span>
                                  </span>
                                </div>
                                <svg className={styles.restoreArrow} viewBox="0 0 20 12" fill="none" aria-hidden="true">
                                  <path d="M1 6h16m-5-4 5 4-5 4" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
                                </svg>
                                <div className={styles.restoreSide}>
                                  <span className={styles.restoreSideLabel}>{t('toolbar.clearCache.hosts.then')}</span>
                                  {r.after ? (
                                    <span className={styles.restoreLine}>
                                      <span className={`${styles.mono} ${styles.restoreAfterIp}`}>{r.after}</span> <span className={styles.restoreLineHost}>{r.hostname}</span>
                                    </span>
                                  ) : (
                                    <span className={styles.restoreAfterNone}>{t('toolbar.clearCache.hosts.thenRemove')}</span>
                                  )}
                                </div>
                              </div>
                            </div>
                          ))}
                        </div>
                      </section>
                    ))}
                  </>
                )}
                <p className={styles.detailNote}>{t('toolbar.clearCache.hosts.note')}</p>
              </div>
            )}
          </div>
        </div>
      </div>
    </Dialog>
  )
}
