import { HostsStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { t } from '../../i18n'
import styles from './StatusBadge.module.css'

// 存文案键而不是成品句子：语言是后端设置里的值，切语言时文案要跟着走。
// $zero 是 Go 零值，表示后端根本没给状态，和 unknown 一样按「没读过」显示。
// unknown 这一档后端在 Hosts 只读链路接上后已经不返回了，留着只为兜住「没给状态」。
const TEXT_KEY: Record<HostsStatus, string> = {
  [HostsStatus.$zero]: 'badge.unknown',
  [HostsStatus.HostsUnknown]: 'badge.unknown',
  [HostsStatus.HostsNone]: 'badge.none',
  [HostsStatus.HostsPartial]: 'badge.partial',
  [HostsStatus.HostsApplied]: 'badge.applied',
  [HostsStatus.HostsConflict]: 'badge.conflict',
  [HostsStatus.HostsReadFailed]: 'badge.readFailed',
}

// 「没读过」和「已读过、没替过」都用中性色：它们不是好坏结论，别长得像告警。
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

// reason 是后端在冲突和读不到时给的一句原因；树上只显示徽章，这句话靠 title 带出，
// 详情页会把同一句话完整显示出来（title 读屏读不到，所以不能只依赖它）。
export function StatusBadge({ status, reason }: { status: HostsStatus; reason?: string }) {
  return (
    <span className={`${styles.badge} ${CLASS[TONE[status]]}`} title={reason}>
      <i className={styles.dot} aria-hidden="true" />
      {t(TEXT_KEY[status])}
    </span>
  )
}
