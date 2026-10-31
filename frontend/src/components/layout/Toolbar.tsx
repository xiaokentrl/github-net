import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { profileLabel } from '../../features/settings/profileOptions'
import { t } from '../../i18n'
import { InlineStatus, type SaveKind } from '../../components/ui/InlineStatus'
import uiStyles from '../ui/ui.module.css'
import styles from './AppShell.module.css'

// 工具栏照原型第 803–817 行的顺序与分组：
// 左边 模式 / 打开Hosts，右边 检测全部 / 档位 / 应用队列 / 恢复。
// 搜索框已挪进侧栏顶部、跟着菜单宽度走（拍板 2026-10-31），工具栏里不再有它。
// 档位那一格是原型同一颗快捷按钮（第 813 行，文字「省流 · ≤108 候选」）：显示当前档位和每轮候选上限，
// 点开进设置中心的网络与流量页去改。这里不再放下拉框、上限说明、跳转按钮和版本号——
// 那些内容把工具栏撑成了两行，而档位与上限在设置页里本来就能改。
// 全选/反选/折叠已挪进侧栏顶部（SidebarTools），原型同步。
export function Toolbar({
  settings,
  error,
  saveKind,
  saveText,
  busy,
  onOpenNetwork,
  onDetectChecked,
  onApplyChecked,
  onOpenHosts,
  restoreBusy,
  onRestoreAll,
}: {
  settings: Config | null
  error: string
  saveKind: SaveKind
  saveText: string
  busy: boolean
  onOpenNetwork: () => void
  onDetectChecked: () => void
  onApplyChecked: () => void
  onOpenHosts: () => void
  restoreBusy: boolean
  onRestoreAll: () => void
}) {
  if (error) return <span className={styles.text}>{t('settings.error', { reason: error })}</span>
  if (!settings) return <span className={styles.text}>{t('detail.loading')}</span>

  return (
    <>
      {/* 模式：本工具只写系统 Hosts；另两项照原型置灰，点了也不会变。 */}
      <select className={styles.mode} value="hosts" aria-label={t('toolbar.mode.label')} onChange={() => undefined}>
        <option value="hosts">{t('toolbar.mode.hosts')}</option>
        <option value="proxy" disabled>
          {t('toolbar.mode.proxy')}
        </option>
        <option value="pac" disabled>
          {t('toolbar.mode.pac')}
        </option>
      </select>

      <button type="button" className={styles.action} disabled={busy} title={t('toolbar.openHostsTitle')} onClick={onOpenHosts}>
        {t('toolbar.openHosts')}
      </button>

      <span className={styles.spacer} />

      <button type="button" className={styles.action} disabled={busy} onClick={onDetectChecked}>
        {t('toolbar.detectChecked')}
      </button>

      {/* 档位与每轮候选上限合成一颗按钮：数字都来自当前配置，点一下去设置页改。 */}
      <button
        type="button"
        className={styles.profileQuick}
        title={t('toolbar.profile.quickHint')}
        onClick={onOpenNetwork}
      >
        {t('toolbar.profile.quick', {
          profile: profileLabel(settings.network.profile),
          cap: settings.network.maxCandidateProbesPerRun,
        })}
      </button>

      {/* .action 已经 composes 过公共按钮配方（自带 --btn），档位直接挂 ui 的那两份即可。 */}
      <button type="button" className={`${styles.action} ${uiStyles.primary}`} disabled={busy} onClick={onApplyChecked}>
        {t('toolbar.applyChecked')}
      </button>
      <button
        type="button"
        className={`${styles.action} ${uiStyles.danger}`}
        disabled={restoreBusy}
        title={t('toolbar.restoreTitle')}
        onClick={onRestoreAll}
      >
        {t('toolbar.restore')}
      </button>

      {/* 保存结果在设置页里也有一份；这里留一格，窗口窄了先把它截断，不把工具栏挤成两行。 */}
      <span className={styles.saveStatus}>
        <InlineStatus kind={saveKind} text={saveText} />
      </span>
    </>
  )
}
