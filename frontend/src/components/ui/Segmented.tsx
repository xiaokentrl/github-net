import styles from './ui.module.css'

export type SegmentOption<T extends number | string = number> = { value: T; label: string }

// 一组互斥预设（窄/标准/宽）。用 aria-pressed 标明当前选中，
// 不用 radio：这里改的是已保存的设置值，不是一组同名的候选。
// 值类型泛型：数字档（宽度/高度/缩放）右边带当前值读数，字符串档（语言）没有读数。
export function Segmented<T extends number | string>({
  label,
  value,
  options,
  disabled,
  disabledHint,
  formatValue,
  onChange,
}: {
  label: string
  value: T
  options: SegmentOption<T>[]
  disabled?: boolean
  disabledHint?: string
  // 右边那个当前值默认按像素读；缩放那组是百分比，自己给读法；字符串档不显示读数。
  formatValue?: (value: T) => string
  // 整组不可用时没有可改的值，所以这里允许不传。
  onChange?: (value: T) => void
}) {
  const readout =
    formatValue !== undefined ? formatValue(value) : typeof value === 'number' ? `${value} px` : ''
  return (
    <div className={styles.segGroup}>
      <div className={styles.seg} role="group" aria-label={label}>
        {options.map((option) => (
          <button
            key={option.value}
            type="button"
            className={option.value === value ? styles.segOn : ''}
            aria-pressed={option.value === value}
            disabled={disabled}
            onClick={() => onChange?.(option.value)}
          >
            {option.label}
          </button>
        ))}
      </div>
      {readout !== '' && <span className={styles.segValue}>{readout}</span>}
      {disabled && disabledHint && <p className={styles.hint}>{disabledHint}</p>}
    </div>
  )
}
