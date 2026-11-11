import { useEffect, useRef, useState, type ReactNode } from 'react'
import { getLastDetectProfile, type NetworkProfile } from '../../api/client'
import { t } from '../../i18n'
import { MethodDetectPicker } from '../strategies/MethodDetectPicker'
import { getErrorMessage } from '../../api/errors'
import { getCandidatePool, previewRestoreHostsItem, reprobeIP } from '../../api/client'
import type { HostEntryState, HostsRestorePreview, PoolEntryView } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import styles from './DetectTables.module.css'

// 上下两张表各管一件事，形态刻意不同，别让人看成同一张表复制了两遍：
//   ① 还原：只放支持还原的 IP，并把「还原前 → 还原后」并排列出来，
//           按结果分成变回原 IP / 删掉映射 / IP 不变三组，让用户先看清差异再点还原。
//   ② 检测与写入：放这个二级菜单下逐条列出的全部候选 IP，「检测全部」刷新全部网络状态，
//           主按钮是「检测全部」，多一列「上次检测」说明结论新旧。
// 还原前后对比走后端「按这个项目还原」的只读预览：共享域名在其他项目还认领时 IP 不变，
// 拿全量还原的结果来报会把那一行错标成删掉／变回。网络状态一律来自后端定格。

type NetState = { status?: string; latencyMs?: number }
// 还原结果按「点下去会变成什么」分三档，未知＝预览还没读到，绝不猜。
type RestoreKind = 'restore' | 'remove' | 'keep' | 'unknown'
type Row = {
  key: string
  hostname: string
  ip: string
  net: NetState
  measuredAt?: string
  restore?: { kind: RestoreKind; after: string }
}

type Actions = {
  restoreIp: () => void
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

const RESTORE_KEY: Record<RestoreKind, string> = {
  restore: 'detail.detectBox.resultRestore',
  remove: 'detail.detectBox.resultRemove',
  keep: 'detail.detectBox.resultKeep',
  unknown: 'detail.detectBox.resultUnknown',
}
const RESTORE_CLASS: Record<RestoreKind, string> = {
  restore: styles.tagRestore,
  remove: styles.tagRemove,
  keep: styles.tagKeep,
  unknown: styles.tagUnknown,
}
const RESTORE_SUM_KEY: Record<'restore' | 'remove' | 'keep', string> = {
  restore: 'detail.detectBox.sumRestore',
  remove: 'detail.detectBox.sumRemove',
  keep: 'detail.detectBox.sumKeep',
}

// 判据与「清除已写入的 Hosts」预览一致：after 空＝整条撤掉，和 before 相同＝不变，其余＝变回。
// 但这里喂进来的是单项目还原预览，所以共享域名会正确落到 keep。
function restoreKindOf(after: string, before: string): RestoreKind {
  if (!after) return 'remove'
  if (after === before) return 'keep'
  return 'restore'
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

function CardHead({ step, title, desc, countLabel, tone, extra }: {
  step?: string
  title: string
  desc: string
  countLabel: string
  tone: 'restore' | 'detect'
  extra?: ReactNode
}) {
  return (
    <div className={`${styles.cardHead} ${tone === 'restore' ? styles.headRestore : styles.headDetect}`}>
      <div className={styles.headLine}>
        {step ? <span className={styles.headStep}>{step}</span> : null}
        <h4 className={styles.headTitle}>{title}</h4>
        <span className={styles.headCount}>{countLabel}</span>
      </div>
      <p className={styles.headDesc}>{desc}</p>
      {extra ? <div className={styles.sumRow}>{extra}</div> : null}
    </div>
  )
}

export function DetectTables({ name, itemId, hostnames, entries, actions, busy, ready, refreshKey, only, onReprobed, scopeId, detectRun }: {
  name: string
  // 还原前后对比按这一个二级菜单算，不能拿全量还原的结果来顶。
  itemId: string
  hostnames: string[]
  entries: HostEntryState[]
  actions: Actions
  busy: Busy
  ready: Ready
  // 检测/还原/写入任务状态一变就重拉候选池与还原预览，表里始终是刚落定的真实数据。
  refreshKey: string
  // 拆分入口（拍板 2026-11-04）：只渲染其中一张表；不传＝两张都渲染（旧形态）。
  only?: 'restore' | 'detect'
  // 「重新检测」整轮收尾后的通知：后端候选池已更新，调用方重拉结果 store，
  // 详情页的候选表才不会停在旧实测状态。
  onReprobed?: () => void
  /** 项目所属范围：给「下拉选方法」取方法清单（一站式检测台） */
  scopeId: string
  /** 弹框内执行：按选中的方法与档位对这一个项目开一轮检测 */
  detectRun: (strategyId: string, profile: NetworkProfile) => void
}) {
  const [pool, setPool] = useState<PoolEntryView[] | null>(null)
  const [poolError, setPoolError] = useState('')
  const [restorePreview, setRestorePreview] = useState<HostsRestorePreview | null>(null)
  const [restorePreviewError, setRestorePreviewError] = useState('')
  // 第一张表现场复测的结论按「域名|IP」存，覆盖后端读回来的旧定格。
  const [probeOverrides, setProbeOverrides] = useState<Record<string, NetState>>({})
  const [reprobe, setReprobe] = useState<{ done: number; total: number } | null>(null)
  // 一站式检测台（拍板 2026-11-12）：选源（方法）与档位都在弹框里定，执行走 detectRun。
  // pickedStrategyId 为空＝用默认方法链（已保存的默认方法）；档位初值读上次记住的那一档。
  const [pickedStrategyId, setPickedStrategyId] = useState('')
  const [pickedProfile, setPickedProfile] = useState<NetworkProfile>('low')
  useEffect(() => {
    let alive = true
    getLastDetectProfile().then((profile) => {
      if (alive && (profile === 'low' || profile === 'standard' || profile === 'full')) setPickedProfile(profile)
    }).catch(() => undefined)
    return () => { alive = false }
  }, [])
  const [reprobeError, setReprobeError] = useState('')
  const alive = useRef(true)
  useEffect(() => () => { alive.current = false }, [])

  // 按需拉取（拍板 2026-11-04）：还原弹框用不到候选池、检测弹框用不到还原预览，
  // 各自只拉自己那张表要用的数据，弹框打开更快。
  useEffect(() => {
    if (only === 'restore') return
    let live = true
    getCandidatePool()
      .then((rows) => { if (live) { setPool(rows); setPoolError(''); setProbeOverrides({}) } })
      .catch((err: unknown) => { if (live) setPoolError(getErrorMessage(err)) })
    return () => { live = false }
  }, [refreshKey, only])

  // 还原前后对比：只读，不写 Hosts。特权服务没起来 / 读失败时把原因摆在表里，
  // 「还原后」一列留空，不拿别的项目或全量还原的结论糊上去。
  useEffect(() => {
    if (only === 'detect') return
    let live = true
    previewRestoreHostsItem(itemId)
      .then((res) => {
        if (!live) return
        // 单项预览固定报 1 个项目；>1 说明对端还是旧版特权服务，把 itemID 忽略了、回了全量结果。
        // 拿全量结果展示会把共享域名错标成删掉／变回，宁可明说读不出来。
        if (res.items > 1) {
          setRestorePreview(null)
          setRestorePreviewError(t('detail.detectBox.previewStaleHelper'))
          return
        }
        setRestorePreview(res)
        setRestorePreviewError('')
      })
      .catch((err: unknown) => {
        if (live) { setRestorePreview(null); setRestorePreviewError(getErrorMessage(err)) }
      })
    return () => { live = false }
  }, [itemId, refreshKey, only])

  const afterByHost = new Map<string, { before: string; after: string }>()
  for (const row of restorePreview?.rows ?? []) {
    afterByHost.set(row.hostname, { before: row.before, after: row.after })
  }
  const restoreLoading = restorePreview === null && restorePreviewError === ''

  // ① 还原：只列支持还原的 IP。written 是后端按认领关系判出来的「本项目写进 Hosts 的映射」，
  // 网络不通不排除它——那正是旁边「这条 IP 通不通」这一列要说的事。
  const restorableRows: Row[] = entries.flatMap((e) => {
    const ip = e.ip
    if (!e.written || !ip) return []
    const key = `${e.hostname}|${ip}`
    const hit = afterByHost.get(e.hostname)
    return [{
      key,
      hostname: e.hostname,
      ip,
      net: probeOverrides[key] ?? { status: e.probeStatus, latencyMs: e.latencyMs },
      measuredAt: e.measuredAt,
      restore: hit ? { kind: restoreKindOf(hit.after, hit.before), after: hit.after } : undefined,
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
  const sums: Record<'restore' | 'remove' | 'keep', number> = { restore: 0, remove: 0, keep: 0 }
  for (const r of restorableRows) {
    const kind = r.restore?.kind
    if (kind === 'restore' || kind === 'remove' || kind === 'keep') sums[kind]++
  }
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
    // 整轮收尾：后端候选池已是新实测，通知调用方重拉结果 store——
    // 不然弹框里看得到新状态、关掉后详情页的候选表还是旧的。
    onReprobed?.()
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

  // 一站式检测台：方法下拉（默认=已保存的默认方法）+ 档位三选一（初值=上次记住的档）。
  const tierButtons = (
    <div className={styles.tierRow}>
      {(['low', 'standard', 'full'] as const).map((id) => (
        <button
          key={id}
          type="button"
          className={`${styles.tierBtn} ${pickedProfile === id ? styles.tierOn : ''}`}
          aria-pressed={pickedProfile === id}
          onClick={() => setPickedProfile(id)}
        >
          {t(`toolbar.profile.${id}`)}
        </button>
      ))}
    </div>
  )
  // ② 的主按钮是「开始检测」，写入是测完之后的动作，用另一种样式但同样可点。
  const detectActions = (
    <div className={styles.actionStack}>
      {button('detail.detectBox.detectRun', 'detail.detectBox.detectRunHint', () =>
        detectRun(pickedStrategyId, pickedProfile),
        busy.detect || reprobe !== null, 'detect', 'detail.box.detecting')}
      {button('detail.detectBox.writeIp', 'detail.detectBox.writeHint', actions.writeIp,
        busy.apply || !hasUsableIp || !ready.hasDetectResult, 'write',
        !hasUsableIp || !ready.hasDetectResult ? 'detail.detectBox.noUsableIp' : 'detail.box.restoring')}
    </div>
  )

  // 两张表共用的几格：二级菜单合并、域名→IP、网络状态。列数与列头由各自的表决定。
  // 二级菜单合并格上下居中、操作列按钮靠下，两张表口径一致。
  const groupCell = (rows: Row[], i: number) => i === 0 ? (
    <td className={styles.groupCell} rowSpan={rows.length}>
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
  // 还原后：结果标签 + 目标 IP，三种结果各一色。预览没读到就明说读不出来，不编。
  const afterCell = (r: Row) => {
    if (restoreLoading) {
      return <td className={styles.afterCell}><span className={styles.dim}>{t('detail.detectBox.previewLoading')}</span></td>
    }
    const kind = r.restore?.kind ?? 'unknown'
    return (
      <td className={styles.afterCell}>
        <span className={`${styles.restoreTag} ${RESTORE_CLASS[kind]}`}>{t(RESTORE_KEY[kind])}</span>
        {kind === 'remove'
          ? <span className={styles.afterNone}>{t('detail.detectBox.afterRemove')}</span>
          : kind === 'unknown'
            ? <span className={styles.dim}>—</span>
            : <span className={styles.ip}>{r.restore?.after}</span>}
      </td>
    )
  }
  const netCell = (r: Row) => <td className={styles.netCell}><NetTag net={r.net} /></td>
  // 操作列整表合并成一格，按钮组靠下：看完数据再动手。
  const actionCell = (rows: Row[], i: number, node: ReactNode) => i === 0 ? (
    <td className={styles.actionCell} rowSpan={rows.length}>{node}</td>
  ) : null
  const emptyRow = (colSpan: number, key: string) => (
    <tr><td className={styles.empty} colSpan={colSpan}>{t(key)}</td></tr>
  )

  const restoreSums = (['restore', 'remove', 'keep'] as const).map((kind) => (
    <span key={kind} className={`${styles.sumChip} ${RESTORE_CLASS[kind]}`}>
      {t(RESTORE_SUM_KEY[kind], { count: sums[kind] })}
    </span>
  ))

  const firstTable = (
    <section className={`${styles.block} ${styles.blockRestore}`}>
      <CardHead
        step="①"
        title={t('detail.detectBox.firstTitle')}
        desc={t('detail.detectBox.firstDesc')}
        countLabel={t('detail.detectBox.restorable', { count: restorableCount })}
        tone="restore"
        extra={restoreLoading || restorePreviewError ? undefined : restoreSums}
      />
      <table className={styles.tbl}>
        <colgroup>
          <col className={styles.cMenu} />
          <col className={styles.cData} />
          <col className={styles.cNet} />
          <col className={styles.cAfter} />
          <col className={styles.cAction} />
        </colgroup>
        <thead>
          <tr>
            <th>{t('detail.detectBox.colMenu')}</th>
            <th>{t('detail.detectBox.colWrittenIp')}</th>
            <th className={styles.centered}>{t('detail.detectBox.colNetFirst')}</th>
            <th>{t('detail.detectBox.colAfter')}</th>
            <th className={styles.centered}>{t('detail.detectBox.colAction')}</th>
          </tr>
        </thead>
        <tbody>
          {restorableRows.length === 0
            ? emptyRow(5, 'detail.detectBox.restorableEmpty')
            : restorableRows.map((r, i) => (
              <tr key={r.key}>
                {groupCell(restorableRows, i)}
                {dataCell(r)}
                {netCell(r)}
                {afterCell(r)}
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
      <div className={styles.runSetup}>
        <div className={styles.runRow}>
          <span className={styles.runLabel}>{t('detail.detectBox.methodLabel')}</span>
          <MethodDetectPicker scopeId={scopeId} detecting={busy.detect} onPick={setPickedStrategyId} compact showButton={false} />
        </div>
        <div className={styles.runRow}>
          <span className={styles.runLabel}>{t('detail.detectBox.tierLabel')}</span>
          {tierButtons}
        </div>
      </div>
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
                {groupCell(allRows, i)}
                {dataCell(r)}
                {netCell(r)}
                <td className={styles.lastCell}>{formatAgo(r.measuredAt)}</td>
                {actionCell(allRows, i, detectActions)}
              </tr>
            ))}
        </tbody>
      </table>
    </section>
  )

  const showRestore = only !== 'detect'
  const showDetect = only !== 'restore'
  return (
    <div className={styles.wrap}>
      {only === undefined ? <p className={styles.hint}>{t('detail.detectBox.hint')}</p> : null}
      {showRestore ? firstTable : null}
      {showRestore && restorePreviewError
        ? <p className={styles.alert}>{t('detail.detectBox.previewFailed', { reason: restorePreviewError })}</p>
        : null}
      {showRestore && reprobeError ? <p className={styles.alert}>{reprobeError}</p> : null}
      {showDetect && poolError ? <p className={styles.alert}>{t('detail.detectBox.poolFailed', { reason: poolError })}</p> : null}
      {showDetect ? secondTable : null}
    </div>
  )
}
