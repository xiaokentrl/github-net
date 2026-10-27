import styles from './ui.module.css'

export type SegmentOption = { value: number; label: string }

// 一组互斥预设（窄/标准/宽）。用 aria-pressed 标明当前选中，
// 不用 radio：这里改的是已保存的设置值，不是一组同名的候选。
export function Segmented({
  label,
  value,
  options,
  disabled,
  disabledHint,
  onChange,
}: {
  label: string
  value: number
  options: SegmentOption[]
  disabled?: boolean
  disabledHint?: string
  // 整组不可用时没有可改的值，所以这里允许不传。
  onChange?: (value: number) => void
}) {
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
      <span className={styles.segValue}>{value} px</span>
      {disabled && disabledHint && <p className={styles.hint}>{disabledHint}</p>}
    </div>
  )
}
