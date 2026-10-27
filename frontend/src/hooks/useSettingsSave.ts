// 设置改动的唯一出口：工具栏和设置对话框都走这里，所以不会出现两条各自数 revision 的保存链路。
// 规则沿用后端契约：带「手上这份的 revision」和一次性幂等键；失败很可能是版本冲突，
// 所以失败后重新读一次后端当前值，界面不停在旧副本上。
import { useCallback, useRef, useState } from 'react'
import type { Config } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import { saveSettings, type SettingsPatch } from '../api/client'
import { toAppError } from '../api/errors'
import { t } from '../i18n'
import type { SaveKind } from '../components/ui/InlineStatus'

export type SaveState = { kind: SaveKind; text: string }

export const IDLE_SAVE: SaveState = { kind: 'idle', text: '' }

export function useSettingsSave(settings: Config | null, apply: (next: Config) => void, reload: () => void) {
  const [state, setState] = useState<SaveState>(IDLE_SAVE)
  // 只认最后一次请求的结果，重试和 StrictMode 重复挂载都不会串台。
  const requestRef = useRef(0)

  const save = useCallback(
    (patch: SettingsPatch, label: string, seed: string) => {
      if (!settings) return
      const request = requestRef.current + 1
      requestRef.current = request
      setState({ kind: 'pending', text: t('settings.save.pending') })
      // 每次点击是一个独立改动，所以幂等键每次新建；界面不自动重发同一个请求。
      saveSettings(patch, settings.revision, `${seed}-${Date.now()}`)
        .then((next) => {
          if (requestRef.current !== request) return
          // 保存成功返回的就是最新快照，界面直接采用，省掉一次读。
          apply(next)
          setState({ kind: 'ok', text: t('settings.save.ok', { revision: next.revision, label }) })
        })
        .catch((err: unknown) => {
          if (requestRef.current !== request) return
          setState({ kind: 'failed', text: t('settings.save.failed', { label, reason: toAppError(err).message }) })
          reload()
        })
    },
    [settings, apply, reload],
  )

  return { state, save }
}
