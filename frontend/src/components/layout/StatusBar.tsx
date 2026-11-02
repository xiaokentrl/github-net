import type { AppStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { AutomationStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/event/models'
import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { profileLabel } from '../../features/settings/profileOptions'
import { formatInteger } from '../../i18n/formatters'
import { t } from '../../i18n'
import styles from './AppShell.module.css'

export type CatalogCounts = { categories: number; items: number; hostnames: number }

// 菜单 ID 换成用户看得懂的主菜单名；目录里找不到就退回显示 ID，不编名字。
function menuNames(ids: string[] | null | undefined, names: Record<string, string>): string {
  return (ids ?? []).map((id) => names[id] ?? id).join('、')
}

// 启动自动化那一行：后端只给机器码和计数，句子在这里按当前语言拼。
// 没跑过（stage 为空）时整行不出现——状态栏不摆没发生的事。
// 句子键一律写成 t('字面键')：internal/app/i18n_keys_parity_test.go 只认字面键，
// 键名拼错的话界面上会直接印出「缺文案:…」，那道门抓不到动态拼的就是漏网。
export function automationText(status: AutomationStatus, names: Record<string, string>): string {
  const detail = status.detail ? `（${status.detail}）` : ''
  const rejected = rejectedNote(status, names)
  switch (status.stage) {
    case 'detecting':
      return t('statusbar.automation.detecting', { count: status.detectCount }) + rejected
    case 'applying':
      return t('statusbar.automation.applying', { count: status.applyCount }) + rejected
    case 'closing':
      return t('statusbar.automation.closing', { count: status.appliedCount }) + rejected
    case 'skipped':
      return t('statusbar.automation.skipped')
    case 'done': {
      let base: string
      switch (status.reason) {
        case 'no_candidates':
          base = t('statusbar.automation.done.noCandidates')
          break
        case 'close_busy':
          base = t('statusbar.automation.done.closeBusy')
          break
        case 'close_no_result':
          base = t('statusbar.automation.done.noResult')
          break
        default:
          base = t('statusbar.automation.done', { applied: status.appliedCount, total: status.applyCount })
      }
      return base + rejected
    }
    case 'failed':
      return failedReason(status, names) + detail
    default:
      return t('statusbar.automation.unknown', { stage: status.stage })
  }
}

// 失败原因码各挑一句；后端以后加了新的原因码而语言包还没补，这里显示机器码，
// 不把它塞进某句现成的话里。
function failedReason(status: AutomationStatus, names: Record<string, string>): string {
  switch (status.reason) {
    case 'plan_failed':
      return t('statusbar.automation.failed.plan')
    case 'detect_rejected':
      return t('statusbar.automation.failed.detect', { menus: menuNames(status.rejectedMenus, names) })
    case 'wait_failed':
      return t('statusbar.automation.failed.wait')
    case 'apply_rejected':
      return t('statusbar.automation.failed.apply')
    default:
      return t('statusbar.automation.failed.other', { reason: status.reason ?? status.stage })
  }
}

// 部分主菜单在检测入口被拒、其余照常跑时，后面补一句哪些没排上。
// 不补这一句的话，勾了四个菜单只跑起三个，界面上看不出差别。
function rejectedNote(status: AutomationStatus, names: Record<string, string>): string {
  if (!status.rejectedMenus || status.rejectedMenus.length === 0) return ''
  return t('statusbar.automation.rejected', { menus: menuNames(status.rejectedMenus, names) })
}

// 底部状态栏只放后端真给出来的东西；没有来源的字段显示「未检测」，不编数字。
export function StatusBar({
  status,
  settings,
  counts,
  sequence,
  dropped,
  automation,
  menuNamesById,
}: {
  status: AppStatus | null
  settings: Config | null
  counts: CatalogCounts
  sequence: number
  dropped: number
  automation: AutomationStatus | null
  menuNamesById: Record<string, string>
}) {
  return (
    <>
      {settings && <span className={styles.item}>{t('statusbar.mode', { name: profileLabel(settings.network.profile) })}</span>}
      {status && (
        <>
          <span className={styles.item}>{t('statusbar.hostsPath', { path: status.hostsPath })}</span>
          <span className={styles.item}>
            {t('statusbar.dns', {
              resolver: status.dnsResolver,
              state: status.dnsDetectionReady ? t('statusbar.dns.ready') : t('statusbar.dns.waiting'),
            })}
          </span>
        </>
      )}
      {/* 检测任务还没接入，所以「上次检测」只能是未检测，不能显示时间。 */}
      <span className={styles.item}>{t('statusbar.lastDetect', { value: t('statusbar.lastDetect.none') })}</span>
      <span className={styles.item}>{t('statusbar.sequence', { sequence, dropped })}</span>
      {/* 启动后那条自动链跑到哪一步。只有真跑过才占这一格，没跑过时这里不出现。 */}
      {automation && <span className={styles.item}>{automationText(automation, menuNamesById)}</span>}
      <span className={styles.spacer} />
      {status?.configNotice && (
        <span className={styles.item} role="alert">
          {t('statusbar.notice', { notice: status.configNotice })}
        </span>
      )}
      <span className={styles.item}>
        {t('statusbar.counts', {
          categories: formatInteger(counts.categories),
          items: formatInteger(counts.items),
          hostnames: formatInteger(counts.hostnames),
        })}
      </span>
    </>
  )
}
