import { useEffect, useRef, useState } from 'react'
import type { StrategySnapshot } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { getStrategies, selectStrategy } from '../../api/client'
import { toAppError } from '../../api/errors'
import { t } from '../../i18n'
import { InlineStatus, type SaveKind } from '../../components/ui/InlineStatus'
import { Select } from '../../components/ui/Select'
import styles from './ServiceStrategySelector.module.css'

type Load = 'loading' | 'ready' | 'error'

// 当前服务用哪个加速方法：列表只来自后端注册表，选完立刻存进配置服务。
// 切换本身不启动检测、不写 Hosts，也绝不替用户挑一个——没保存过就显示「还没选」，
// 因为「显示成第一个方法」会让人以为已经在用这个方法（后端同一条规则：不静默换方法）。
export function ServiceStrategySelector({
  scopeId,
  scopeName,
  onSaved,
  detecting,
  onDetectSelected,
}: {
  scopeId: string
  scopeName: string
  onSaved: () => void
  detecting: boolean
  onDetectSelected: (strategyId: string) => void
}) {
  const [snap, setSnap] = useState<StrategySnapshot | null>(null)
  const [load, setLoad] = useState<Load>('loading')
  const [reason, setReason] = useState('')
  const [kind, setKind] = useState<SaveKind>('idle')
  const [text, setText] = useState('')
  // 只认最后一次请求：换分类点得快时，旧范围回来的快照不能盖掉新范围的。
  const requestRef = useRef(0)

  const label = t('strategy.label', { name: scopeName })

  useEffect(() => {
    const request = requestRef.current + 1
    requestRef.current = request
    setLoad('loading')
    setSnap(null)
    setReason('')
    setKind('idle')
    setText('')
    getStrategies(scopeId).then(
      (next) => {
        if (requestRef.current !== request) return
        setSnap(next)
        setLoad('ready')
      },
      (err: unknown) => {
        if (requestRef.current !== request) return
        setReason(toAppError(err).message)
        setLoad('error')
      },
    )
  }, [scopeId])

  const choose = (strategyId: string) => {
    if (!snap) return
    const request = requestRef.current + 1
    requestRef.current = request
    setKind('pending')
    setText(t('settings.save.pending'))
    selectStrategy(scopeId, strategyId, snap.revision, `strategy-${scopeId}-${Date.now()}`).then(
      (next) => {
        if (requestRef.current !== request) return
        setSnap(next)
        // 成功不留提示：选择框里已经是新选中的方法，不需要再喊一句「已保存」。
        setKind('idle')
        setText('')
        // 这次保存把配置版本号抬了一格：让手里也有一份配置的地方重读，不然它下一次保存必撞版本冲突。
        onSaved()
      },
      (err: unknown) => {
        if (requestRef.current !== request) return
        const failed = toAppError(err)
        setKind('failed')
        setText(t('settings.save.failed', { label, reason: failed.message }))
        // 失败最可能是手上这份版本旧了，回读一次让选择框回到后端真实保存的状态；读不到就保留原话。
        getStrategies(scopeId).then(
          (next) => {
            if (requestRef.current !== request) return
            setSnap(next)
            setLoad('ready')
          },
          () => {
            /* 回读也失败：失败原因已经显示在状态条上，这里不再叠第二条 */
          },
        )
      },
    )
  }

  if (load === 'loading') return <p className={styles.note}>{t('strategy.loading')}</p>
  if (load === 'error') return <p className={styles.warn}>{t('strategy.error', { name: scopeName, reason })}</p>

  const options = snap?.options ?? []
  if (options.length === 0) {
    return <p className={styles.warn}>{t('strategy.empty', { name: scopeName })}</p>
  }

  // 后端说这个选择已经不能用了（方法消失了、或它不声明本范围）：选择框回到「还没选」，
  // 并把那句原因显示出来——不能把失效的选择画成生效中，也不能自作主张换成别的。
  const current = snap?.savedId && snap.savedUsable ? snap.savedId : ''
  const notices = snap?.notices ?? []

  // 下拉分组（拍板 2026-10-31）：三个公共源一组按优先序编号，汇总和本机储存一组。
  // 分组归属只对有公共源的范围生效；别的范围（只有 DNS 一个方法）平铺，不给空组。
  const SOURCE_IDS = ['gitcdn', 'hellogithub', 'gitlab']
  const flatOptions = options.map((option) => ({ value: option.id, label: option.name }))
  const sourceOptions = SOURCE_IDS.flatMap((id, index) => {
    const found = options.find((option) => option.id === id)
    return found ? [{ value: found.id, label: `${index + 1}. ${found.name}` }] : []
  })
  const grouped = sourceOptions.length > 0
  const restOptions = grouped
    ? options.filter((option) => !SOURCE_IDS.includes(option.id)).map((option) => ({ value: option.id, label: option.name }))
    : []

  return (
    <div className={styles.stack}>
      <div className={styles.panel} title={t('strategy.hint')}>
        <Select
          label={label}
          value={current}
          placeholder={t('strategy.pickOne')}
          disabled={kind === 'pending'}
          options={flatOptions}
          groups={
            grouped
              ? [
                  { label: t('strategy.group.sources'), options: sourceOptions },
                  { label: t('strategy.group.local'), options: restOptions },
                ]
              : undefined
          }
          onChange={choose}
        />
        {/* 「检测」只检测下拉选中的那一个方法（拍板 2026-10-31）：不改保存的选择，
            直接按当前选中的 ID 发一轮检测。没选中的时候这颗按钮不出现。 */}
        {current !== '' ? (
          <button
            type="button"
            className={styles.detectBtn}
            disabled={kind === 'pending' || detecting}
            title={t('strategy.detectHint', { name: options.find((o) => o.id === current)?.name ?? current })}
            onClick={() => onDetectSelected(current)}
          >
            {detecting ? t('detect.running') : t('strategy.detect')}
          </button>
        ) : null}
      </div>
      {/* 保存状态和提示一律放面板外面：面板里只留选择框和按钮，宽度不再被一句话挤来挤去。 */}
      <InlineStatus kind={kind} text={text} />
      {snap?.savedNote ? <p className={styles.warn}>{t('strategy.stale', { note: snap.savedNote })}</p> : null}
      {current === '' && !snap?.savedNote ? <p className={styles.note}>{t('strategy.unsaved')}</p> : null}
      {notices.length > 0 ? <p className={styles.warn}>{t('strategy.notice', { reason: notices.join('；') })}</p> : null}
    </div>
  )
}
