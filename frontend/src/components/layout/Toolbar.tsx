import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { NetworkProfile } from '../../api/client'
import { SearchBox } from '../navigation/SearchBox'
import { PROFILE_IDS, profileLabel, profileNote } from '../../features/settings/profileOptions'
import { t } from '../../i18n'
import { InlineStatus, type SaveKind } from '../../components/ui/InlineStatus'
import { Select } from '../../components/ui/Select'
import styles from './AppShell.module.css'

// 工具栏照原型第 802–819 行的顺序与分组：
// 左边 全选 / 反选 / 折叠 | 搜索 / 模式 / 打开Hosts，右边 检测全部 / 档位 / 应用队列 / 恢复。
// 多出来的是本仓自己的两块：档位那组（原型只有一颗快捷按钮）和设置版本号/档位说明，
// 都塞在档位这一格里，不新增视觉。
export function Toolbar({
  settings,
  error,
  saveKind,
  saveText,
  busy,
  allChecked,
  query,
  onQueryChange,
  onProfileChange,
  onOpenNetwork,
  onCheckAll,
  onInvertChecked,
  onFoldAll,
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
  allChecked: boolean
  query: string
  onQueryChange: (value: string) => void
  onProfileChange: (profile: NetworkProfile) => void
  onOpenNetwork: () => void
  onCheckAll: (checked: boolean) => void
  onInvertChecked: () => void
  onFoldAll: () => void
  onDetectChecked: () => void
  onApplyChecked: () => void
  onOpenHosts: () => void
  restoreBusy: boolean
  onRestoreAll: () => void
}) {
  if (error) return <span className={styles.text}>{t('settings.error', { reason: error })}</span>
  if (!settings) return <span className={styles.text}>{t('detail.loading')}</span>

  const profile = settings.network.profile

  return (
    <>
      <button
        type="button"
        className={styles.action}
        disabled={busy}
        title={t('toolbar.checkAllTitle')}
        onClick={() => onCheckAll(!allChecked)}
      >
        {allChecked ? t('toolbar.uncheckAll') : t('toolbar.checkAll')}
      </button>
      <button type="button" className={styles.action} disabled={busy} onClick={onInvertChecked}>
        {t('toolbar.invertChecked')}
      </button>
      <button type="button" className={styles.action} disabled={busy} title={t('toolbar.foldTitle')} onClick={onFoldAll}>
        {t('toolbar.fold')}
      </button>
      <span className={styles.divider} aria-hidden="true" />

      <SearchBox value={query} onChange={onQueryChange} />

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

      <Select
        label={t('toolbar.profile.label')}
        value={profile}
        disabled={busy}
        options={PROFILE_IDS.map((id) => ({ value: id, label: profileLabel(id) }))}
        onChange={(value) => onProfileChange(value as NetworkProfile)}
      />
      <span className={styles.text}>{t('toolbar.profile.summary', { cap: settings.network.maxCandidateProbesPerRun })}</span>
      <button type="button" className={styles.jump} title={t('toolbar.network.jumpHint')} onClick={onOpenNetwork}>
        {t('toolbar.network.jump')}
      </button>

      <button type="button" className={`${styles.action} ${styles.primary}`} disabled={busy} onClick={onApplyChecked}>
        {t('toolbar.applyChecked')}
      </button>
      <button
        type="button"
        className={`${styles.action} ${styles.danger}`}
        disabled={restoreBusy}
        title={t('toolbar.restoreTitle')}
        onClick={onRestoreAll}
      >
        {t('toolbar.restore')}
      </button>

      <InlineStatus kind={saveKind} text={saveText} />
      <span className={styles.text}>{t('toolbar.settings.revision', { revision: settings.revision })}</span>
      <p className={styles.note}>{profileNote(profile)}</p>
    </>
  )
}
