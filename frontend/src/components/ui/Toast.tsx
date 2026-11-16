import { useNotices } from './toastStore'
import { t } from '../../i18n'
import styles from './Toast.module.css'

// Toast 提示（2026-11-15 重写，照中央指南库 Toast 模式）：右下角浮动堆叠，
// 每条到点自动消失——替代旧的常驻提示面板＋「提示」胶囊（用户拍板重写）。
// 只负责「此刻让你看见」：写入结果在树上有持久状态（已替换/未替换），
// 设置在设置中心有持久状态，Toast 消失不丢事实。无面板壳、无拖拽、无按钮。
export function Toast() {
  const notices = useNotices()
  if (notices.length === 0) return null
  return (
    <div className={styles.region} aria-label={t('ui.toast.region')}>
      {notices.map((notice) => (
        <div
          key={notice.id}
          role={notice.kind === 'error' ? 'alert' : 'status'}
          className={`${styles.toast} ${styles[notice.kind]}`}
        >
          {notice.text}
        </div>
      ))}
    </div>
  )
}
