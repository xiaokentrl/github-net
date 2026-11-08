import { t } from '../../i18n'
import type { HostsRestorePreview } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import dt from './DetectTables.module.css'
import styles from './RestorePreviewTable.module.css'

// 「还原本菜单」的前后对照表，布局照搬检测弹框的还原表（拍板 2026-11-08，仅借布局）：
// 二级菜单合并格 | 还原前 · 当前 IP | 还原后（结果标签 + 目标 IP）。
// 判据与「清除已写入的 Hosts」预览一致——after 空＝整条删掉、和 before 相同＝不变、其余＝变回。
// 这里喂进来的是「这批二级菜单一起还原」的联合预览，不能由单项目预览拼出来。
type PreviewRow = NonNullable<HostsRestorePreview['rows']>[number]
type Kind = 'restore' | 'remove' | 'keep'

const RESTORE_KEY: Record<Kind, string> = {
  restore: 'detail.detectBox.resultRestore',
  remove: 'detail.detectBox.resultRemove',
  keep: 'detail.detectBox.resultKeep',
}
const RESTORE_CLASS: Record<Kind, string> = {
  restore: dt.tagRestore,
  remove: dt.tagRemove,
  keep: dt.tagKeep,
}
const SUM_KEY: Record<Kind, string> = {
  restore: 'detail.detectBox.sumRestore',
  remove: 'detail.detectBox.sumRemove',
  keep: 'detail.detectBox.sumKeep',
}

// 判据与单项目预览同一套，不另造说法。
function kindOf(r: PreviewRow): Kind {
  if (!r.after) return 'remove'
  if (r.after === r.before) return 'keep'
  return 'restore'
}

export function RestorePreviewTable({ rows, groups, loading, error }: {
  rows: PreviewRow[] | null
  // 域名→二级菜单的归属（ItemDetail 按目录 + Hosts 实写条目算好）；归不上的行落到「—」组，不硬塞。
  groups: { name: string; hostnames: string[] }[]
  loading: boolean
  error: string
}) {
  if (loading) {
    return <p className={dt.hint}>{t('hosts.restoreCategory.previewLoading')}</p>
  }
  if (error) {
    return <p className={dt.alert}>{t('detail.detectBox.previewFailed', { reason: error })}</p>
  }
  const list = rows ?? []
  if (list.length === 0) {
    return <p className={dt.hint}>{t('hosts.restoreCategory.previewEmpty')}</p>
  }

  // 行按二级菜单分组，组序跟 groups 走；联合预览的行序在各组内保持原样。
  // 按组的位置分桶而不是按名字：不同分类可能有同名二级菜单，按名字分会把两桶混成一桶。
  const buckets: PreviewRow[][] = groups.map(() => [])
  const extra: PreviewRow[] = []
  for (const r of list) {
    const gi = groups.findIndex((x) => x.hostnames.includes(r.hostname))
    if (gi >= 0) buckets[gi].push(r)
    else extra.push(r)
  }
  const ordered = [
    ...groups.map((g, i) => ({ name: g.name, rows: buckets[i] })),
    ...(extra.length > 0 ? [{ name: '—', rows: extra }] : []),
  ].filter((g) => g.rows.length > 0)

  const sums: Record<Kind, number> = { restore: 0, remove: 0, keep: 0 }
  for (const r of list) sums[kindOf(r)] += 1

  const afterCell = (r: PreviewRow) => {
    const kind = kindOf(r)
    return (
      <td className={dt.afterCell}>
        <span className={`${dt.restoreTag} ${RESTORE_CLASS[kind]}`}>{t(RESTORE_KEY[kind])}</span>
        {kind === 'remove'
          ? <span className={dt.afterNone}>{t('detail.detectBox.afterRemove')}</span>
          : <span className={dt.ip}>{r.after}</span>}
      </td>
    )
  }

  return (
    <div className={dt.wrap}>
      <section className={`${dt.block} ${dt.blockRestore}`}>
        <div className={styles.sums}>
          {(['restore', 'remove', 'keep'] as const).map((kind) => (
            <span key={kind} className={`${dt.sumChip} ${RESTORE_CLASS[kind]}`}>
              {t(SUM_KEY[kind], { count: sums[kind] })}
            </span>
          ))}
        </div>
        <table className={dt.tbl}>
          <colgroup>
            <col className={styles.cMenu} />
            <col className={styles.cData} />
            <col className={styles.cAfter} />
          </colgroup>
          <thead>
            <tr>
              <th>{t('detail.detectBox.colMenu')}</th>
              <th>{t('detail.detectBox.colWrittenIp')}</th>
              <th>{t('detail.detectBox.colAfter')}</th>
            </tr>
          </thead>
          <tbody>
            {ordered.map((g) =>
              g.rows.map((r, i) => (
                <tr key={r.hostname}>
                  {i === 0 ? (
                    <td className={dt.groupCell} rowSpan={g.rows.length}>
                      <span className={dt.groupArrow}>▸</span>{g.name}
                    </td>
                  ) : null}
                  <td className={dt.dataCell}>
                    <span className={dt.domain}>{r.hostname}</span>
                    <span className={dt.arrow}>→</span>
                    <span className={dt.ip}>{r.before}</span>
                  </td>
                  {afterCell(r)}
                </tr>
              )),
            )}
          </tbody>
        </table>
      </section>
    </div>
  )
}
