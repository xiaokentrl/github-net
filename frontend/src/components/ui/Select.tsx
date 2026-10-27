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
  onChange,
}: {
  label: string
  value: string
  options: SelectOption[]
  disabled?: boolean
  onChange: (value: string) => void
}) {
  return (
    <label className={styles.field}>
      <span>{label}</span>
      <select
        value={value}
        disabled={disabled}
        onChange={(event: ChangeEvent<HTMLSelectElement>) => onChange(event.target.value)}
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  )
}
