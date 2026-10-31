import { useEffect, useRef, useState } from 'react'
import { t } from '../../i18n'
import styles from './ui.module.css'

export type MultiSelectOption = { value: string; label: string }

// 下拉多选：按钮上写选了几个，点开是一份勾选清单，勾一下就改一次配置。
// 勾选行用原生 checkbox，键盘（Tab、空格）和读屏交给浏览器；
// 「点外面收起」和「Esc 收起」浏览器不管，这里自己接。
// 面板开着按 Esc 时要把事件挡住，不然设置对话框那条「Esc 关整个对话框」会一起触发。
export function MultiSelect({
  options,
  value,
  labelText,
  emptyText,
  selectedText,
  disabled,
  disabledHint,
  onChange,
}: {
  options: MultiSelectOption[]
  value: string[]
  // 这一栏叫什么（卡片标题）。按钮上只显示「选了几个」，读屏光念那一句会听不出这是在改哪一栏，
  // 所以把名字接进按钮的可读名称里。
  labelText: string
  // 一个都没选时按钮上的那句话（这一屏用它写「不自动检测」）。
  emptyText: string
  // 选了以后按钮上的那句话，{count} 是选中个数；不传就用默认的「已选 {count} 项」。
  selectedText?: string
  disabled?: boolean
  disabledHint?: string
  onChange: (value: string[]) => void
}) {
  const [open, setOpen] = useState(false)
  const wrapRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)
  const picked = new Set(value)

  // 点控件外面收起。挂在 document 上而不是面板上，面板里没有那个点的落点。
  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: PointerEvent) => {
      if (!wrapRef.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    return () => document.removeEventListener('pointerdown', onPointerDown)
  }, [open])

  const toggle = (item: string, on: boolean) => {
    const next = new Set(picked)
    if (on) next.add(item)
    else next.delete(item)
    // 顺序照选项表的顺序给，不按点击顺序：存进配置的清单要稳定，
    // 同样几个菜单不该因为先后点法不同写成两种顺序。
    const ordered = options.filter((option) => next.has(option.value)).map((option) => option.value)
    onChange(ordered)
  }

  const summary = picked.size === 0 ? emptyText : (selectedText ?? t('ui.multiSelect.selected')).replace('{count}', String(picked.size))

  return (
    <div className={styles.msWrap} ref={wrapRef} onKeyDown={(event) => {
      if (!open || event.key !== 'Escape') return
      event.stopPropagation()
      setOpen(false)
      buttonRef.current?.focus()
    }}>
      <button
        ref={buttonRef}
        type="button"
        className={styles.msButton}
        disabled={disabled}
        aria-expanded={open}
        aria-haspopup="true"
        aria-label={`${labelText}：${summary}`}
        onClick={() => setOpen((prev) => !prev)}
      >
        <span className={styles.msSummary}>{summary}</span>
        <span className={styles.msCaret} aria-hidden="true">{open ? '▴' : '▾'}</span>
      </button>
      {open && (
        <div className={styles.msPanel} role="group">
          <div className={styles.msTools}>
            <button type="button" className={styles.msTool} onClick={() => onChange(options.map((option) => option.value))}>
              {t('ui.multiSelect.selectAll')}
            </button>
            <button type="button" className={styles.msTool} onClick={() => onChange([])}>
              {t('ui.multiSelect.clear')}
            </button>
          </div>
          {options.length === 0 && <p className={styles.msNone}>{t('ui.multiSelect.none')}</p>}
          {options.map((option) => (
            <label key={option.value} className={styles.msOption}>
              <input
                type="checkbox"
                checked={picked.has(option.value)}
                disabled={disabled}
                onChange={(event) => toggle(option.value, event.target.checked)}
              />
              {option.label}
            </label>
          ))}
        </div>
      )}
      {disabled && disabledHint && <p className={styles.hint}>{disabledHint}</p>}
    </div>
  )
}
