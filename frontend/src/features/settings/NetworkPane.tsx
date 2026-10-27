import { useEffect, useRef, useState } from 'react'
import type { Config } from '../../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { NetworkProfile } from '../../api/client'
import { InlineStatus } from '../../components/ui/InlineStatus'
import { t } from '../../i18n'
import { PROFILE_IDS, profileLabel, profileLimitText, profileNote, profileSpec } from './profileOptions'
import type { SaveState } from '../../hooks/useSettingsSave'
import styles from './SettingsDialog.module.css'

// 和后端 internal/config 的 MinCandidateProbes / MaxCandidateProbesLimit 一致；
// 后端也会再校验一次，这里只负责在发送前把明显越界的输入挡住并说清原因。
const CANDIDATE_MIN = 1
const CANDIDATE_MAX = 108

// 网络与流量：档位三选一 + 每轮候选 IP 上限。每个控件改完立刻保存，
// 所以没有「保存」按钮；改动只落到配置里，不会在这里发起任何检测。
export function NetworkPane({
  settings,
  hostnames,
  save,
  onProfile,
  onBudget,
}: {
  settings: Config
  hostnames: number
  save: SaveState
  onProfile: (profile: NetworkProfile) => void
  onBudget: (value: number) => void
}) {
  const [draft, setDraft] = useState(String(settings.network.maxCandidateProbesPerRun))
  const [invalid, setInvalid] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const focusedRef = useRef(false)
  const busy = save.kind === 'pending'
  const spec = profileSpec(settings.network.profile)
  // 一轮全量需要的次数按目录里去重后的主机名数算，和后端「每 hostname 候选上限」同一口径。
  const needed = hostnames * spec.candidates
  const cap = settings.network.maxCandidateProbesPerRun

  // 输入框在焦点上时保留用户正在敲的值；焦点离开或后端返回新快照后，再跟回真实值。
  useEffect(() => {
    if (!focusedRef.current) setDraft(String(settings.network.maxCandidateProbesPerRun))
  }, [settings.network.maxCandidateProbesPerRun])

  const commit = () => {
    const value = Number(draft)
    if (!Number.isInteger(value) || value < CANDIDATE_MIN || value > CANDIDATE_MAX) {
      setInvalid(true)
      return
    }
    setInvalid(false)
    if (value === cap) return
    onBudget(value)
  }

  return (
    <>
      <h3 className={styles.paneTitle}>{t('settings.network.title')}</h3>
      <p className={styles.paneDesc}>{t('settings.network.paneDesc')}</p>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⌁
          </span>
          {t('settings.network.profileTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.network.profileDesc')}</p>
        <div className={styles.profileGrid}>
          {PROFILE_IDS.map((id) => {
            const selected = settings.network.profile === id
            return (
              <button
                key={id}
                type="button"
                className={styles.profileCard}
                aria-pressed={selected}
                disabled={busy}
                onClick={() => onProfile(id)}
              >
                <span className={styles.profileTitle}>{profileLabel(id)}</span>
                <span className={styles.profileDesc}>{profileNote(id)}</span>
                <span className={styles.profileLimit}>{profileLimitText(id)}</span>
                <span className={styles.profileCheck} aria-hidden="true">
                  {selected ? '✓' : ''}
                </span>
              </button>
            )
          })}
        </div>
      </div>

      <div className={styles.card}>
        <div className={styles.cardTitle}>
          <span className={styles.ico} aria-hidden="true">
            ⇣
          </span>
          {t('settings.network.budgetTitle')}
        </div>
        <p className={styles.cardDesc}>{t('settings.network.budgetDesc')}</p>
        <div className={styles.budgetRow}>
          <input
            ref={inputRef}
            type="number"
            min={CANDIDATE_MIN}
            max={CANDIDATE_MAX}
            step={1}
            inputMode="numeric"
            aria-label={t('settings.network.budgetTitle')}
            aria-invalid={invalid}
            disabled={busy}
            value={draft}
            onFocus={() => {
              focusedRef.current = true
            }}
            onChange={(event) => setDraft(event.target.value)}
            onBlur={() => {
              focusedRef.current = false
              commit()
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter') inputRef.current?.blur()
            }}
          />
          <span>{t('settings.network.budgetUnit')}</span>
          <InlineStatus
            kind={invalid ? 'failed' : save.kind}
            text={invalid ? t('settings.network.budgetRange', { min: CANDIDATE_MIN, max: CANDIDATE_MAX }) : save.text}
          />
        </div>
        <div className={styles.budgetPreview}>
          {t('settings.network.preview', { hostnames, candidates: spec.candidates, needed })}
          {needed <= cap
            ? t('settings.network.coverageFull', { cap })
            : t('settings.network.coverageShort', { cap, gap: needed - cap })}
        </div>
      </div>

      <div className={styles.note}>{t('settings.network.note')}</div>
    </>
  )
}
