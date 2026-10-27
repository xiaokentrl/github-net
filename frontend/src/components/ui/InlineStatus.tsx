import styles from './ui.module.css'

export type SaveKind = 'idle' | 'pending' | 'ok' | 'failed'

// 保存中、已保存、保存失败三态分开展示；后端没确认成功之前不能显示成已生效。
export function InlineStatus({ kind, text }: { kind: SaveKind; text: string }) {
  if (kind === 'idle') return null
  const tone = kind === 'pending' ? styles.pending : kind === 'ok' ? styles.ok : styles.failed
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
