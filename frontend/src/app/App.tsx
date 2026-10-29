import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { AppStatus, CatalogCategory, CatalogItem, HostsState, ItemsSnapshot } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { applyItems, detectItems, getAppStatus, getItems, getSettings, openHosts, restoreItems, type NetworkProfile } from '../api/client'
import { subscribeRuntimeStatus } from '../api/events'
import { getErrorMessage } from '../api/errors'
import { AppShell } from '../components/layout/AppShell'
import { SettingsEntry } from '../components/layout/SettingsEntry'
import { StatusBar } from '../components/layout/StatusBar'
import { Toolbar } from '../components/layout/Toolbar'
import { CategoryTree } from '../components/navigation/CategoryTree'
import type { HostsLookup } from '../components/navigation/CategoryNode'
import { SearchBox } from '../components/navigation/SearchBox'
import { SettingsDialog, type SettingsPaneID } from '../features/settings/SettingsDialog'
import { ItemDetail } from '../features/items/ItemDetail'
import { TaskPanel } from '../features/tasks/TaskPanel'
import { subscribeTaskEvents } from '../features/tasks/taskEvents'
import { useDebouncedValue } from '../hooks/useDebouncedValue'
import { useSettingsSave } from '../hooks/useSettingsSave'
import { setLocale, t } from '../i18n'
import type { Config } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'

type Load = 'loading' | 'ready' | 'error'

// 六套配色的令牌都在 styles/themes.css 里，所以后端给哪个主题就挂哪个：
// 挂的是 html 的 data-theme，页面组件不写自己的主题色（原型第 2 行与 531–585 行同一机制）。
function applyTheme(theme: string) {
  document.documentElement.dataset.theme = theme
}

// 缩放抄原型第 1631 行：给 body 设 CSS zoom，只作用在本机界面上，不写进任何文件。
function applyZoom(zoom: number) {
  document.body.style.zoom = String(zoom)
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

  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogPane, setDialogPane] = useState<SettingsPaneID>('layout')

  const [sequence, setSequence] = useState(0)
  const [dropped, setDropped] = useState(0)

  // 检测/应用/还原入口被后端拒绝时的一句话，显示在详情页对应按钮旁边；下一次成功就清掉。
  const [detectError, setDetectError] = useState('')
  const [hostsError, setHostsError] = useState('')

  // 收到的最新序号存在 ref 里，订阅回调才不会读到旧值。
  const sequenceRef = useRef(0)
  // 只认最后一次请求的结果，重试和 StrictMode 重复挂载都不会串台。
  const statusRequestRef = useRef(0)
  const settingsRequestRef = useRef(0)
  const catalogRequestRef = useRef(0)

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

  // 一份配置快照落到界面上要做的事只有这些：语言、主题、缩放、数据本身。
  // 读回来的和保存成功返回的都走这里，省得两条链路对同一份配置处理得不一样。
  const applyConfig = useCallback((next: Config) => {
    setLocale(next.appearance.language)
    applyTheme(next.appearance.theme)
    applyZoom(next.appearance.zoom)
    setSettings(next)
    setSettingsLoad('ready')
  }, [])

  // 设置是语言、档位和布局的唯一来源；读不到时界面回落到「未就绪」，不用假值渲染。
  const loadSettings = useCallback(() => {
    const request = settingsRequestRef.current + 1
    settingsRequestRef.current = request
    setSettingsLoad('loading')
    getSettings()
      .then((result) => {
        if (settingsRequestRef.current !== request) return
        applyConfig(result)
      })
      .catch((err: unknown) => {
        if (settingsRequestRef.current !== request) return
        setSettings(null)
        setSettingsError(getErrorMessage(err))
        setSettingsLoad('error')
      })
  }, [applyConfig])

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

    // 任务与候选进度：应用级只挂一份订阅，先订阅再拉快照（taskEvents.ts）。
    // 应用/还原类任务落定后重读目录，详情页的 Hosts 状态才会跟着变；检测类不用（结果存储自己追）。
    const unsubscribeTasks = subscribeTaskEvents((settled) => {
      if (settled.kind === 'apply' || settled.kind === 'restore') loadCatalog()
    })

    return () => {
      // 让仍在路上的响应作废，并退订事件；退订必须调用，否则回调会一直堆积。
      statusRequestRef.current += 1
      settingsRequestRef.current += 1
      catalogRequestRef.current += 1
      unsubscribe()
      unsubscribeTasks()
    }
  }, [loadStatus, loadSettings, loadCatalog])

  // 后端每次 GetItems 都现读一遍 Hosts，这里只留最新一份：itemId → 那次读出来的整条状态。
  const hostsOf: HostsLookup = useMemo(() => {
    const map = new Map<string, HostsState>()
    for (const state of snapshot?.hosts ?? []) map.set(state.itemId, state)
    return map
  }, [snapshot])

  const counts = useMemo(() => {
    const categories = snapshot?.categories ?? []
    const hostnames = new Set<string>()
    let items = 0
    for (const category of categories) {
      const categoryItems = category.items ?? []
      items += categoryItems.length
      for (const item of categoryItems) {
        for (const hostname of item.hostnames ?? []) hostnames.add(hostname)
      }
    }
    return { categories: categories.length, items, hostnames: hostnames.size }
  }, [snapshot])

  const selected = useMemo(() => {
    const categories = snapshot?.categories ?? []
    for (const category of categories) {
      for (const item of category.items ?? []) {
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

  // 工具栏和设置对话框共用这一条保存链路：同一份 revision 只数一遍，不会出现两边各存各的。
  const { state: save, save: submit } = useSettingsSave(settings, applyConfig, loadSettings)

  const changeProfile = useCallback(
    (profile: NetworkProfile) => {
      if (!settings || settings.network.profile === profile) return
      submit({ networkProfile: profile }, t('settings.subject.profile'), 'profile')
    },
    [settings, submit],
  )

  const changeBudget = useCallback(
    (value: number) => {
      if (!settings || settings.network.maxCandidateProbesPerRun === value) return
      submit({ maxCandidateProbesPerRun: value }, t('settings.subject.budget'), 'budget')
    },
    [settings, submit],
  )

  const changeSidebarWidth = useCallback(
    (width: number) => {
      if (!settings || settings.layout.sidebarWidth === width) return
      submit({ sidebarWidth: width }, t('settings.subject.sidebar'), 'sidebar')
    },
    [settings, submit],
  )

  const changeZoom = useCallback(
    (factor: number) => {
      if (!settings || settings.appearance.zoom === factor) return
      submit({ zoom: factor }, t('settings.subject.zoom'), 'zoom')
    },
    [settings, submit],
  )

  const changeTheme = useCallback(
    (id: string) => {
      if (!settings || settings.appearance.theme === id) return
      submit({ theme: id }, t('settings.subject.theme'), 'theme')
    },
    [settings, submit],
  )

  const openSettings = useCallback((pane: SettingsPaneID) => {
    setDialogPane(pane)
    setDialogOpen(true)
  }, [])

  // 检测入口：后端整批校验，被拒的原因显示在检测按钮旁边，不弹 Toast。
  // 成功的反馈不在这一层——任务排进队列后，进度由 task:progress 事件推给队列面板和详情页。
  const startDetect = useCallback((itemId: string) => {
    setDetectError('')
    detectItems([itemId]).catch((err: unknown) => {
      setDetectError(getErrorMessage(err))
    })
  }, [])

  const startApply = useCallback((itemId: string) => {
    setHostsError('')
    applyItems([itemId]).catch((err: unknown) => {
      setHostsError(getErrorMessage(err))
    })
  }, [])

  const startRestore = useCallback((itemId: string) => {
    setHostsError('')
    restoreItems([itemId]).catch((err: unknown) => {
      setHostsError(getErrorMessage(err))
    })
  }, [])

  const openHostsFile = useCallback(() => {
    setHostsError('')
    openHosts().catch((err: unknown) => {
      setHostsError(getErrorMessage(err))
    })
  }, [])

  const debouncedQuery = useDebouncedValue(query, 150)

  const sidebar = (
    <>
      {catalogLoad === 'ready' && <SearchBox value={query} onChange={setQuery} />}
      <CategoryTree
        categories={snapshot?.categories ?? []}
        hostsOf={hostsOf}
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
    <>
      <AppShell
        appName={status?.app ?? ''}
        status={status}
        statusLoad={statusLoad}
        statusError={statusError}
        onRetryStatus={loadStatus}
        sidebarWidth={settings?.layout.sidebarWidth ?? null}
        onSidebarResize={changeSidebarWidth}
        toolbar={
          <Toolbar
            settings={settings}
            error={settingsLoad === 'error' ? settingsError : ''}
            saveKind={save.kind}
            saveText={save.text}
            busy={save.kind === 'pending'}
            onProfileChange={changeProfile}
            onOpenNetwork={() => openSettings('network')}
          />
        }
        sidebar={sidebar}
        sidebarFooter={<SettingsEntry onOpen={() => openSettings('layout')} />}
        detail={
          <ItemDetail
            item={selected.item}
            category={selected.category}
            hosts={hostsOf.get(selectedId)}
            detectReady={status?.dnsDetectionReady ?? false}
            detectError={detectError}
            hostsError={hostsError}
            onDetect={startDetect}
            onApply={startApply}
            onRestore={startRestore}
            onOpenHosts={openHostsFile}
          />
        }
        queue={<TaskPanel />}
        statusbar={
          <StatusBar
            status={status}
            settings={settings}
            counts={counts}
            sequence={sequence}
            dropped={dropped}
          />
        }
      />
      {dialogOpen && settings && (
        <SettingsDialog
          pane={dialogPane}
          settings={settings}
          hostnames={counts.hostnames}
          status={status}
          save={save}
          onPaneChange={setDialogPane}
          onClose={() => setDialogOpen(false)}
          onProfile={changeProfile}
          onBudget={changeBudget}
          onSidebarWidth={changeSidebarWidth}
          onZoom={changeZoom}
          onTheme={changeTheme}
        />
      )}
    </>
  )
}

export default App
