import { t } from '../../i18n'
import styles from './ui.module.css'

// 失败状态的统一长相：给出原因和重试入口，绝不把失败当空数据或成功显示。
export function ErrorMessage({ reason, onRetry }: { reason: string; onRetry?: () => void }) {
  return (
    <p className={`${styles.line} ${styles.error}`} role="alert">
      <span>{reason}</span>
      {onRetry && (
        <button type="button" onClick={onRetry}>
          {t('common.retry')}
        </button>
      )}
    </p>
  )
}
