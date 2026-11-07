import { useEffect, useRef, useState, type ReactNode } from 'react'
import { t } from '../../i18n'
import { getErrorMessage } from '../../api/errors'
import { getCandidatePool, reprobeIP } from '../../api/client'
import type { HostEntryState, PoolEntryView } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import styles from './DetectTables.module.css'

// 上下两张表各管一件事，形态刻意不同，别让人看成同一张表复制了两遍：
//   ① 还原：只放支持还原的 IP，「重新检测」逐条重探这批 IP 通不通，主按钮是「还原可用IP」。
//           列是「当前写入 IP / 这条 IP 通不通」，不谈检测轮次。
//   ② 检测与写入：放这个二级菜单下的全部域名，「检测全部」跑一轮全量检测刷新全部网络状态，
//           主按钮是「检测全部」，多一列「上次检测」说明结论新旧，再把测通的写进 Hosts。
// 网络状态一律来自后端定格（Hosts 逐条贴回的 probeStatus / 候选池的 status），界面不自己推断通不通。

type NetState = { status?: string; latencyMs?: number }
type Row = { key: string; hostname: string; ip: string; net: NetState; measuredAt?: string }

type Actions = {
  restoreIp: () => void
  detectAll: () => void
  writeIp: () => void
}

type Busy = { detect: boolean; restore: boolean; apply: boolean; restoreSettled: boolean }

// 「检测全部」要按目录里这一项去找域名；「写入可用IP」要有最近一轮的检测结论才能写，
// 否则后端会在入口拒掉，按钮可点却失败就是骗人。
type Ready = { canDetectAll: boolean; hasDetectResult: boolean }

type Variant = 'ghost' | 'restore' | 'detect' | 'write'

// 后端 CandidateStatus 八档照抄成文案键，不认识的状态退回「状态未知」。
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

// 「上次检测」说距今多久，复用清理弹框那套相对时间文案，不另造一份说法。
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

// 一个域名的全部候选，一条都不截：可用优先，档内按延迟升序，同档同延迟保持池里原顺序。
// 排序口径与详情页候选表一致，只是这里「全列」而不是只挑最优那条。
function candidatesFor(pool: PoolEntryView[], hostname: string): PoolEntryView[] {
  return pool
    .filter((p) => p.hostname === hostname)
    .map((p, order) => ({ p, order }))
    .sort((a, b) => {
      const rank = (x: PoolEntryView) => x.status === 'available' ? 0 : 1
      const latency = (x: PoolEntryView) => x.latencyMs > 0 ? x.latencyMs : Number.MAX_SAFE_INTEGER
      return rank(a.p) - rank(b.p) || latency(a.p) - latency(b.p) || a.order - b.order
    })
    .map(({ p }) => p)
}

function NetTag({ net }: { net: NetState }) {
  const status = net.status
  const tone = status === 'available' ? styles.netOk
    : status === 'unavailable' || status === 'timeout' ? styles.netBad
      : styles.netIdle
  const label = status
    ? t(STATUS_KEY[status] ?? 'detect.candidate.unknown')
    : t('detail.detectBox.notProbed')
  return (
    <span className={`${styles.netTag} ${tone}`}>
      {label}
      {net.latencyMs && net.latencyMs > 0
        ? <span className={styles.netLatency}>{t('detect.latency', { ms: net.latencyMs })}</span>
        : null}
    </span>
  )
}

function CardHead({ step, title, desc, countLabel, tone }: {
  step: string
  title: string
  desc: string
  countLabel: string
  tone: 'restore' | 'detect'
}) {
  return (
    <div className={`${styles.cardHead} ${tone === 'restore' ? styles.headRestore : styles.headDetect}`}>
      <div className={styles.headLine}>
        <span className={styles.headStep}>{step}</span>
        <h4 className={styles.headTitle}>{title}</h4>
        <span className={styles.headCount}>{countLabel}</span>
      </div>
      <p className={styles.headDesc}>{desc}</p>
    </div>
  )
}

export function DetectTables({ name, hostnames, entries, actions, busy, ready, refreshKey }: {
  name: string
  hostnames: string[]
  entries: HostEntryState[]
  actions: Actions
  busy: Busy
  ready: Ready
  // 检测/还原/写入任务状态一变就重拉候选池，表里始终是刚落定的真实数据。
  refreshKey: string
}) {
  const [pool, setPool] = useState<PoolEntryView[] | null>(null)
  const [poolError, setPoolError] = useState('')
  // 第一张表现场复测的结论按「域名|IP」存，覆盖后端读回来的旧定格。
  const [probeOverrides, setProbeOverrides] = useState<Record<string, NetState>>({})
  const [reprobe, setReprobe] = useState<{ done: number; total: number } | null>(null)
  const [reprobeError, setReprobeError] = useState('')
  const alive = useRef(true)
  useEffect(() => () => { alive.current = false }, [])

  useEffect(() => {
    let live = true
    getCandidatePool()
      .then((rows) => { if (live) { setPool(rows); setPoolError(''); setProbeOverrides({}) } })
      .catch((err: unknown) => { if (live) setPoolError(getErrorMessage(err)) })
    return () => { live = false }
  }, [refreshKey])

  // ① 还原：只列支持还原的 IP。written 是后端按认领关系判出来的「本项目写进 Hosts 的映射」，
  // 网络不通不排除它——那正是旁边「这条 IP 通不通」这一列要说的事。
  const restorableRows: Row[] = entries.flatMap((e) => {
    const ip = e.ip
    if (!e.written || !ip) return []
    const key = `${e.hostname}|${ip}`
    return [{
      key,
      hostname: e.hostname,
      ip,
      net: probeOverrides[key] ?? { status: e.probeStatus, latencyMs: e.latencyMs },
      measuredAt: e.measuredAt,
    }]
  })

  // ② 检测与写入：这个二级菜单下每个候选 IP 逐条列成一行，各带自己的网络状态和上次检测。
  // 目录里的域名一条候选都没有时也留一行占位（IP 显示「—」），不然「全部域名」会漏掉没测过的那几个。
  const allRows: Row[] = hostnames.flatMap((hostname) => {
    const hits = pool ? candidatesFor(pool, hostname) : []
    if (hits.length === 0) {
      return [{ key: `${hostname}|—`, hostname, ip: '—', net: {} }]
    }
    return hits.map((hit) => ({
      key: `${hostname}|${hit.ip}`,
      hostname,
      ip: hit.ip || '—',
      net: { status: hit.status, latencyMs: hit.latencyMs },
      measuredAt: hit.updatedAt,
    }))
  })

  const restorableCount = restorableRows.length
  const cachedCount = allRows.filter((r) => r.ip !== '—').length
  // 缓存里真有验通的地址，才谈得上「写入可用IP」；没测出来就别让按钮可点。
  const hostSet = new Set(hostnames)
  const hasUsableIp = (pool ?? []).some((p) => hostSet.has(p.hostname) && p.status === 'available')

  // 逐条现场复测：同步等每条后端探测结论，测完一条就把那行的网络状态刷掉，
  // 同时把候选池里同一行也刷新，第二张表跟着一致。失败不吞，汇总在表下给原因。
  const runReprobeAll = async () => {
    if (reprobe || restorableRows.length === 0) return
    const targets = [...restorableRows]
    setReprobeError('')
    setReprobe({ done: 0, total: targets.length })
    let failed = 0
    let firstReason = ''
    for (let i = 0; i < targets.length; i++) {
      if (!alive.current) return
      const row = targets[i]
      setReprobe({ done: i, total: targets.length })
      try {
        const updated = await reprobeIP(row.hostname, row.ip)
        if (!alive.current) return
        setProbeOverrides((prev) => ({
          ...prev,
          [row.key]: { status: updated.status, latencyMs: updated.latencyMs },
        }))
        setPool((prev) => prev
          ? prev.map((p) => p.hostname === row.hostname && p.ip === row.ip ? updated : p)
          : prev)
      } catch (err: unknown) {
        failed++
        if (!firstReason) firstReason = getErrorMessage(err)
      }
    }
    if (!alive.current) return
    setReprobe(null)
    if (failed > 0) {
      setReprobeError(t('detail.detectBox.reprobeFailed', { count: failed, reason: firstReason }))
    }
  }

  const button = (
    labelKey: string, hintKey: string, onClick: () => void, disabled: boolean,
    variant: Variant, reasonKey?: string, labelOverride?: string,
  ) => (
    <button
      type="button"
      className={`${styles.actionBtn} ${variant !== 'ghost' ? styles.btnHero : ''} ${
        variant === 'restore' ? styles.btnRestore
          : variant === 'detect' ? styles.btnDetect
            : variant === 'write' ? styles.btnWrite : ''
      }`}
      onClick={onClick}
      disabled={disabled}
      title={disabled && reasonKey ? t(reasonKey) : t(hintKey)}
    >
      {labelOverride ?? t(labelKey)}
    </button>
  )

  const redetectDisabled = busy.detect || reprobe !== null || restorableCount === 0
  const redetectLabel = reprobe
    ? t('detail.detectBox.redetecting', { done: reprobe.done, total: reprobe.total })
    : t('detail.detectBox.redetect')

  // ① 的主按钮是「还原」，重测只是还原前的一步确认，所以放上面、弱样式。
  const restoreActions = (
    <div className={styles.actionStack}>
      {button('detail.detectBox.redetect', 'detail.detectBox.redetectHint', () => void runReprobeAll(),
        redetectDisabled, 'ghost',
        restorableCount === 0 ? 'detail.detectBox.noRestorable' : 'detail.box.detecting',
        redetectLabel)}
      {button('detail.detectBox.restoreIp', 'detail.detectBox.restoreHint', actions.restoreIp,
        busy.restore || busy.restoreSettled || restorableCount === 0, 'restore',
        restorableCount === 0 ? 'detail.detectBox.noRestorable' : 'detail.box.restoring')}
    </div>
  )

  // ② 的主按钮是「检测全部」，写入是测完之后的动作，用另一种样式但同样可点。
  const detectActions = (
    <div className={styles.actionStack}>
      {button('detail.detectBox.detectAll', 'detail.detectBox.detectAllHint', actions.detectAll,
        busy.detect || reprobe !== null || !ready.canDetectAll, 'detect', 'detail.box.detecting')}
      {button('detail.detectBox.writeIp', 'detail.detectBox.writeHint', actions.writeIp,
        busy.apply || !hasUsableIp || !ready.hasDetectResult, 'write',
        !hasUsableIp || !ready.hasDetectResult ? 'detail.detectBox.noUsableIp' : 'detail.box.restoring')}
    </div>
  )

  // 两张表共用的几格：二级菜单合并、域名→IP、网络状态。列数与列头由各自的表决定。
  // 第二张表候选行多，合并格贴顶，别让菜单名和按钮垂直居中在长表中间看不见。
  const groupCell = (rows: Row[], i: number, top = false) => i === 0 ? (
    <td className={`${styles.groupCell} ${top ? styles.groupCellTop : ''}`} rowSpan={rows.length}>
      <span className={styles.groupArrow}>▸</span>{name}
    </td>
  ) : null
  const dataCell = (r: Row) => (
    <td className={styles.dataCell}>
      <span className={styles.domain}>{r.hostname}</span>
      <span className={styles.arrow}>→</span>
      <span className={`${styles.ip} ${r.ip === '—' ? styles.dim : ''}`}>{r.ip}</span>
    </td>
  )
  const netCell = (r: Row) => <td className={styles.netCell}><NetTag net={r.net} /></td>
  const actionCell = (rows: Row[], i: number, node: ReactNode, top = false) => i === 0 ? (
    <td className={`${styles.actionCell} ${top ? styles.actionCellTop : ''}`} rowSpan={rows.length}>{node}</td>
  ) : null
  const emptyRow = (colSpan: number, key: string) => (
    <tr><td className={styles.empty} colSpan={colSpan}>{t(key)}</td></tr>
  )

  const firstTable = (
    <section className={`${styles.block} ${styles.blockRestore}`}>
      <CardHead
        step="①"
        title={t('detail.detectBox.firstTitle')}
        desc={t('detail.detectBox.firstDesc')}
        countLabel={t('detail.detectBox.restorable', { count: restorableCount })}
        tone="restore"
      />
      <table className={styles.tbl}>
        <colgroup>
          <col className={styles.cMenu} />
          <col className={styles.cData} />
          <col className={styles.cNet} />
          <col className={styles.cAction} />
        </colgroup>
        <thead>
          <tr>
            <th>{t('detail.detectBox.colMenu')}</th>
            <th>{t('detail.detectBox.colWrittenIp')}</th>
            <th className={styles.centered}>{t('detail.detectBox.colNetFirst')}</th>
            <th className={styles.centered}>{t('detail.detectBox.colAction')}</th>
          </tr>
        </thead>
        <tbody>
          {restorableRows.length === 0
            ? emptyRow(4, 'detail.detectBox.restorableEmpty')
            : restorableRows.map((r, i) => (
              <tr key={r.key}>
                {groupCell(restorableRows, i)}
                {dataCell(r)}
                {netCell(r)}
                {actionCell(restorableRows, i, restoreActions)}
              </tr>
            ))}
        </tbody>
      </table>
    </section>
  )

  const secondTable = (
    <section className={`${styles.block} ${styles.blockDetect}`}>
      <CardHead
        step="②"
        title={t('detail.detectBox.secondTitle')}
        desc={t('detail.detectBox.secondDesc')}
        countLabel={t('detail.detectBox.cached', { count: cachedCount })}
        tone="detect"
      />
      <table className={styles.tbl}>
        <colgroup>
          <col className={styles.cMenu2} />
          <col className={styles.cData2} />
          <col className={styles.cNet2} />
          <col className={styles.cLast} />
          <col className={styles.cAction2} />
        </colgroup>
        <thead>
          <tr>
            <th>{t('detail.detectBox.colMenu')}</th>
            <th>{t('detail.detectBox.colDetectedIp')}</th>
            <th className={styles.centered}>{t('detail.detectBox.colNet')}</th>
            <th className={styles.centered}>{t('detail.detectBox.colLast')}</th>
            <th className={styles.centered}>{t('detail.detectBox.colAction')}</th>
          </tr>
        </thead>
        <tbody>
          {allRows.length === 0
            ? emptyRow(5, 'detail.detectBox.noData')
            : allRows.map((r, i) => (
              <tr key={r.key} className={i > 0 && allRows[i - 1].hostname !== r.hostname ? styles.rowStart : ''}>
                {groupCell(allRows, i, true)}
                {dataCell(r)}
                {netCell(r)}
                <td className={styles.lastCell}>{formatAgo(r.measuredAt)}</td>
                {actionCell(allRows, i, detectActions, true)}
              </tr>
            ))}
        </tbody>
      </table>
    </section>
  )

  return (
    <div className={styles.wrap}>
      <p className={styles.hint}>{t('detail.detectBox.hint')}</p>
      {firstTable}
      {reprobeError ? <p className={styles.alert}>{reprobeError}</p> : null}
      {poolError ? <p className={styles.alert}>{t('detail.detectBox.poolFailed', { reason: poolError })}</p> : null}
      {secondTable}
    </div>
  )
}
