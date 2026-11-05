import { t } from '../../i18n'
import { stateLabel, type ProjectState } from '../../features/tasks/statusTone'
import styles from './StatusBadge.module.css'

// 徽章不再自己判状态：调用方用 projectState 算好，这里只按四档显示同一句话、同一个色。
const CLASS: Record<ProjectState['tone'], string> = {
  working: styles.working,
  written: styles.written,
  failed: styles.failed,
  unwritten: styles.unwritten,
}

export function StatusBadge({ state }: { state: ProjectState }) {
  return (
    <span className={`${styles.badge} ${CLASS[state.tone]}`} title={state.reason}>
      <i className={styles.dot} aria-hidden="true" />
      {t(stateLabel(state))}
    </span>
  )
}
