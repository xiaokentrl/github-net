import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { NetworkProfile } from '../../api/client'
import { PROFILE_IDS, profileLabel, profileNote } from '../../features/settings/profileOptions'
import { t } from '../../i18n'
import { InlineStatus, type SaveKind } from '../../components/ui/InlineStatus'
import { Select } from '../../components/ui/Select'
import styles from './AppShell.module.css'

// 工具栏上的紧凑流量状态：档位可以在这里改，改动立刻走后端保存。
// 三档解释必须一直看得见（规划要求同时说明请求量、等待和覆盖面），所以这一栏允许换成两行。
// 那颗跳转按钮是「跳进网络与流量页」的入口：这里只放得下单个档位选择，
// 每轮上限、覆盖率算式这些都在设置页里，不在这儿摊开。
export function Toolbar({
  settings,
  error,
  saveKind,
  saveText,
  busy,
  onProfileChange,
  onOpenNetwork,
  onCheckAll,
  onInvertChecked,
  onDetectChecked,
  onApplyChecked,
}: {
  settings: Config | null
  error: string
  saveKind: SaveKind
  saveText: string
  busy: boolean
  onProfileChange: (profile: NetworkProfile) => void
  onOpenNetwork: () => void
  onCheckAll: (checked: boolean) => void
  onInvertChecked: () => void
  onDetectChecked: () => void
  onApplyChecked: () => void
}) {
  if (error) return <span className={styles.text}>{t('settings.error', { reason: error })}</span>
  if (!settings) return <span className={styles.text}>{t('detail.loading')}</span>

  const profile = settings.network.profile

  return (
    <>
      <button type="button" className={styles.action} disabled={busy} onClick={() => onCheckAll(true)}>
        {t('toolbar.checkAll')}
      </button>
      <button type="button" className={styles.action} disabled={busy} onClick={onInvertChecked}>
        {t('toolbar.invertChecked')}
      </button>
      <button type="button" className={styles.action} disabled={busy} onClick={onDetectChecked}>
        {t('toolbar.detectChecked')}
      </button>
      <button type="button" className={`${styles.action} ${styles.primary}`} disabled={busy} onClick={onApplyChecked}>
        {t('toolbar.applyChecked')}
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
      <InlineStatus kind={saveKind} text={saveText} />
      <span className={styles.spacer} />
      <span className={styles.text}>{t('toolbar.settings.revision', { revision: settings.revision })}</span>
      <p className={styles.note}>{profileNote(profile)}</p>
    </>
  )
}
