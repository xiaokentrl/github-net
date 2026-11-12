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
 * 预选规则：默认选中「使用已有储存」——不重新拉源，直接重验本机已存的可用地址
 * （注意：候选照样要真探测联网，只是省了拉源那一步）；
 * 范围里没有这个方法时退回「已保存的默认方法」。用户手动选过就以用户的为准。
 * 预选解析完成时通过 onPick 上报一次：宿主的确认按钮要拿到与显示一致的方法 ID。
 *
 * 组件只出下拉：执行按钮由宿主自己放（两个调用方——档位弹框与一站式检测台——
 * 都有自己的确认按钮）。自带「用「X」测本项目」按钮的形态因零调用点已删
 * （2026-11-13 按钮审计：矩阵死行「方法下拉旁的检测按钮」）。
 * 与页面头部的 ServiceStrategySelector 是两回事：头部那颗选完会存成默认。
 */
export function MethodDetectPicker({
  scopeId,
  compact,
  onPick,
  onReady,
}: {
  scopeId: string
  /** 横排紧凑模式：下拉单独一行横排；缺省纵排 */
  compact?: boolean
  /** 选择变化（含预选解析完成）时回调；宿主靠它拿选中的方法 ID */
  onPick?: (strategyId: string) => void
  /** 清单状态通知：true＝预选就绪可确认；false＝读失败/清单为空（宿主应退默认方法链并放开确认） */
  onReady?: (ready: boolean) => void
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

  // 清单状态通知（审计缺口 3）：读失败或清单为空时如实通知宿主——
  // 别让「确认永久灰＋title 读取中」把用户卡死在弹框里。
  useEffect(() => {
    if (load === 'loading') return
    onReady?.(load === 'ready' && current !== '')
  }, [load, current, onReady])

  if (load === 'loading') return <p className={styles.note}>{t('strategy.loading')}</p>
  if (load === 'error') return <p className={styles.warn}>{t('strategy.error', { name: scopeId, reason })}</p>

  if (options.length === 0) return <p className={styles.warn}>{t('strategy.empty', { name: scopeId })}</p>

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
    </div>
  )
}
