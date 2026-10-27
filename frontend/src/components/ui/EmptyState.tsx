import styles from './ui.module.css'

// 真的没有数据时才用空状态；加载中、失败中不能借用它显示。
export function EmptyState({ message }: { message: string }) {
  return (
    <p className={`${styles.line} ${styles.empty}`} role="status">
      {message}
    </p>
  )
}
