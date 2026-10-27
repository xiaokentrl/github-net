import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import styles from './StatusBadge.module.css'

// 存文案键而不是成品句子：语言是后端设置里的值，切语言时文案要跟着走。
// $zero 是 Go 零值，表示后端根本没给状态，按「未读取」显示，不编出新含义。
const TEXT_KEY: Record<HostsStatus, string> = {
  [HostsStatus.$zero]: 'badge.unknown',
  [HostsStatus.HostsUnknown]: 'badge.unknown',
  [HostsStatus.HostsNone]: 'badge.none',
  [HostsStatus.HostsPartial]: 'badge.partial',
  [HostsStatus.HostsApplied]: 'badge.applied',
  [HostsStatus.HostsConflict]: 'badge.conflict',
  [HostsStatus.HostsReadFailed]: 'badge.readFailed',
}

// 未读取用中性色：它是「后端还没读过」，不能长得像一种检测结论。
const TONE: Record<HostsStatus, 'neutral' | 'good' | 'warn' | 'bad'> = {
  [HostsStatus.$zero]: 'neutral',
  [HostsStatus.HostsUnknown]: 'neutral',
  [HostsStatus.HostsNone]: 'neutral',
  [HostsStatus.HostsPartial]: 'warn',
  [HostsStatus.HostsApplied]: 'good',
  [HostsStatus.HostsConflict]: 'bad',
  [HostsStatus.HostsReadFailed]: 'bad',
}

const CLASS: Record<'neutral' | 'good' | 'warn' | 'bad', string> = {
  neutral: styles.neutral,
  good: styles.good,
  warn: styles.warn,
  bad: styles.bad,
}

export function StatusBadge({ status }: { status: HostsStatus }) {
  return (
    <span className={`${styles.badge} ${CLASS[TONE[status]]}`}>
      <i className={styles.dot} aria-hidden="true" />
      {t(TEXT_KEY[status])}
    </span>
  )
}
