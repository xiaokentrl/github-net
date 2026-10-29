import { useCallback, useEffect, useState } from 'react'
import type { Info as UpdateInfo } from '../../../bindings/github.com/xiaokentrl/github-net/internal/updater/models'
import type { AppStatus } from '../../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { checkForUpdates } from '../../api/client'
import { getErrorMessage } from '../../api/errors'
import { subscribeUpdateStatus } from '../../api/events'
import { t } from '../../i18n'
import styles from './UpdatesPane.module.css'

type Load = 'idle' | 'checking' | 'ready' | 'error'

// 发布页链接只信 GitHub 的域名：链接来自 GitHub API 的 html_url 字段，
// 域名不对就不渲染成链接，只给文字，不把后端给的东西不加校验地变成可点入口。
function isTrustedReleaseURL(url: string): boolean {
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' && (parsed.hostname === 'github.com' || parsed.hostname === 'www.github.com')
  } catch {
    return false
  }
}

// 「检测升级」屏：手动检查 + 启动自动检查的结论都落在这里显示。
// 只比较和提示；下载与安装属安全更新流程，这里不出现任何下载入口。
export function UpdatesPane({ status }: { status: AppStatus | null }) {
  const [load, setLoad] = useState<Load>('idle')
  const [info, setInfo] = useState<UpdateInfo | null>(null)
  const [errorText, setErrorText] = useState('')

  const check = useCallback(() => {
    setLoad('checking')
    setErrorText('')
    checkForUpdates()
      .then((result) => {
        setInfo(result)
        setLoad('ready')
      })
      .catch((err: unknown) => {
        setErrorText(getErrorMessage(err))
        setLoad('error')
      })
  }, [])

  // 启动自动检查的结论经 update:status 事件进来，和手动检查显示在同一块。
  useEffect(() => {
    const unsubscribe = subscribeUpdateStatus((payload) => {
      if (payload.info) {
        setInfo(payload.info)
        setLoad('ready')
      } else if (payload.error) {
        setErrorText(payload.error)
        setLoad('error')
      }
    })
    return unsubscribe
  }, [])

  // 进这屏就检查一次：用户不用先找按钮。
  useEffect(() => {
    check()
  }, [check])

  return (
    <div className={styles.pane}>
      <p className={styles.current}>
        {t('settings.updates.current', { version: status?.version ?? '' })}
      </p>

      <div className={styles.actions}>
        <button type="button" onClick={check} disabled={load === 'checking'}>
          {load === 'checking' ? t('settings.updates.checking') : t('settings.updates.check')}
        </button>
        {info?.checkedAt ? <span className={styles.checkedAt}>{t('settings.updates.checkedAt', { time: formatCheckedAt(info.checkedAt) })}</span> : null}
      </div>

      {load === 'ready' && info && (
        info.updateAvailable ? (
          <div className={styles.available} role="status">
            <p className={styles.headline}>{t('settings.updates.available', { latest: info.latestVersion ?? '' })}</p>
            {info.releaseUrl && isTrustedReleaseURL(info.releaseUrl) ? (
              <a className={styles.link} href={info.releaseUrl} target="_blank" rel="noreferrer">
                {t('settings.updates.openRelease')}
              </a>
            ) : info.releaseUrl ? (
              <p className={styles.plain}>{t('settings.updates.releaseAt', { url: info.releaseUrl })}</p>
            ) : null}
            <p className={styles.plain}>{t('settings.updates.noAutoDownload')}</p>
          </div>
        ) : (
          <p className={styles.headline} role="status">
            {t('settings.updates.upToDate', { latest: info.latestVersion ?? '' })}
          </p>
        )
      )}

      {load === 'error' && (
        <p className={styles.error} role="alert">
          {t('settings.updates.failed', { reason: errorText })}
        </p>
      )}
    </div>
  )
}

// checkedAt 是 RFC3339 的 UTC 时间；界面只显示到分钟，交给 Intl 格式化。
function formatCheckedAt(value: string): string {
  const at = new Date(value)
  if (Number.isNaN(at.getTime())) return value
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'short', timeStyle: 'short' }).format(at)
}
