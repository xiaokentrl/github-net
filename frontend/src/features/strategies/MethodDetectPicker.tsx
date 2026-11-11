import { useEffect, useRef, useState } from 'react'
import type { StrategySnapshot } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { getStrategies } from '../../api/client'
import { toAppError } from '../../api/errors'
import { t } from '../../i18n'
import { Select } from '../../components/ui/Select'
import styles from './ServiceStrategySelector.module.css'

type Load = 'loading' | 'ready' | 'error'

/**
 * 「下拉选方法」公共组件（拍板 2026-11-12）：
 * 方法清单按传入的范围取（内容随菜单变化），选择只活在本组件里——
 * 不存默认方法、不写配置，结果通过 onPick / onDetect 交给调用方。
 *
 * 预选规则：默认选中「使用已有储存」——不联网、秒出结论，是最稳的起点；
 * 范围里没有这个方法时退回「已保存的默认方法」。用户手动选过就以用户的为准。
 * 预选解析完成时通过 onPick 上报一次：宿主的确认按钮要拿到与显示一致的方法 ID。
 *
 * 两种形态：
 * - showButton（缺省）：组件自带「用「X」测本项目」按钮；
 * - showButton=false + compact：纯下拉横排（档位弹框里用，宿主自己放确认按钮）。
 * 与页面头部的 ServiceStrategySelector 是两回事：头部那颗选完会存成默认。
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
  /** 横排紧凑模式：下拉与按钮同一行；缺省纵排 */
  compact?: boolean
  /** 只要下拉不要按钮（宿主自己有确认按钮时） */
  showButton?: boolean
  /** 选择变化（含预选解析完成）时回调；showButton=false 的宿主靠它拿选中的方法 ID */
  onPick?: (strategyId: string) => void
}) {
  const [snap, setSnap] = useState<StrategySnapshot | null>(null)
  const [load, setLoad] = useState<Load>('loading')
  const [reason, setReason] = useState('')
  const [pickedId, setPickedId] = useState('')
  const requestRef = useRef(0)

  const options = snap?.options ?? []
  // 预选解析（放在 hooks 之后、早退之前）：默认「使用已有储存」，没有再退「已保存的默认方法」。
  const saved = snap?.savedId && snap.savedUsable ? snap.savedId : ''
  const storedOption = options.find((o) => o.id === 'stored-ips')
  const preferred = storedOption?.id ?? saved
  const current = pickedId || preferred

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

  // 预选解析完成时上报一次：宿主的确认按钮要拿到与显示一致的方法 ID——
  // 不上报的话，显示「使用已有储存」、确认却走默认方法链，两边对不上。
  useEffect(() => {
    if (load !== 'ready' || pickedId !== '' || current === '') return
    onPick?.(current)
  }, [load, pickedId, current, onPick])

  if (load === 'loading') return <p className={styles.note}>{t('strategy.loading')}</p>
  if (load === 'error') return <p className={styles.warn}>{t('strategy.error', { name: scopeId, reason })}</p>

  if (options.length === 0) return <p className={styles.warn}>{t('strategy.empty', { name: scopeId })}</p>

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

  const pick = (strategyId: string) => {
    setPickedId(strategyId)
    onPick?.(strategyId)
  }

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
        onChange={pick}
      />
      {showButton && current !== '' ? (
        <button
          type="button"
          className={styles.detectBtn}
          disabled={detecting}
          title={t('strategy.detectHint', { name: picked?.name ?? current })}
          onClick={() => onDetect?.(current)}
        >
          {detecting ? t('detect.running') : t('strategy.detectProject', { name: picked?.name ?? current })}
        </button>
      ) : null}
    </div>
  )
}
