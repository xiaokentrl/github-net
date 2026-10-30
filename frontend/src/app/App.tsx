import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { AppStatus, CatalogCategory, CatalogItem, HostsState, ItemsSnapshot } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import { applyItems, detectBudget, detectItems, getAppStatus, getItems, getSettings, openHosts, restoreItems, type NetworkProfile } from '../api/client'
import { subscribeRuntimeStatus, subscribeUpdateStatus } from '../api/events'
import { getErrorMessage } from '../api/errors'
import { AppShell } from '../components/layout/AppShell'
import { SettingsEntry } from '../components/layout/SettingsEntry'
import { StatusBar } from '../components/layout/StatusBar'
import { Toolbar } from '../components/layout/Toolbar'
import { CategoryTree } from '../components/navigation/CategoryTree'
import type { HostsLookup } from '../components/navigation/CategoryNode'
import { liveByItem } from '../components/navigation/treeStatus'
import { SearchBox } from '../components/navigation/SearchBox'
import { SettingsDialog, type SettingsPaneID } from '../features/settings/SettingsDialog'
import { ItemDetail } from '../features/items/ItemDetail'
import { TaskPanel } from '../features/tasks/TaskPanel'
import { subscribeTaskEvents } from '../features/tasks/taskEvents'
import { useTaskQueue } from '../features/tasks/taskStore'
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

  // 勾选模型：批量检测/应用的参与范围，内存态（规划明确勾选不持久化）。
  // 首次拿到目录时默认勾选 GitHub 和 Steam（用户拍板的默认值）。
  const [checkedIds, setCheckedIds] = useState<ReadonlySet<string>>(new Set())
  const checkedInitialized = useRef(false)

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
  // 启动自动检查发现的最新版本；空串表示没有已知更新（没查过、没新版本或检查失败）。
  const [updateLatest, setUpdateLatest] = useState('')

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

    // 启动自动检查的结果落到顶栏徽标；详情看设置中心的检测升级屏。
    const unsubscribeUpdate = subscribeUpdateStatus((payload) => {
      if (payload.info?.updateAvailable && payload.info.latestVersion) {
        setUpdateLatest(payload.info.latestVersion)
      }
    })

    return () => {
      // 让仍在路上的响应作废，并退订事件；退订必须调用，否则回调会一直堆积。
      statusRequestRef.current += 1
      settingsRequestRef.current += 1
      catalogRequestRef.current += 1
      unsubscribe()
      unsubscribeTasks()
      unsubscribeUpdate()
    }
  }, [loadStatus, loadSettings, loadCatalog])

  // 后端每次 GetItems 都现读一遍 Hosts，这里只留最新一份：itemId → 那次读出来的整条状态。
  const hostsOf: HostsLookup = useMemo(() => {
    const map = new Map<string, HostsState>()
    for (const state of snapshot?.hosts ?? []) map.set(state.itemId, state)
    return map
  }, [snapshot])

  // 树上那一行的状态字和色点要跟着队列走：正在检测、正在写 Hosts、正在还原的项目，原型是整行变色。
  // 队列只有 taskStore 这一份，TaskPanel 和树读的是同一个快照，不存在两份真相。
  const queue = useTaskQueue()
  const liveOf = useMemo(() => liveByItem(queue.tasks), [queue.tasks])

  // 默认勾选：第一次拿到目录时勾上 GitHub 和 Steam（已拍板）；此后不再自动改。
  useEffect(() => {
    if (checkedInitialized.current || catalogLoad !== 'ready') return
    checkedInitialized.current = true
    if (checkedIds.size > 0) return
    const defaults = new Set<string>()
    for (const cat of snapshot?.categories ?? []) {
      if (cat.id === 'github' || cat.id === 'steam') for (const it of cat.items ?? []) defaults.add(it.id)
    }
    setCheckedIds(defaults)
  }, [catalogLoad, snapshot, checkedIds.size])

  const toggleChecked = useCallback((itemId: string, checked: boolean) => {
    setCheckedIds((current) => {
      const next = new Set(current)
      if (checked) next.add(itemId)
      else next.delete(itemId)
      return next
    })
  }, [])

  const checkAll = useCallback(
    (checked: boolean) => {
      const all = new Set<string>()
      if (checked) for (const cat of snapshot?.categories ?? []) for (const it of cat.items ?? []) all.add(it.id)
      setCheckedIds(all)
    },
    [snapshot],
  )

  const invertChecked = useCallback(() => {
    setCheckedIds((current) => {
      const next = new Set<string>()
      for (const cat of snapshot?.categories ?? []) {
        for (const it of cat.items ?? []) {
          if (!current.has(it.id)) next.add(it.id)
        }
      }
      return next
    })
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

  // 队列面板的高度：上方那条横向手柄和布局屏三档预设都走这一条，和后端一致时不写盘。
  const changeQueueHeight = useCallback(
    (height: number) => {
      if (!settings || settings.layout.queueHeight === height) return
      submit({ queueHeight: height }, t('settings.subject.queueHeight'), 'queueHeight')
    },
    [settings, submit],
  )

  // 队列里「任务列表」的列宽：拖动条和布局屏预设卡都走这一条，和后端一致时不写盘。
  const changeQueueListWidth = useCallback(
    (width: number) => {
      if (!settings || settings.layout.queueListWidth === width) return
      submit({ queueListWidth: width }, t('settings.subject.queueList'), 'queueList')
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

  const changeCloseToTray = useCallback(
    (enabled: boolean) => {
      if (!settings || settings.startup.closeToTray === enabled) return
      submit({ closeToTray: enabled }, t('settings.subject.closeToTray'), 'closeToTray')
    },
    [settings, submit],
  )

  const changeStartMinimized = useCallback(
    (enabled: boolean) => {
      if (!settings || settings.startup.startMinimized === enabled) return
      submit({ startMinimized: enabled }, t('settings.subject.startMinimized'), 'startMinimized')
    },
    [settings, submit],
  )

  const changeAutoStart = useCallback(
    (enabled: boolean) => {
      if (!settings || settings.startup.autoStart === enabled) return
      submit({ autoStart: enabled }, t('settings.subject.autoStart'), 'autoStart')
    },
    [settings, submit],
  )

  const openSettings = useCallback((pane: SettingsPaneID) => {
    setDialogPane(pane)
    setDialogOpen(true)
  }, [])

  // 检测入口：后端整批校验，被拒的原因显示在检测按钮旁边，不弹 Toast。
  // 成功的反馈不在这一层——任务排进队列后，进度由 task:progress 事件推给队列面板和详情页。
  // 批量检测：按一级菜单分组逐类目发起——后端每类目一轮（轮名=分类名，匹配原型）。
  const startDetectChecked = useCallback(
    (checked: ReadonlySet<string>) => {
      const groups = new Map<string, string[]>()
      for (const cat of snapshot?.categories ?? []) {
        for (const it of cat.items ?? []) {
          if (!checked.has(it.id)) continue
          const list = groups.get(cat.id) ?? []
          list.push(it.id)
          groups.set(cat.id, list)
        }
      }
      setDetectError('')
      for (const ids of groups.values()) {
        detectItems(ids).catch((err: unknown) => {
          setDetectError(getErrorMessage(err))
        })
      }
    },
    [snapshot],
  )

  // 批量应用：勾选项里已检测且有可用候选的，交给后端建应用轮。
  const startApplyChecked = useCallback(
    (checked: ReadonlySet<string>) => {
      setHostsError('')
      const ids = [...checked].filter((id) => {
        const view = queue.results[id]
        return (view?.hosts ?? []).some((h) => (h.candidates ?? []).some((c) => c.status === 'available'))
      })
      if (ids.length === 0) {
        setHostsError(t('queue.detail.none'))
        return
      }
      applyItems(ids).catch((err: unknown) => {
        setHostsError(getErrorMessage(err))
      })
    },
    [queue.results],
  )

  const startDetect = useCallback((itemId: string) => {
    setDetectError('')
    detectItems([itemId]).catch((err: unknown) => {
      setDetectError(getErrorMessage(err))
    })
  }, [])

  // 批量检测：项目清单已由详情页按 detectableItems 过滤，这里不再重算第二遍口径。
  // 先问后端要预算预估；预算盖不全时按规划先请用户确认，取消就不发。
  const startDetectAll = useCallback(
    (items: CatalogItem[]) => {
      const ids = items.map((it) => it.id)
      setDetectError('')
      detectBudget(ids)
        .then((budget) => {
          if (budget.shortfall > 0 && !window.confirm(t('detect.budget.confirm', { needed: budget.needed, budget: budget.budget, shortfall: budget.shortfall }))) {
            return
          }
          return detectItems(ids).catch((err: unknown) => {
            setDetectError(getErrorMessage(err))
          })
        })
        .catch((err: unknown) => {
          setDetectError(getErrorMessage(err))
        })
    },
    [],
  )

  const startApply = useCallback((itemId: string) => {
    setHostsError('')
    applyItems([itemId]).catch((err: unknown) => {
      setHostsError(getErrorMessage(err))
    })
  }, [])

  // 树侧焦点跟随：正在跑的子项自动展开所属类目并滚动到可见（原型 scrollTreeToItem 同款）。
  // 用户点过树 = 手动浏览，焦点再变时重新跟随（和队列面板的翻页语义一致）。
  const focusManualRef = useRef(false)
  const runningItemId = useMemo(() => {
    for (const task of queue.tasks) {
      if (task.status === 'running' || task.status === 'restoring') return task.itemId
    }
    return null
  }, [queue.tasks])
  useEffect(() => {
    if (focusManualRef.current) return
    if (!runningItemId) return
    const cat = (snapshot?.categories ?? []).find((c) => (c.items ?? []).some((it) => it.id === runningItemId))
    if (cat) setExpanded((current) => {
      if (!current.has(cat.id)) {
        const next = new Set(current)
        next.delete(cat.id)
        return next
      }
      return current
    })
    requestAnimationFrame(() => {
      document.querySelector(`[data-item-id="${runningItemId}"]`)?.scrollIntoView({ block: 'nearest' })
    })
  }, [runningItemId, snapshot])

  // 一条还原链路，详情页的「还原此项」和树上的 ↺（单项或整类）都走这里；
  // 落定后由上面的任务订阅重读目录，树上的状态字、色点和 ↺ 是否还在都会跟着变。
  const startRestoreItems = useCallback((itemIds: string[]) => {
    setHostsError('')
    restoreItems(itemIds).catch((err: unknown) => {
      setHostsError(getErrorMessage(err))
    })
  }, [])

  const startRestore = useCallback((itemId: string) => startRestoreItems([itemId]), [startRestoreItems])

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
        liveOf={liveOf}
        results={queue.results}
        query={debouncedQuery}
        expanded={expanded}
        selectedId={selectedId}
        checkedIds={checkedIds}
        onToggleChecked={toggleChecked}
        onManualBrowse={() => { focusManualRef.current = true }}
        load={catalogLoad}
        error={catalogError}
        onRetry={loadCatalog}
        onToggle={toggleCategory}
        onSelect={setSelectedId}
        onRestoreItems={startRestoreItems}
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
            onCheckAll={checkAll}
            onInvertChecked={invertChecked}
            onDetectChecked={() => startDetectChecked(checkedIds)}
            onApplyChecked={() => startApplyChecked(checkedIds)}
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
            onDetectAll={startDetectAll}
            onApply={startApply}
            onRestore={startRestore}
            onOpenHosts={openHostsFile}
            onStrategySaved={loadSettings}
          />
        }
        queue={
          <TaskPanel
            height={settings?.layout.queueHeight ?? null}
            onHeightChange={changeQueueHeight}
            listWidth={settings?.layout.queueListWidth ?? null}
            onListWidthChange={changeQueueListWidth}
            activeItemId={selectedId}
            onPickItem={(itemId) => setSelectedId(itemId)}
          />
        }
        updateLatest={updateLatest}
        onOpenUpdate={() => openSettings('updates')}
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
          onQueueHeight={changeQueueHeight}
          onQueueListWidth={changeQueueListWidth}
          onZoom={changeZoom}
          onTheme={changeTheme}
          onCloseToTray={changeCloseToTray}
          onStartMinimized={changeStartMinimized}
          onAutoStart={changeAutoStart}
        />
      )}
    </>
  )
}

export default App
