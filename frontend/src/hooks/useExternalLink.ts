// 「关于我们」里点外链的失败状态。单独成一个 hook，是因为失败必须让人看见：
// 点了「项目主页」却什么都没发生也不给提示，比不给按钮更糟。
import { useCallback, useState } from 'react'
import { openExternal } from '../api/browser'
import { getErrorMessage } from '../api/errors'
import { t } from '../i18n'

export type ExternalLink = {
  failed: string
  open: (url: string) => void
}

export function useExternalLink(): ExternalLink {
  const [failed, setFailed] = useState('')

  const open = useCallback((url: string) => {
    // 每次点先清掉上一条失败说明，否则上一次「没打开」会一直挂着像本次也没打开。
    setFailed('')
    openExternal(url).catch((error: unknown) => {
      setFailed(t('about.link.failed', { reason: getErrorMessage(error) }))
    })
  }, [])

  return { failed, open }
}
