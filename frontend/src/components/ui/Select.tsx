import type { ChangeEvent } from 'react'
import styles from './ui.module.css'

export type SelectOption = { value: string; label: string }

// 这一版用原生 select：键盘、读屏和弹层定位都是浏览器负责的，不会有
// 「下拉挂错位置」那类坑。规划里的自定义弹层 Select 等做完整设置界面时再换，
// 换的时候只改这个文件，调用方不动。
export function Select({
  label,
  value,
  options,
  disabled,
  disabledHint,
  placeholder,
  onChange,
}: {
  label: string
  value: string
  options: SelectOption[]
  disabled?: boolean
  disabledHint?: string
  // 「还没选」要能被显示出来：不给占位项时，浏览器会把没有匹配值的选择框显示成第一项，
  // 那等于把没保存过的状态画成已经选了第一个方法。占位项禁用、不可选，只负责把空状态说清。
  placeholder?: string
  // 整组不可用时没有可改的值，所以这里允许不传（与 Segmented 同一形状）。
  onChange?: (value: string) => void
}) {
  return (
    <div className={styles.fieldGroup}>
      <label className={styles.field}>
        <span>{label}</span>
        <select
          value={value}
          disabled={disabled}
          onChange={(event: ChangeEvent<HTMLSelectElement>) => onChange?.(event.target.value)}
        >
          {placeholder !== undefined && <option value="" disabled>{placeholder}</option>}
          {options.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
            </option>
          ))}
        </select>
      </label>
      {disabled && disabledHint && <p className={styles.hint}>{disabledHint}</p>}
    </div>
  )
}
