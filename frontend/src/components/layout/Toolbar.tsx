import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { t } from '../../i18n'
import { InlineStatus, type SaveKind } from '../../components/ui/InlineStatus'
import uiStyles from '../ui/ui.module.css'
import styles from './AppShell.module.css'

// 工具栏照原型第 803–817 行的顺序与分组：
// 左边 模式 / 打开Hosts / 选择清理项，右边 检测选中项 / 写入 Hosts / 还原 Hosts。
// 搜索框已挪进侧栏顶部、跟着菜单宽度走（拍板 2026-10-31），工具栏里不再有它。
// 档位快捷按钮已移除：档位和每轮候选上限改在设置中心的网络与流量页里看、在那里改。
// 全选/反选/折叠已挪进侧栏顶部（SidebarTools），原型同步。
export function Toolbar({
  settings,
  error,
  saveKind,
  saveText,
  busy,
  clearBusy,
  clearEnabled,
  queueBusy,
  onDetectChecked,
  onClearCheckedCache,
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
  clearBusy: boolean
  clearEnabled: boolean
  queueBusy: boolean
  onDetectChecked: () => void
  onClearCheckedCache: () => void
  onApplyChecked: () => void
  onOpenHosts: () => void
  restoreBusy: boolean
  onRestoreAll: () => void
}) {
  if (error) return <span className={styles.text}>{t('settings.error', { reason: error })}</span>
  if (!settings) return <span className={styles.text}>{t('detail.loading')}</span>

  // 四颗动作按钮共用一个「现在能不能点」：队列里还有没落定的任务，
  // 或保存设置／清理缓存／全部还原这些不进队列的命令正在等回执，都算「正在执行功能」。
  // 一处判断四颗一起关，不各写各的，免得漏掉某一颗。
  const actionsBusy = busy || clearBusy || queueBusy || restoreBusy

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

      {/* 清理缓存：点开框里分两件事——清勾中项目的源缓存，或清掉全部检测出来的 IP 缓存。
          第二件不需要勾选项；没勾选时框里第一颗自会禁用，有任务在跑时这四颗一起禁用。 */}
      <button
        type="button"
        className={styles.action}
        disabled={actionsBusy}
        title={t(clearEnabled ? 'toolbar.clearCache.title' : 'toolbar.clearCache.needCheck')}
        onClick={onClearCheckedCache}
      >
        {t('toolbar.clearCache')}
      </button>

      <span className={styles.spacer} />

      <button
        type="button"
        className={styles.action}
        disabled={actionsBusy}
        title={t('toolbar.detectChecked.title')}
        onClick={onDetectChecked}
      >
        {t('toolbar.detectChecked')}
      </button>

      {/* .action 已经 composes 过公共按钮配方（自带 --btn），这里只再挂主色/危险色即可。 */}
      <button
        type="button"
        className={`${styles.action} ${uiStyles.primary}`}
        disabled={actionsBusy}
        title={t('toolbar.applyChecked.title')}
        onClick={onApplyChecked}
      >
        {t('toolbar.applyChecked')}
      </button>
      <button
        type="button"
        className={`${styles.action} ${uiStyles.danger}`}
        disabled={actionsBusy}
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
