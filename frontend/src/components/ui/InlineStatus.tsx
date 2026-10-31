import styles from './ui.module.css'

export type SaveKind = 'idle' | 'pending' | 'failed'

// 保存成功不留提示（成功的改动用户自己看得到，版本号是内部细节）；只显示保存中和保存失败。
export function InlineStatus({ kind, text }: { kind: SaveKind; text: string }) {
  if (kind === 'idle') return null
  const tone = kind === 'pending' ? styles.pending : styles.failed
  return (
    <span
      className={`${styles.status} ${tone}`}
      role={kind === 'failed' ? 'alert' : 'status'}
      aria-live="polite"
    >
      {text}
    </span>
  )
}
