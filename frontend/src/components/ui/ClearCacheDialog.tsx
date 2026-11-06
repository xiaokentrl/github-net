import { useState } from 'react'
import { t } from '../../i18n'
import { Dialog } from './Dialog'
import uiStyles from './ui.module.css'
import styles from './ClearCacheDialog.module.css'

export type ClearCacheChoice = { sources: boolean; allIP: boolean; hosts: boolean }

// 清理弹框：三件会删数据／改系统 Hosts 的事做成上下排列的可多选卡片，可任意组合，默认全不勾。
// 第三项「清除已写入的 Hosts」是还原动作，队列里还有没跑完的任务时由外部禁用，理由就地写明。
// 组件随弹框条件挂载，关闭即卸载，所以「默认全不选」不需要外部复位。
export function ClearCacheDialog({
  sourceCount,
  hostsDisabled,
  onCancel,
  onConfirm,
}: {
  sourceCount: number
  hostsDisabled: boolean
  onCancel: () => void
  onConfirm: (choice: ClearCacheChoice) => void
}) {
  const [sources, setSources] = useState(false)
  const [allIP, setAllIP] = useState(false)
  const [hosts, setHosts] = useState(false)
  const noCheckedItems = sourceCount === 0
  const any = sources || allIP || (hosts && !hostsDisabled)

  return (
    <Dialog
      title={t('toolbar.clearCache.ask.title')}
      onClose={onCancel}
      footer={
        <>
          <button type="button" className={uiStyles.btn} onClick={onCancel}>
            {t('toolbar.clearCache.ask.cancel')}
          </button>
          <button
            type="button"
            className={`${uiStyles.btn} ${uiStyles.primary}`}
            disabled={!any}
            onClick={() => onConfirm({ sources, allIP, hosts })}
          >
            {t('toolbar.clearCache.ask.action')}
          </button>
        </>
      }
    >
      <p className={styles.hint}>{t('toolbar.clearCache.ask.hint')}</p>

      <label className={`${styles.option} ${sources ? styles.selected : ''} ${noCheckedItems ? styles.disabled : ''}`}>
        <input
          type="checkbox"
          checked={sources}
          disabled={noCheckedItems}
          onChange={(event) => setSources(event.target.checked)}
        />
        <span className={styles.optionBody}>
          <span className={styles.optionTitle}>{t('toolbar.clearCache.source')}</span>
          <span className={styles.optionHint}>{t('toolbar.clearCache.source.hint', { count: sourceCount })}</span>
        </span>
      </label>

      <label className={`${styles.option} ${allIP ? styles.selected : ''}`}>
        <input type="checkbox" checked={allIP} onChange={(event) => setAllIP(event.target.checked)} />
        <span className={styles.optionBody}>
          <span className={styles.optionTitle}>{t('toolbar.clearCache.allIP')}</span>
          <span className={styles.optionHint}>{t('toolbar.clearCache.allIP.hint')}</span>
        </span>
      </label>

      <label className={`${styles.option} ${hosts && !hostsDisabled ? styles.selected : ''} ${hostsDisabled ? styles.disabled : ''}`}>
        <input
          type="checkbox"
          checked={hosts && !hostsDisabled}
          disabled={hostsDisabled}
          onChange={(event) => setHosts(event.target.checked)}
        />
        <span className={styles.optionBody}>
          <span className={styles.optionTitle}>{t('toolbar.clearCache.hosts')}</span>
          <span className={styles.optionHint}>{t('toolbar.clearCache.hosts.hint')}</span>
        </span>
      </label>
      {hostsDisabled ? <p className={styles.needCheck}>{t('toolbar.clearCache.hosts.busy')}</p> : null}

      {noCheckedItems ? <p className={styles.needCheck}>{t('toolbar.clearCache.needCheck')}</p> : null}
    </Dialog>
  )
}
