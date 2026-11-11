import { useEffect, useRef, useState } from 'react'
import type { StrategySnapshot } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { getStrategies } from '../../api/client'
import { toAppError } from '../../api/errors'
import { t } from '../../i18n'
import { Select } from '../../components/ui/Select'
import styles from './ServiceStrategySelector.module.css'

type Load = 'loading' | 'ready' | 'error'

/**
 * 「下拉选方法 + 只测本项目」公共组件（拍板 2026-11-12）：
 * 方法清单按传入的范围取（内容随菜单变化），选择只活在本组件里——
 * 不存默认方法、不写配置，检测回调把选中的方法 ID 交给调用方。
 * 按钮写结果：选了什么就显示「用「X」测本项目」。
 * 和页面头部的 ServiceStrategySelector 是两回事：头部那颗选完会存成默认。
 */
export function MethodDetectPicker({
  scopeId,
  detecting,
  onDetect,
  compact,
  showButton = true,
  onPick,
}: {
  scopeId: string
  /** 检测进行中：按钮置灰用；showButton=false 的宿主可不传 */
  detecting?: boolean
  /** 检测回调（showButton=true 时必传） */
  onDetect?: (strategyId: string) => void
  /** 横排紧凑模式：下拉与按钮同一行（弹框里用）；缺省纵排（页面头部用） */
  compact?: boolean
  /** 只要下拉不要按钮（宿主自己有确认按钮时，如档位弹框） */
  showButton?: boolean
  /** 选择变化的回调（showButton=false 时宿主靠它拿选中的方法 ID） */
  onPick?: (strategyId: string) => void
}) {
  const [snap, setSnap] = useState<StrategySnapshot | null>(null)
  const [load, setLoad] = useState<Load>('loading')
  const [reason, setReason] = useState('')
  const [pickedId, setPickedId] = useState('')
  const requestRef = useRef(0)

  useEffect(() => {
    const request = requestRef.current + 1
    requestRef.current = request
    setLoad('loading')
    setSnap(null)
    setReason('')
    setPickedId('')
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

  if (load === 'loading') return <p className={styles.note}>{t('strategy.loading')}</p>
  if (load === 'error') return <p className={styles.warn}>{t('strategy.error', { name: scopeId, reason })}</p>

  const options = snap?.options ?? []
  if (options.length === 0) return <p className={styles.warn}>{t('strategy.empty', { name: scopeId })}</p>

  // 默认选中「已保存的默认方法」（若可用）：多数时候用户就是想用它，少点一步。
  const saved = snap?.savedId && snap.savedUsable ? snap.savedId : ''
  const current = pickedId || saved
  const picked = options.find((o) => o.id === current)

  const flatOptions = options.map((option) => ({ value: option.id, label: option.name }))
  const SOURCE_IDS = ['gitcdn', 'hellogithub', 'gitlab']
  const sourceOptions = SOURCE_IDS.flatMap((id, index) => {
    const found = options.find((option) => option.id === id)
    return found ? [{ value: found.id, label: `${index + 1}. ${found.name}` }] : []
  })
  const grouped = sourceOptions.length > 0
  const restOptions = grouped
    ? options.filter((option) => !SOURCE_IDS.includes(option.id)).map((option) => ({ value: option.id, label: option.name }))
    : []

  return (
    <div className={compact ? `${styles.stack} ${styles.stackRow}` : styles.stack}>
      <Select
        label={t('strategy.label', { name: scopeId })}
        labelHidden
        value={current}
        placeholder={t('strategy.pickOne')}
        options={flatOptions}
        groups={
          grouped
            ? [
                { label: t('strategy.group.sources'), options: sourceOptions },
                { label: t('strategy.group.local'), options: restOptions },
              ]
            : undefined
        }
        onChange={(id) => {
          setPickedId(id)
          onPick?.(id)
        }}
      />
      {showButton && current !== '' ? (
        <button
          type="button"
          className={styles.detectBtn}
          disabled={detecting}
          title={t('strategy.detectHint', { name: picked?.name ?? current })}
          onClick={() => onDetect?.(current)}
        >
          {/* 按钮写结果：标签恒为「用「X」测本项目」。检测中（无论哪路检测在跑）只置灰，
              不换成「检测中」——弹框里可能有另一路检测全部在跑，换文案会让人以为选的方法正在用。 */}
          {t('strategy.detectProject', { name: picked?.name ?? current })}
        </button>
      ) : null}
    </div>
  )
}
