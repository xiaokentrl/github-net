import { useEffect, useState } from 'react'
import { t } from '../../i18n'
import { Dialog } from './Dialog'
import uiStyles from './ui.module.css'
import styles from './ClearCacheDialog.module.css'
import { previewIPCache, previewRestoreHosts, previewSourcesCache } from '../../api/client'
import type { HostsRestorePreview, IPCachePreview } from '../../api/client'

export type ClearCacheChoice = { sources: boolean; allIP: boolean; hosts: boolean }

type PanelKey = 'cache' | 'ip' | 'hosts' | null

// 清理与还原弹框：左列三张可多选卡片（勾选决定执行什么），右侧「详情」展开对应项的预览。
// 三项预览进入即预拉（hosts 仅在可还原时），左侧卡片的数量徽标和右侧详情都吃这份数据；
// 勾选与「看详情」是两件事：勾选只决定底部「立即清理」是否可用，详情按钮才在右侧展示数据。
export function ClearCacheDialog({
  sourceIds,
  hostsDisabled,
  onCancel,
  onConfirm,
}: {
  sourceIds: string[]
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

  const [sourceNames, setSourceNames] = useState<string[] | null>(null)
  const [ipPreview, setIpPreview] = useState<IPCachePreview | null>(null)
  const [hostsPreview, setHostsPreview] = useState<HostsRestorePreview | null>(null)

  // 进入即预拉三项，徽标和详情都直接读这份；hosts 预览在队列占用时跳过（还原被禁用，看了也没用）。
  useEffect(() => {
    let alive = true
    previewSourcesCache(sourceIds)
      .then((names) => { if (alive) setSourceNames(names) })
      .catch(() => { if (alive) setSourceNames([]) })
    return () => { alive = false }
  }, [sourceIds])

  useEffect(() => {
    let alive = true
    previewIPCache()
      .then((res) => { if (alive) setIpPreview(res) })
      .catch(() => { if (alive) setIpPreview(null) })
    return () => { alive = false }
  }, [])

  useEffect(() => {
    if (hostsDisabled) {
      setHostsPreview(null)
      return
    }
    let alive = true
    previewRestoreHosts()
      .then((res) => { if (alive) setHostsPreview(res) })
      .catch(() => { if (alive) setHostsPreview(null) })
    return () => { alive = false }
  }, [hostsDisabled])

  const togglePanel = (key: Exclude<PanelKey, null>) =>
    setActivePanel((p) => (p === key ? null : key))

  const pickedCount = [sources, allIP, hostsOn].filter(Boolean).length

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
              onClick={() => onConfirm({ sources, allIP, hosts })}
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
                    <span className={styles.tag}>{sourceNames ? sourceNames.length : 0} 份</span>
                  </span>
                  <span className={styles.optDesc}>{t('toolbar.clearCache.source.hint', { count: sourceCount })}</span>
                </label>
                <button type="button" className={styles.optExpand} onClick={() => togglePanel('cache')} aria-label="查看缓存源详情">
                  详情
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
                    <span className={styles.tag}>{ipPreview ? ipPreview.domains : 0} 个域名</span>
                  </span>
                  <span className={styles.optDesc}>{t('toolbar.clearCache.allIP.hint')}</span>
                </label>
                <button type="button" className={styles.optExpand} onClick={() => togglePanel('ip')} aria-label="查看全部 IP 缓存详情">
                  详情
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
                    <span className={styles.tag}>{hostsPreview ? hostsPreview.count : 0} 个域名</span>
                  </span>
                  <span className={styles.optDesc}>{t('toolbar.clearCache.hosts.hint')}</span>
                </label>
                <button
                  type="button"
                  className={styles.optExpand}
                  disabled={hostsDisabled}
                  onClick={() => togglePanel('hosts')}
                  aria-label="查看 Hosts 对比详情"
                >
                  详情
                  <svg viewBox="0 0 16 16" fill="none"><path d="M6 4l4 4-4 4" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" /></svg>
                </button>
              </div>
            </section>
          </div>

          {/* 右详情面板：点左侧「详情」在右侧展示对应项，空状态提示去点。 */}
          <div className={styles.detail}>
            {activePanel === null ? (
              <div className={styles.detailEmpty}>
                <div className={styles.emptyIcon}>
                  <svg width="20" height="20" viewBox="0 0 24 24" fill="none">
                    <rect x="3.5" y="4.5" width="17" height="15" rx="2" stroke="currentColor" strokeWidth="1.5" />
                    <path d="M14 4.5v15" stroke="currentColor" strokeWidth="1.5" />
                  </svg>
                </div>
                <p>点击左侧「详情」在右侧查看</p>
              </div>
            ) : activePanel === 'cache' ? (
              <div className={styles.detailBody}>
                <div className={styles.detailHead}>
                  <h3>{t('toolbar.clearCache.source')}</h3>
                  <span className={styles.detailSub}>{t('toolbar.clearCache.source.previewTitle', { count: sourceNames?.length ?? 0 })}</span>
                </div>
                <table className={styles.tbl}>
                  <thead>
                    <tr>
                      <th style={{ width: 40 }}>#</th>
                      <th>缓存源</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(sourceNames ?? []).map((n, i) => (
                      <tr key={n}>
                        <td className={styles.num}>{i + 1}</td>
                        <td>{n}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <p className={styles.detailNote}>{t('toolbar.clearCache.source.note', { count: sourceCount })}</p>
              </div>
            ) : activePanel === 'ip' ? (
              <div className={styles.detailBody}>
                <div className={styles.detailHead}>
                  <h3>{t('toolbar.clearCache.allIP')}</h3>
                  <span className={styles.detailSub}>{t('toolbar.clearCache.allIP.previewSummary', {
                    rounds: ipPreview?.rounds ?? 0,
                    addresses: ipPreview?.addresses ?? 0,
                    domains: ipPreview?.domains ?? 0,
                  })}</span>
                </div>
                <table className={styles.tbl}>
                  <thead>
                    <tr>
                      <th>域名</th>
                      <th>当前 IP</th>
                      <th>状态</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(ipPreview?.entries ?? []).map((e) => (
                      <tr key={`${e.hostname}|${e.ip}`}>
                        <td className={styles.mono}>{e.hostname}</td>
                        <td className={styles.mono}>{e.ip}{e.ipv6 ? ' (v6)' : ''}{e.latencyMs ? ` · ${e.latencyMs}ms` : ''}</td>
                        <td>
                          <span className={`${styles.status} ${e.status === 'written' ? styles.written : styles.unwritten}`}>
                            {e.status === 'written' ? '已写' : '未写'}
                          </span>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <p className={styles.detailNote}>{t('toolbar.clearCache.allIP.note')}</p>
              </div>
            ) : (
              <div className={styles.detailBody}>
                <div className={styles.detailHead}>
                  <h3>{t('toolbar.clearCache.hosts')}</h3>
                  <span className={styles.detailSub}>{t('toolbar.clearCache.hosts.previewTitle', { count: hostsPreview?.count ?? 0 })}</span>
                </div>
                <table className={styles.tbl}>
                  <thead>
                    <tr>
                      <th>域名</th>
                      <th>恢复前</th>
                      <th>恢复后</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(hostsPreview?.rows ?? []).map((r) => (
                      <tr key={r.hostname}>
                        <td className={styles.mono}>{r.hostname}</td>
                        <td className={`${styles.mono} ${styles.hostIp}`}>{r.before}</td>
                        <td>
                          {r.after ? (
                            <span className={styles.mono}>{r.after}</span>
                          ) : (
                            <span className={styles.hostTag}>{t('toolbar.clearCache.hosts.removed')}</span>
                          )}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
                <p className={styles.detailNote}>{t('toolbar.clearCache.hosts.note')}</p>
              </div>
            )}
          </div>
        </div>
      </div>
    </Dialog>
  )
}
