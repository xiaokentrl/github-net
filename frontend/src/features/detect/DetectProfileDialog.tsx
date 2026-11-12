import { useEffect, useRef, useState } from 'react'
import { getLastDetectProfile, setLastDetectProfile, type NetworkProfile } from '../../api/client'
import { MethodDetectPicker } from '../strategies/MethodDetectPicker'
import { Dialog } from '../../components/ui/Dialog'
import { pushNotice } from '../../components/ui/toastStore'
import uiStyles from '../../components/ui/ui.module.css'
import { t } from '../../i18n'
import { PROFILE_IDS, profileLabel, profileLimitText, profileNote, type ProfileID } from '../settings/profileOptions'
import cardStyles from '../settings/SettingsDialog.module.css'
import styles from './DetectProfileDialog.module.css'

// 「记住选项」记住的是弹框上次选了哪一档，不是设置中心那个档位：
// 权威是后端单独一份小文件（重启、重装都还在），config.json 和设置中心一个字不改。
// 只有勾了「记住选项」并点了「检测」才记；取消勾选再检测就忘掉。
// 进程内再留一份缓存：弹框打开时先用它，不等读盘，也不会闪一下省流。
let cachedProfile: ProfileID | null = null
// 本次运行里用户已经改过选择：读盘回来不能把这个选择覆盖掉（写盘本身是异步的）。
let overriddenThisRun = false

function isProfileID(value: string): value is ProfileID {
  return (PROFILE_IDS as readonly string[]).includes(value)
}

// 批量检测前选「这一次」用哪一档。三张卡片和设置中心同一套文案、数字、样式，
// 选择不写回配置：选完只作用于点「检测」发起的那一轮。
export function DetectProfileDialog({
  onCancel,
  onConfirm,
  scopeId,
}: {
  onCancel: () => void
  /** 确认时回调：profile 是本次档位；strategyId 是弹框里选的方法（没动下拉＝未传＝走默认方法链） */
  onConfirm: (profile: NetworkProfile, strategyId?: string) => void
  /** 这批项目共同所属的范围：传了才显示「下拉选方法」；跨范围批次不传（各用各的保存默认） */
  scopeId?: string
}) {
  // 没记住过就默认省流；记住过就从那档开始，并把「记住选项」保持勾上。
  const [selected, setSelected] = useState<ProfileID>(cachedProfile ?? 'low')
  const [remember, setRemember] = useState(cachedProfile !== null)
  const touched = useRef(false)
  // 方法下拉：预选「使用已有储存」（无则退已保存默认）；动了就钉住该方法，只作用于这一轮。
  // methodReady：方法清单读完才放开确认——没读完就点会静默走默认方法链（审计缺口 3）。
  const [methodReady, setMethodReady] = useState(scopeId === undefined)
  const [strategyId, setStrategyId] = useState('')


  // 每次开弹框都对一次盘：重启／重装后这份记忆还在，不依赖进程内存。
  // 用户已经动过手就不覆盖；读失败、读到不认得的值都当没记住过，绝不挡检测。
  useEffect(() => {
    let alive = true
    getLastDetectProfile()
      .then((profile) => {
        const next = isProfileID(profile) ? profile : null
        if (!alive || touched.current || overriddenThisRun) return
        cachedProfile = next
        setSelected(next ?? 'low')
        setRemember(next !== null)
      })
      .catch(() => {
        // 读不到就沿用本次运行里那份缓存，没缓存就是省流。
      })
    return () => {
      alive = false
    }
  }, [])

  const confirm = () => {
    const next = remember ? selected : null
    cachedProfile = next
    overriddenThisRun = true
    // 写盘失败如实递一条（2026-11-13 按钮审计低危）：用户以为记住了，重启后发现没记
    // 会当成 bug。本次运行内缓存还在（只降级成「本次运行记住」），这一轮检测照常发起。
    void setLastDetectProfile(next ?? '').catch(() => {
      pushNotice('warning', t('detect.profile.rememberFailed'))
    })
    onConfirm(selected, strategyId === '' ? undefined : strategyId)
  }
  return (
    <Dialog
      title={t('detect.profile.ask.title')}
      onClose={onCancel}
      width={600}
      footer={
        <>
          <button type="button" className={uiStyles.btn} onClick={onCancel}>
            {t('detect.profile.ask.cancel')}
          </button>
          <button
            type="button"
            className={`${uiStyles.btn} ${uiStyles.primary}`}
            disabled={!methodReady}
            title={!methodReady ? t('detect.profile.methodLoading') : undefined}
            onClick={confirm}
          >
            {t('detect.profile.ask.action')}
          </button>
        </>
      }
    >
      <p className={styles.hint}>{t('detect.profile.ask.hint')}</p>
      {scopeId ? (
        <div className={styles.methodRow}>
          <p className={styles.methodNote}>{t('detail.detectBox.pickerNote')}</p>
          <MethodDetectPicker
            scopeId={scopeId}
            compact
            onPick={(id) => {
              setStrategyId(id)
              setMethodReady(true)
            }}
            onReady={(ready) => {
              // 读失败/清单为空：也放开确认（退默认方法链）——别把用户卡死在弹框里（审计缺口 3）。
              // 失败原因已由 picker 的 error 分支摆在框内。
              setMethodReady(true)
              if (!ready) setStrategyId('')
            }}
          />
        </div>
      ) : null}
      <div className={cardStyles.profileGrid}>
        {PROFILE_IDS.map((id) => {
          const active = selected === id
          return (
            <button
              key={id}
              type="button"
              className={cardStyles.profileCard}
              aria-pressed={active}
              onClick={() => {
                touched.current = true
                setSelected(id)
              }}
            >
              <span className={cardStyles.profileTitle}>{profileLabel(id)}</span>
              <span className={cardStyles.profileDesc}>{profileNote(id)}</span>
              <span className={cardStyles.profileLimit}>{profileLimitText(id)}</span>
              <span className={cardStyles.profileCheck} aria-hidden="true">
                {active ? '✓' : ''}
              </span>
            </button>
          )
        })}
      </div>
      <label className={styles.remember}>
        <input
          type="checkbox"
          checked={remember}
          onChange={(event) => {
            touched.current = true
            setRemember(event.target.checked)
          }}
        />
        <span className={styles.rememberBody}>
          <span className={styles.rememberTitle}>{t('detect.profile.ask.remember')}</span>
          <span className={styles.rememberNote}>{t('detect.profile.ask.rememberHint')}</span>
        </span>
      </label>
    </Dialog>
  )
}
