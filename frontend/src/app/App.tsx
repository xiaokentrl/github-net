import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { HostsStatus } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { AppStatus, CatalogCategory, CatalogItem, ItemsSnapshot } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { getAppStatus, getItems, getSettings, saveSettings, type NetworkProfile } from '../api/client'
import { subscribeRuntimeStatus } from '../api/events'
import { getErrorMessage, toAppError } from '../api/errors'
import { AppShell } from '../components/layout/AppShell'
import { StatusBar } from '../components/layout/StatusBar'
import { Toolbar } from '../components/layout/Toolbar'
import { CategoryTree } from '../components/navigation/CategoryTree'
import type { StatusLookup } from '../components/navigation/CategoryNode'
import { SearchBox } from '../components/navigation/SearchBox'
import { profileLabel } from '../features/settings/profileOptions'
import { ItemDetail } from '../features/items/ItemDetail'
import { useDebouncedValue } from '../hooks/useDebouncedValue'
import { setLocale, t } from '../i18n'
import type { SaveKind } from '../components/ui/InlineStatus'
import type { Config } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'

type Load = 'loading' | 'ready' | 'error'

type SaveState = { kind: SaveKind; text: string }

const IDLE_SAVE: SaveState = { kind: 'idle', text: '' }

// 目前只搬了晨光白一套配色；其它主题在后端设置里存着，界面先按 light 显示并说明原因，
// 不假装已经切换成功（另外五套主题等 ThemePicker 那一屏一起搬）。
const PORTED_THEMES = new Set(['light'])

function applyTheme(theme: string): string {
  if (PORTED_THEMES.has(theme)) {
    document.documentElement.dataset.theme = theme
    return ''
  }
  document.documentElement.dataset.theme = 'light'
  return t('theme.pending', { name: theme })
}

export function App() {
  const [status, setStatus] = useState<AppStatus | null>(null)
  const [statusLoad, setStatusLoad] = useState<Load>('loading')
  const [statusError, setStatusError] = useState('')

  const [settings, setSettings] = useState<Config | null>(null)
  const [settingsLoad, setSettingsLoad] = useState<Load>('loading')
  const [settingsError, setSettingsError] = useState('')

  const [snapshot, setSnapshot] = useState<ItemsSnapshot | null>(null)
  const [catalogLoad, setCatalogLoad] = useState<Load>('loading')
  const [catalogError, setCatalogError] = useState('')

  const [query, setQuery] = useState('')
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const [selectedId, setSelectedId] = useState('')
  const [save, setSave] = useState<SaveState>(IDLE_SAVE)
  const [themeNotice, setThemeNotice] = useState('')

  const [sequence, setSequence] = useState(0)
  const [dropped, setDropped] = useState(0)

  // 收到的最新序号存在 ref 里，订阅回调才不会读到旧值。
  const sequenceRef = useRef(0)
  // 只认最后一次请求的结果，重试和 StrictMode 重复挂载都不会串台。
  const statusRequestRef = useRef(0)
  const settingsRequestRef = useRef(0)
  const catalogRequestRef = useRef(0)
  const saveRequestRef = useRef(0)

  const loadStatus = useCallback(() => {
    const request = statusRequestRef.current + 1
    statusRequestRef.current = request
    setStatusLoad('loading')
    getAppStatus()
      .then((result) => {
        if (statusRequestRef.current !== request) return
        setStatus(result)
        setStatusLoad('ready')
      })
      .catch((err: unknown) => {
        if (statusRequestRef.current !== request) return
        setStatusError(getErrorMessage(err))
        setStatusLoad('error')
      })
  }, [])

  // 设置是语言、档位和布局的唯一来源；读不到时界面回落到「未就绪」，不用假值渲染。
  const loadSettings = useCallback(() => {
    const request = settingsRequestRef.current + 1
    settingsRequestRef.current = request
    setSettingsLoad('loading')
    getSettings()
      .then((result) => {
        if (settingsRequestRef.current !== request) return
        setLocale(result.appearance.language)
        setThemeNotice(applyTheme(result.appearance.theme))
        setSettings(result)
        setSettingsLoad('ready')
      })
      .catch((err: unknown) => {
        if (settingsRequestRef.current !== request) return
        setSettings(null)
        setSettingsError(getErrorMessage(err))
        setSettingsLoad('error')
      })
  }, [])

  const loadCatalog = useCallback(() => {
    const request = catalogRequestRef.current + 1
    catalogRequestRef.current = request
    setCatalogLoad('loading')
    getItems()
      .then((result) => {
        if (catalogRequestRef.current !== request) return
        setSnapshot(result)
        setCatalogLoad('ready')
      })
      .catch((err: unknown) => {
        if (catalogRequestRef.current !== request) return
        setSnapshot(null)
        setCatalogError(getErrorMessage(err))
        setCatalogLoad('error')
      })
  }, [])

  useEffect(() => {
    loadStatus()
    loadSettings()
    loadCatalog()

    const unsubscribe = subscribeRuntimeStatus((payload) => {
      if (payload.sequence <= sequenceRef.current) {
        setDropped((count) => count + 1)
        return
      }
      sequenceRef.current = payload.sequence
      setSequence(payload.sequence)
    })

    return () => {
      // 让仍在路上的响应作废，并退订事件；退订必须调用，否则回调会一直堆积。
      statusRequestRef.current += 1
      settingsRequestRef.current += 1
      catalogRequestRef.current += 1
      saveRequestRef.current += 1
      unsubscribe()
    }
  }, [loadStatus, loadSettings, loadCatalog])

  const statusOf: StatusLookup = useMemo(() => {
    const map = new Map<string, HostsStatus>()
    for (const state of snapshot?.hosts ?? []) map.set(state.itemId, state.status)
    return map
  }, [snapshot])

  const counts = useMemo(() => {
    const categories = snapshot?.categories ?? []
    const hostnames = new Set<string>()
    let items = 0
    for (const category of categories) {
      items += category.items.length
      for (const item of category.items) {
        for (const hostname of item.hostnames) hostnames.add(hostname)
      }
    }
    return { categories: categories.length, items, hostnames: hostnames.size }
  }, [snapshot])

  const selected = useMemo(() => {
    const categories = snapshot?.categories ?? []
    for (const category of categories) {
      for (const item of category.items) {
        if (item.id === selectedId) return { item, category }
      }
    }
    return { item: null as CatalogItem | null, category: null as CatalogCategory | null }
  }, [snapshot, selectedId])

  const toggleCategory = useCallback((categoryID: string) => {
    setExpanded((current) => {
      const next = new Set(current)
      if (next.has(categoryID)) next.delete(categoryID)
      else next.add(categoryID)
      return next
    })
  }, [])

  const saveProfile = useCallback(
    (profile: NetworkProfile) => {
      if (!settings || settings.network.profile === profile) return
      const baseRevision = settings.revision
      const request = saveRequestRef.current + 1
      saveRequestRef.current = request
      setSave({ kind: 'pending', text: t('toolbar.save.pending') })
      // 每次点击是一个独立改动，所以幂等键每次新建；界面不自动重发同一个请求。
      saveSettings({ networkProfile: profile }, baseRevision, `${profile}-${Date.now()}`)
        .then((next) => {
          if (saveRequestRef.current !== request) return
          setSettings(next)
          setSave({ kind: 'ok', text: t('toolbar.save.ok', { revision: next.revision }) })
        })
        .catch((err: unknown) => {
          if (saveRequestRef.current !== request) return
          const failure = toAppError(err)
          setSave({
            kind: 'failed',
            text: t('toolbar.save.failed', { name: profileLabel(settings.network.profile), reason: failure.message }),
          })
          // 失败很可能是版本冲突，重新读一次后端的当前值，界面不停在旧副本上。
          loadSettings()
        })
    },
    [settings, loadSettings],
  )

  const debouncedQuery = useDebouncedValue(query, 150)

  const sidebar = (
    <>
      {catalogLoad === 'ready' && <SearchBox value={query} onChange={setQuery} />}
      <CategoryTree
        categories={snapshot?.categories ?? []}
        statusOf={statusOf}
        query={debouncedQuery}
        expanded={expanded}
        selectedId={selectedId}
        load={catalogLoad}
        error={catalogError}
        onRetry={loadCatalog}
        onToggle={toggleCategory}
        onSelect={setSelectedId}
      />
    </>
  )

  return (
    <AppShell
      appName={status?.app ?? ''}
      status={status}
      statusLoad={statusLoad}
      statusError={statusError}
      onRetryStatus={loadStatus}
      sidebarWidth={settings?.layout.sidebarWidth ?? null}
      toolbar={
        <Toolbar
          settings={settings}
          error={settingsLoad === 'error' ? settingsError : ''}
          saveKind={save.kind}
          saveText={save.text}
          busy={save.kind === 'pending'}
          onProfileChange={saveProfile}
        />
      }
      sidebar={sidebar}
      detail={
        <ItemDetail item={selected.item} category={selected.category} status={statusOf.get(selectedId)} />
      }
      statusbar={
        <StatusBar
          status={status}
          settings={settings}
          counts={counts}
          sequence={sequence}
          dropped={dropped}
          themeNotice={themeNotice}
        />
      }
    />
  )
}

export default App
