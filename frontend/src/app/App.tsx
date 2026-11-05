import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { AppStatus, CatalogCategory, CatalogItem, HostsState, ItemsSnapshot, SourcesCacheResult } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { AutomationStatus } from '../../bindings/github.com/xiaokentrl/github-net/internal/event/models'
import type { Task } from '../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import { applyItems, clearSourcesCache, detectBudget, detectItems, getAutomationStatus, getAppStatus, getItems, getSettings, getTasks, openHosts, restoreItems, type NetworkProfile, type SettingsPatch } from '../api/client'
import { subscribeAutomationStatus, subscribeRuntimeStatus, subscribeSettingsChanged, subscribeUpdateStatus } from '../api/events'
import { automationText } from '../components/layout/StatusBar'
import { getErrorMessage } from '../api/errors'
import { AppShell } from '../components/layout/AppShell'
import { SettingsEntry } from '../components/layout/SettingsEntry'
import { SyncEntry } from '../components/layout/SyncEntry'
import { SidebarTools } from '../components/layout/SidebarTools'
import { StatusBar } from '../components/layout/StatusBar'
import { Toolbar } from '../components/layout/Toolbar'
import { CategoryTree } from '../components/navigation/CategoryTree'
import type { HostsLookup } from '../components/navigation/CategoryNode'
import { liveByItem } from '../components/navigation/treeStatus'
import { Dialog } from '../components/ui/Dialog'
import { Toast } from '../components/ui/Toast'
import { pushNotice } from '../components/ui/toastStore'
import uiStyles from '../components/ui/ui.module.css'
import { SettingsDialog, type SettingsPaneID } from '../features/settings/SettingsDialog'
import { ItemDetail } from '../features/items/ItemDetail'
import { TaskPanel } from '../features/tasks/TaskPanel'
import { subscribeTaskEvents } from '../features/tasks/taskEvents'
import { isTerminalTask, refreshResult, taskStateSnapshot, useTaskQueue } from '../features/tasks/taskStore'
import { useRestoreAllTasks } from '../features/tasks/useRestoreAll'
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

// 入口被后端整批拒了：原位那句话照旧留着（结构规划要求错误必须在操作附近可见，不能只靠会消失的提示），
// 这里再递一条常驻提示进右下角面板——用户可能正在别的分类、别的屏幕，光看按钮旁边那一句看不到。
// 只递后端给的那句：本地前置条件（「还没勾选项目」）就留在按钮旁边，不额外喊一嗓子。
function reportRejection(setter: (text: string) => void, err: unknown): void {
  const message = getErrorMessage(err)
  setter(message)
  pushNotice('error', message)
}

// 清理回执的文字：三种情况各说各的，数字全部来自后端，界面不自己数。
// 「清掉 N 份」和「今天还没下载过」是两件不同的事——后者不用清，说成「已清理 0 份」会让人以为出错了。
function cacheNoticeOf(res: SourcesCacheResult): { kind: 'info' | 'warning'; text: string } {
  const cleared = res.cleared ?? []
  const sources = res.sources ?? []
  if (cleared.length > 0) {
    return { kind: 'info', text: t('toolbar.clearCache.cleared', { count: cleared.length, names: cleared.join('、') }) }
  }
  if (sources.length === 0) {
    return { kind: 'warning', text: t('toolbar.clearCache.noSource') }
  }
  return { kind: 'info', text: t('toolbar.clearCache.noneToday', { count: sources.length }) }
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
  // 默认无选中（拍板 2026-10-31）：勾选是「接下来要操作哪些」的意图，不替用户预设。
  const [checkedIds, setCheckedIds] = useState<ReadonlySet<string>>(new Set())

  // 展开状态。默认全部收起（拍板 2026-10-31），用户自己点开想看的簇。
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const [selectedId, setSelectedId] = useState('')

  // 右侧详情默认常显（拍板 2026-10-31）：拿到目录后还没选过，就选中第一个子菜单。
  useEffect(() => {
    if (catalogLoad !== 'ready' || selectedId !== '') return
    const first = snapshot?.categories?.[0]?.items?.[0]
    if (first) setSelectedId(first.id)
  }, [catalogLoad, snapshot, selectedId])

  const [dialogOpen, setDialogOpen] = useState(false)
  const [dialogPane, setDialogPane] = useState<SettingsPaneID>('layout')

  const [sequence, setSequence] = useState(0)
  const [dropped, setDropped] = useState(0)

  // 检测/应用/还原入口被后端拒绝时的一句话，显示在详情页对应按钮旁边；下一次成功就清掉。
  const [detectError, setDetectError] = useState('')
  const [hostsError, setHostsError] = useState('')
  // 启动自动检查发现的最新版本；空串表示没有已知更新（没查过、没新版本或检查失败）。
  const [updateLatest, setUpdateLatest] = useState('')

  // 启动后那条自动链最近一次的结论；null 表示这一轮还没跑过，状态栏那一格就不出现。
  const [automation, setAutomation] = useState<AutomationStatus | null>(null)

  // 收到的最新序号存在 ref 里，订阅回调才不会读到旧值。
  const sequenceRef = useRef(0)
  // 只认最后一次请求的结果，重试和 StrictMode 重复挂载都不会串台。
  const statusRequestRef = useRef(0)
  const settingsRequestRef = useRef(0)
  const catalogRequestRef = useRef(0)
  const automationRequestRef = useRef(0)

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

  // 自动化结论的补读：这条链可能在窗口刚起来那一刻就跑完了，事件整个错过。
  // 后端把最近一份留在内存里，挂载时先读一次再订阅，读到的 stage 是空串就当没跑过。
  const loadAutomation = useCallback(() => {
    const request = automationRequestRef.current + 1
    automationRequestRef.current = request
    getAutomationStatus()
      .then((latest) => {
        if (automationRequestRef.current !== request) return
        if (latest && latest.stage !== '') setAutomation(latest)
      })
      .catch(() => {
        // 读不到就只靠事件；这一格本来就是可选信息，不弹错误条。
      })
  }, [])

  useEffect(() => {
    loadStatus()
    loadSettings()
    loadCatalog()
    loadAutomation()
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
      // 任务落定的统一同步层（拍板 2026-11-03 从底层修复）：
      // 任何类型的任务落定都做一轮权威刷新——Hosts 状态（loadCatalog）+ 该项目的检测结果
      // （refreshResult），再在 800ms 后补一轮，吸收后端写盘与元数据落盘的时序差。
      // 以前各处散着「apply/restore 才刷目录」「detect 靠结果存储自己追」的碎片规则，
      // 每条都可能漏；漏了就是用户报「表格没同步」。
      loadCatalog()
      void refreshResult(settled.itemId).catch(() => undefined)
      window.setTimeout(() => {
        loadCatalog()
        void refreshResult(settled.itemId).catch(() => undefined)
      }, 800)
      // 检测轮收尾回执（拍板 2026-11-03）：一项一项递就是刷屏，按轮聚合一条。
      // 该轮全部任务到终态的那一刻算收尾；有失败的用警告档，干净的用成功档。
      if (settled.kind === 'detect' && settled.status === 'done' && settled.roundId) {
        const snap = taskStateSnapshot()
        const siblings = snap.tasks.filter((tk) => tk.roundId === settled.roundId)
        if (siblings.length > 0 && siblings.every((tk) => tk.status !== 'waiting' && tk.status !== 'running')) {
          const failed = siblings.filter((tk) => tk.status === 'failed' || tk.status === 'skipped').length
          const total = siblings.length
          pushNotice(
            failed > 0 ? 'warning' : 'success',
            failed > 0
              ? t('notice.round.detectDone.failed', { total, failed })
              : t('notice.round.detectDone', { total }),
          )
        }
      }
      // 写系统 Hosts 的结果递进右下角面板：辅助服务没在跑、写失败被回滚这两种，
      // 光靠队列面板那一行很容易被划过去；检测类不递（一轮几十条，递出来就是刷屏）。
      // 文字里不带项目名，同一批的同一种失败自然并成一条。
      const reason = settled.errorMsg || t('error.unknown')
      if (settled.status === 'failed') pushNotice('error', t('hosts.task.failed', { reason }))
      if (settled.status === 'rolledback') pushNotice('error', t('hosts.task.rolledback', { reason }))
      if (settled.kind === 'apply' && settled.status === 'done') pushNotice('info', t('hosts.apply.doneHint'))
    })

    // 启动自动检查的结果落到顶栏徽标；详情看设置中心的检测升级屏。
    // 徽标只在「有新版本」时出现，检查失败在界面上本来完全无声——这里给一条会自己走的警告：
    // 更新检查不是主流程，不值得留一条常驻错误。
    const unsubscribeUpdate = subscribeUpdateStatus((payload) => {
      if (payload.error) {
        pushNotice('warning', t('update.checkFailed', { reason: payload.error }))
        return
      }
      if (payload.info?.updateAvailable && payload.info.latestVersion) {
        setUpdateLatest(payload.info.latestVersion)
        pushNotice('info', t('update.available', { version: payload.info.latestVersion }))
      }
    })

    // 托盘切主题推来的新配置：整体刷新（语言/主题/缩放一条路），和设置屏保存同一条 applyConfig。
    const unsubscribeSettings = subscribeSettingsChanged((config) => {
      applyConfig(config)
    })

    // 启动自动化每一步推来的结论：后到的一份覆盖前一份，界面只显示手上这最新一句。
    // 同一结论同时递一条进右下角面板（拍板 2026-11-03）：阶段或原因码变了才推一条，
    // 进行中的重复进度不打扰；关键收尾（跑完/跳过/失败/不退出）在面板里留底。
    let lastAutomationKey = ''
    const unsubscribeAutomation = subscribeAutomationStatus((payload) => {
      setAutomation(payload)
      const key = `${payload.stage}:${payload.reason ?? ''}`
      if (key === lastAutomationKey) return
      lastAutomationKey = key
      if (payload.stage === 'detecting' || payload.stage === 'applying' || payload.stage === 'closing') return
      if (!payload.stage) return
      const kind = payload.stage === 'failed' ? 'error' : payload.stage === 'done' && !payload.reason ? 'success' : 'warning'
      const text = automationText(payload, menuNamesRef.current)
      pushNotice(kind, text)
    })

    return () => {
      // 让仍在路上的响应作废，并退订事件；退订必须调用，否则回调会一直堆积。
      statusRequestRef.current += 1
      settingsRequestRef.current += 1
      catalogRequestRef.current += 1
      automationRequestRef.current += 1
      unsubscribe()
      unsubscribeTasks()
      unsubscribeUpdate()
      unsubscribeSettings()
      unsubscribeAutomation()
    }
  }, [loadStatus, loadSettings, loadCatalog, loadAutomation, applyConfig])

  // 后端每次 GetItems 都现读一遍 Hosts，这里只留最新一份：itemId → 那次读出来的整条状态。
  const hostsOf: HostsLookup = useMemo(() => {
    const map = new Map<string, HostsState>()
    for (const state of snapshot?.hosts ?? []) map.set(state.itemId, state)
    return map
  }, [snapshot])

  // itemId → 目录里这个项目的域名清单。队列明细在整段 Hosts 判不出归属时（后端逐条给空数组）
  // 退回这份清单逐条列域名，值写「判不出归属」——和详情页那条退路同一个口径，不自己编 IP。
  const hostnamesOf = useMemo(() => {
    const map = new Map<string, string[]>()
    for (const cat of snapshot?.categories ?? []) for (const item of cat.items ?? []) map.set(item.id, item.hostnames ?? [])
    return map
  }, [snapshot])

  // 树上那一行的状态字和色点要跟着队列走：正在检测、正在写 Hosts、正在还原的项目，原型是整行变色。
  // 队列只有 taskStore 这一份，TaskPanel 和树读的是同一个快照，不存在两份真相。
  const queue = useTaskQueue()
  // 让队列面板展开的把手：点「检测全部/应用队列/恢复/清理缓存」这类要动队列的按钮时拉一下，
  // 队列面板在收起态就自动回到展开高度。把手由 TaskPanel 塞进来。
  const queueExpandRef = useRef<(() => void) | null>(null)
  const liveOf = useMemo(() => liveByItem(queue.tasks), [queue.tasks])
  // 状态栏的「上次检测」看全应用最近一轮，不是当前选中项那一轮。
  // 两处都取最晚的那个：新一轮开跑时该项目的旧结果会被清掉，但队列里已完成的任务记录还在；
  // 用户清空队列后反过来，结果还在。只盯一处会在这两种时候倒回「未检测」，那是假话。
  const lastDetectAt = useMemo(() => {
    let latest: string | null = null
    let latestMS = Number.NEGATIVE_INFINITY
    const consider = (at?: string) => {
      if (!at) return
      const ms = new Date(at).getTime()
      if (!Number.isFinite(ms) || ms <= latestMS) return
      latestMS = ms
      latest = at
    }
    for (const view of Object.values(queue.results)) consider(view.finishedAt)
    for (const task of queue.tasks) if (task.kind === 'detect') consider(task.finishedAt)
    return latest
  }, [queue.results, queue.tasks])

  // 默认无选中；勾选持久化（拍板 2026-11-03）：目录和设置都就绪后恢复上次的勾选
  // （配置里的 ID 目录里没有的就过滤掉），之后的每次变化防抖写回配置，跨重装有效。
  const checkedInitializedRef = useRef(false)
  const savedCheckedRef = useRef('')
  useEffect(() => {
    if (checkedInitializedRef.current || catalogLoad !== 'ready' || !settings) return
    checkedInitializedRef.current = true
    const stored = new Set(settings.treeBehavior.checkedItemIds ?? [])
    const restored = new Set<string>()
    for (const cat of snapshot?.categories ?? []) {
      for (const it of cat.items ?? []) {
        if (stored.has(it.id)) restored.add(it.id)
      }
    }
    savedCheckedRef.current = [...restored].sort().join('\n')
    setCheckedIds(restored)
  }, [catalogLoad, snapshot, settings])


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

  // 默认折叠：进来全部收着，用户自己想看哪簇点开哪簇（拍板 2026-10-31，替代原型的「初始全展开」）。
  // 运行中的焦点跟随照旧会把正在跑的分类展开，不受这个默认影响。

  // 工具栏「折叠」：全展开时一键全折叠，否则一键全展开（原型第 3599–3610 行同一条规则）。
  const foldAll = useCallback(() => {
    const categories = snapshot?.categories ?? []
    setExpanded((current) => {
      if (categories.length > 0 && categories.every((cat) => current.has(cat.id))) return new Set()
      return new Set(categories.map((cat) => cat.id))
    })
  }, [snapshot])

  // 侧栏顶部「全选」按钮在两种说法之间切换：已经全勾上时显示「全不选」（原型第 3588–3593 行）。
  const allChecked = useMemo(() => {
    const ids = (snapshot?.categories ?? []).flatMap((cat) => (cat.items ?? []).map((it) => it.id))
    return ids.length > 0 && ids.every((id) => checkedIds.has(id))
  }, [snapshot, checkedIds])

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

  // 一级菜单清单：设置屏那张「启动后自动检测哪些主菜单」的选项，和自动化状态行把菜单 ID
  // 换成名字，用的都是目录里这一份，不另列一份菜单名。
  const menuOptions = useMemo(
    () => (snapshot?.categories ?? []).map((cat) => ({ value: cat.id, label: cat.name })),
    [snapshot],
  )
  const menuNamesRef = useRef<Record<string, string>>({})
  const menuNamesById = useMemo(() => {
    const map: Record<string, string> = {}
    for (const cat of snapshot?.categories ?? []) map[cat.id] = cat.name
    return map
  }, [snapshot])
  menuNamesRef.current = menuNamesById

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

  // 勾选变化的防抖写回：和初始化时记录的那份一致就不写，避免白涨 revision。
  useEffect(() => {
    if (!checkedInitializedRef.current) return
    const serialized = [...checkedIds].sort().join('\n')
    if (serialized === savedCheckedRef.current) return
    const timer = window.setTimeout(() => {
      savedCheckedRef.current = serialized
      submit({ checkedItemIds: [...checkedIds] }, t('settings.subject.checkedItems'), 'checkedItems')
    }, 400)
    return () => window.clearTimeout(timer)
  }, [checkedIds, submit])

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

  const changeLanguage = useCallback(
    (language: string) => {
      if (!settings || settings.appearance.language === language) return
      submit({ language }, t('settings.subject.language'), 'language')
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

  // 启动自动化三张卡。下游开关在上游不成立时同一次保存里一起落回关闭：
  // 后端的校验是「自动应用必须有勾中的菜单」「自动退出必须开自动应用」，
  // 分两次保存会让界面短暂停在一个自相矛盾的配置上，读到就报错。
  const changeAutoDetectMenus = useCallback(
    (menus: string[]) => {
      if (!settings) return
      const current = settings.startup.autoDetectMenus ?? []
      if (menus.length === current.length && menus.every((id, index) => id === current[index])) return
      const patch: SettingsPatch = { autoDetectMenus: menus }
      if (menus.length === 0) {
        patch.autoApplyOnStartup = false
        patch.autoCloseAfterApply = false
      }
      submit(patch, t('settings.subject.autoDetectMenus'), 'autoDetectMenus')
    },
    [settings, submit],
  )

  const changeAutoApply = useCallback(
    (enabled: boolean) => {
      if (!settings || settings.startup.autoApplyOnStartup === enabled) return
      const patch: SettingsPatch = { autoApplyOnStartup: enabled }
      if (!enabled) patch.autoCloseAfterApply = false
      submit(patch, t('settings.subject.autoApplyOnStartup'), 'autoApplyOnStartup')
    },
    [settings, submit],
  )

  const changeAutoClose = useCallback(
    (enabled: boolean) => {
      if (!settings || settings.startup.autoCloseAfterApply === enabled) return
      submit({ autoCloseAfterApply: enabled }, t('settings.subject.autoCloseAfterApply'), 'autoCloseAfterApply')
    },
    [settings, submit],
  )

  // 两个焦点跟随开关（原型「检测时焦点跟随」「应用时焦点跟随」两张卡）：
  // 开着才在任务开跑时自动展开所在类目并滚动过去；关着时树的折叠状态一律不动，只靠高亮提示。
  const changeDetectFocusFollow = useCallback(
    (enabled: boolean) => {
      if (!settings || settings.treeBehavior.detectFocusFollow === enabled) return
      submit({ detectFocusFollow: enabled }, t('settings.subject.detectFocusFollow'), 'detectFocusFollow')
    },
    [settings, submit],
  )

  const changeApplyFocusFollow = useCallback(
    (enabled: boolean) => {
      if (!settings || settings.treeBehavior.applyFocusFollow === enabled) return
      submit({ applyFocusFollow: enabled }, t('settings.subject.applyFocusFollow'), 'applyFocusFollow')
    },
    [settings, submit],
  )

  const openSettings = useCallback((pane: SettingsPaneID) => {
    setDialogPane(pane)
    setDialogOpen(true)
  }, [])

  // 检测入口：后端整批校验，被拒的原因显示在检测按钮旁边，不弹 Toast。
  // 成功的反馈不在这一层——任务排进队列后，进度由 task:progress 事件推给队列面板和详情页。
  // 同步状态：把目录（含 Hosts 状态）和应用状态一起重新拉一遍——
  // 两条链路各有请求代次保护，重复点击安全；失败由各自既有的错误槽位显示。
  const [syncing, setSyncing] = useState(false)
  const resyncAll = useCallback(() => {
    setSyncing(true)
    Promise.all([loadCatalog(), loadStatus()])
      .finally(() => setSyncing(false))
  }, [loadCatalog, loadStatus])

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
      queueExpandRef.current?.()
      if (groups.size === 0) {
        setDetectError(t('toolbar.detectChecked.none'))
        return
      }
      for (const ids of groups.values()) {
        detectItems(ids).catch((err: unknown) => reportRejection(setDetectError, err))
      }
    },
    [snapshot],
  )

  // 这一轮里「没做」的项不能悄悄消失：右下角给一句、工具栏原位也留一句，
  // 但事实的主场是队列——每一行都带着自己的状态和原因，这里只负责把用户引过去。
  const reportSkippedApply = useCallback((tasks: Task[] | null) => {
    const skipped = (tasks ?? []).filter((tk) => tk.status === 'skipped')
    if (skipped.length === 0) return
    const notice = t('toolbar.applyChecked.skipped', {
      skipped: skipped.length,
      total: (tasks ?? []).length,
      names: skipped.map((tk) => tk.name || tk.itemId).join('、'),
    })
    setHostsError(notice)
    pushNotice('warning', notice)
  }, [])

  // 批量应用：勾选项整批交给后端。界面不再自己筛「有没有可用候选」——
  // 自己筛就是第二套判断标准，用户勾 6 项、队列只剩 1 行时说不清另外 5 行去哪了；
  // 现在做不了的项也在同一轮里以「跳过」落定并写明原因。
  const startApplyChecked = useCallback(
    (checked: ReadonlySet<string>) => {
      setHostsError('')
      if (checked.size === 0) {
        setHostsError(t('toolbar.applyChecked.none'))
        return
      }
      queueExpandRef.current?.()
      applyItems([...checked])
        .then(reportSkippedApply)
        .catch((err: unknown) => reportRejection(setHostsError, err))
    },
    [reportSkippedApply],
  )

  // 清理已选缓存：勾了哪个菜单就清那个菜单的源（拍板 2026-11-02，源一天只下一份）。
  // 结果只走右下角提示面板——这条命令不进队列、不写 Hosts，没有别的界面能显示它。
  // 数字全部来自后端回执：清掉几份、今天到底下载过没有，只有那份落盘记录知道。
  const [clearBusy, setClearBusy] = useState(false)
  // 点工具栏那颗只开确认框，不碰任何数据：原生 window.confirm 的标题和两颗按钮归 WebKit，
  // 语言包改不了，写不出「先不清理／清掉这 N 项缓存」这种带结果的文案，所以换成公共 Dialog。
  const [clearAsk, setClearAsk] = useState<string[] | null>(null)
  const clearCheckedCache = useCallback((checked: ReadonlySet<string>) => {
    if (checked.size === 0) return
    queueExpandRef.current?.()
    setClearAsk([...checked])
  }, [])
  const runClearCheckedCache = useCallback((ids: string[]) => {
    setClearAsk(null)
    setClearBusy(true)
    clearSourcesCache(ids)
      .then((res) => {
        const notice = cacheNoticeOf(res)
        pushNotice(notice.kind, notice.text)
      })
      .catch((err: unknown) => pushNotice('error', getErrorMessage(err)))
      .finally(() => setClearBusy(false))
  }, [])

  // strategyId 给空走范围默认；选择区那颗「检测」按钮会带上下拉选中的方法。
  const startDetect = useCallback((itemId: string, strategyId?: string) => {
    setDetectError('')
    detectItems([itemId], strategyId ?? '').catch((err: unknown) => reportRejection(setDetectError, err))
  }, [])

  // 批量检测：项目清单已由详情页按 detectableItems 过滤，这里不再重算第二遍口径。
  // 先问后端要预算预估；预算盖不全时开确认框请用户拍板，取消就不发（同上一条：不用原生 confirm，文案要写结果）。
  const [budgetAsk, setBudgetAsk] = useState<{ ids: string[]; needed: number; budget: number; shortfall: number } | null>(null)
  const startDetectAll = useCallback(
    (items: CatalogItem[]) => {
      const ids = items.map((it) => it.id)
      setDetectError('')
      detectBudget(ids)
        .then((budget) => {
          if (budget.shortfall > 0) {
            setBudgetAsk({ ids, needed: budget.needed, budget: budget.budget, shortfall: budget.shortfall })
            return
          }
          queueExpandRef.current?.()
          return detectItems(ids).catch((err: unknown) => reportRejection(setDetectError, err))
        })
        .catch((err: unknown) => reportRejection(setDetectError, err))
    },
    [],
  )
  const runDetectAll = useCallback((ids: string[]) => {
    setBudgetAsk(null)
    queueExpandRef.current?.()
    detectItems(ids).catch((err: unknown) => reportRejection(setDetectError, err))
  }, [])

  const startApply = useCallback((itemId: string) => {
    setHostsError('')
    queueExpandRef.current?.()
    applyItems([itemId])
      .then(reportSkippedApply)
      .catch((err: unknown) => reportRejection(setHostsError, err))
  }, [reportSkippedApply])

  // 树侧焦点跟随（原型 followItem，第 2676–2689 行 + 开关说明第 1002–1030 行）：
  // 开启时临时收起其他类目、只展开当前项所属类目，跑完恢复开跑前的展开状态；
  // 关闭时只让状态高亮走，不动展开状态。用户点树 = 手动浏览，当前这项不再抢焦点，
  // 下一项开跑时恢复跟随；这轮里用户手动动过展开状态，结束时就不拿开跑前的快照盖回去。
  const focusManualRef = useRef(false)
  const userTouchedRef = useRef(false)
  const lastItemRef = useRef<string | null>(null)
  const runExpandedRef = useRef<ReadonlySet<string> | null>(null)
  const runningTask = useMemo(() => {
    for (const task of queue.tasks) {
      if (task.status === 'running' || task.status === 'restoring') return task
    }
    return null
  }, [queue.tasks])
  const runningItemId = runningTask?.itemId || null
  const followEnabled =
    runningTask?.kind === 'detect'
      ? (settings?.treeBehavior.detectFocusFollow ?? false)
      : (settings?.treeBehavior.applyFocusFollow ?? false)

  useEffect(() => {
    if (!runningItemId) {
      const saved = runExpandedRef.current
      const touched = userTouchedRef.current
      runExpandedRef.current = null
      focusManualRef.current = false
      userTouchedRef.current = false
      lastItemRef.current = null
      if (saved && !touched) setExpanded(new Set(saved))
      return
    }
    if (lastItemRef.current !== runningItemId) {
      lastItemRef.current = runningItemId
      focusManualRef.current = false
    }
    if (!followEnabled || focusManualRef.current) return
    const cat = (snapshot?.categories ?? []).find((c) => (c.items ?? []).some((it) => it.id === runningItemId))
    if (!cat) return
    setExpanded((current) => {
      if (runExpandedRef.current === null) runExpandedRef.current = new Set(current)
      return new Set([cat.id])
    })
    setSelectedId(runningItemId)
  }, [runningItemId, followEnabled, snapshot])

  // 目标行要等上面展开那一轮渲染完才在 DOM 里，所以滚动单独放一条效应，跟着 expanded 再跑一次。
  useEffect(() => {
    if (!runningItemId || !followEnabled || focusManualRef.current) return
    document.querySelector(`[data-item-id="${runningItemId}"]`)?.scrollIntoView({ block: 'nearest' })
  }, [runningItemId, followEnabled, expanded])

  // 一条还原链路，详情页的「还原此项」和树上的 ↺（单项或整类）都走这里。
  // 命令只代表「排进队列」，返回的任务还在 waiting：除了终态事件之外，这里直接问后端这批任务
  // 落定没有，落定就无条件重读目录。不把表格刷新全押在一条 Wails 事件上——事件没送到、
  // 或者中途被别处并发请求挤掉，列表就会一直停在还原前的样子。
  const startRestoreItems = useCallback((itemIds: string[]) => {
    setHostsError('')
    restoreItems(itemIds)
      .then((queued) => {
        const ids = new Set((queued ?? []).map((t) => t.id))
        if (ids.size === 0) {
          loadCatalog()
          return
        }
        let tries = 0
        const poll = () => {
          getTasks()
            .then((snap) => {
              const mine = (snap.tasks ?? []).filter((t) => ids.has(t.id))
              const settled = mine.length > 0 && mine.every((t) => isTerminalTask(t.status))
              if (settled || tries++ >= 40) {
                loadCatalog()
                // 再补一轮，吸收后端写盘与恢复元数据落盘的时序差。
                window.setTimeout(loadCatalog, 800)
                const failed = mine.find((t) => String(t.status) === 'failed' || String(t.status) === 'rolledback')
                if (failed?.errorMsg) setHostsError(failed.errorMsg)
                return
              }
              window.setTimeout(poll, 250)
            })
            .catch(() => loadCatalog())
        }
        window.setTimeout(poll, 200)
      })
      .catch((err: unknown) => reportRejection(setHostsError, err))
  }, [loadCatalog])

  const startRestore = useCallback((itemId: string) => startRestoreItems([itemId]), [startRestoreItems])

  // 还原整表：当前主菜单下全部已写入的二级菜单一次退回（确认弹框在详情页里做了）。
  const startRestoreCategory = useCallback((itemIds: string[]) => startRestoreItems(itemIds), [startRestoreItems])

  const openHostsFile = useCallback(() => {
    setHostsError('')
    openHosts().catch((err: unknown) => reportRejection(setHostsError, err))
  }, [])

  // 工具栏「恢复」和队列面板那颗共用同一条实现（useRestoreAll）；失败按同一条规矩：原位一句 + 右下角一条。
  // 还原不进队列、没有任务落定事件，所以成功后在这里重读目录，树上的状态才跟着回到「未替换」。
  const handleRestoreError = useCallback((err: unknown) => reportRejection(setHostsError, err), [])
  const { busy: restoreBusy, restoreAll, confirmDialog: restoreAllConfirm } = useRestoreAllTasks(handleRestoreError, loadCatalog)

  const sidebar = (
    <CategoryTree
      categories={snapshot?.categories ?? []}
      hostsOf={hostsOf}
      liveOf={liveOf}
      results={queue.results}
      expanded={expanded}
      selectedId={selectedId}
      checkedIds={checkedIds}
      onToggleChecked={toggleChecked}
      onManualBrowse={() => {
        focusManualRef.current = true
      }}
      load={catalogLoad}
      error={catalogError}
      onRetry={loadCatalog}
      onToggle={(categoryID) => {
        toggleCategory(categoryID)
        userTouchedRef.current = true
      }}
      onSelect={setSelectedId}
      onRestoreItems={startRestoreItems}
    />
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
            clearBusy={clearBusy}
            clearEnabled={checkedIds.size > 0}
            onOpenNetwork={() => openSettings('network')}
            onClearCheckedCache={() => clearCheckedCache(checkedIds)}
            onDetectChecked={() => startDetectChecked(checkedIds)}
            onApplyChecked={() => startApplyChecked(checkedIds)}
            onOpenHosts={openHostsFile}
            restoreBusy={restoreBusy}
            onRestoreAll={() => {
              queueExpandRef.current?.()
              restoreAll()
            }}
          />
        }
        sidebar={sidebar}
        sidebarTools={
          <SidebarTools
            allChecked={allChecked}
            busy={save.kind === 'pending'}
            onCheckAll={checkAll}
            onInvertChecked={invertChecked}
            onFoldAll={foldAll}
          />
        }
        sidebarFooter={
          <>
            <SyncEntry syncing={syncing} onSync={resyncAll} />
            <SettingsEntry onOpen={() => openSettings('layout')} />
          </>
        }
        detail={
          <ItemDetail
            item={selected.item}
            category={selected.category}
            hosts={hostsOf.get(selectedId)}
            hostsOf={hostsOf}
            onSelectItem={(itemId) => {
              setSelectedId(itemId)
              focusManualRef.current = true
            }}
            detectReady={status?.dnsDetectionReady ?? false}
            detectError={detectError}
            hostsError={hostsError}
            onDetect={startDetect}
            onDetectAll={startDetectAll}
            onApply={startApply}
            onRestore={startRestore}
            onRestoreCategory={startRestoreCategory}
            onStrategySaved={loadSettings}
          />
        }
        queue={
          <TaskPanel
            height={settings?.layout.queueHeight ?? null}
            onHeightChange={changeQueueHeight}
            expandRef={queueExpandRef}
            listWidth={settings?.layout.queueListWidth ?? null}
            onListWidthChange={changeQueueListWidth}
            activeItemId={selectedId}
            onPickItem={(itemId) => {
              setSelectedId(itemId)
              focusManualRef.current = true
            }}
            hostsOf={hostsOf}
            hostnamesOf={hostnamesOf}
          />
        }
        updateLatest={updateLatest}
        theme={settings?.appearance.theme ?? 'light'}
        running={runningTask !== null}
        onTheme={changeTheme}
        onOpenUpdate={() => openSettings('updates')}
        statusbar={
          <StatusBar
            status={status}
            settings={settings}
            counts={counts}
            sequence={sequence}
            dropped={dropped}
            automation={automation}
            menuNamesById={menuNamesById}
            lastDetectAt={lastDetectAt}
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
          onLanguage={changeLanguage}
          onCloseToTray={changeCloseToTray}
          onStartMinimized={changeStartMinimized}
          onAutoStart={changeAutoStart}
          menus={menuOptions}
          onAutoDetectMenus={changeAutoDetectMenus}
          onAutoApply={changeAutoApply}
          onAutoClose={changeAutoClose}
          onDetectFocusFollow={changeDetectFocusFollow}
          onApplyFocusFollow={changeApplyFocusFollow}
        />
      )}
      {/* 三处确认框（拍板 2026-11-05）：全部还原／清理已选缓存／预算不够还开不开。
          原来都用系统原生 window.confirm，标题和按钮归 WebKit、语言包改不了，按「按钮写结果」必然不合格。 */}
      {restoreAllConfirm}
      {clearAsk ? (
        <Dialog
          title={t('toolbar.clearCache.ask.title', { count: clearAsk.length })}
          onClose={() => setClearAsk(null)}
          footer={
            <>
              <button type="button" className={uiStyles.btn} onClick={() => setClearAsk(null)}>
                {t('toolbar.clearCache.ask.cancel')}
              </button>
              <button
                type="button"
                className={`${uiStyles.btn} ${uiStyles.primary}`}
                onClick={() => runClearCheckedCache(clearAsk)}
              >
                {t('toolbar.clearCache.ask.action', { count: clearAsk.length })}
              </button>
            </>
          }
        >
          <p>{t('toolbar.clearCache.confirm', { count: clearAsk.length })}</p>
        </Dialog>
      ) : null}
      {budgetAsk ? (
        <Dialog
          title={t('detect.budget.ask.title', { count: budgetAsk.ids.length, shortfall: budgetAsk.shortfall })}
          onClose={() => setBudgetAsk(null)}
          footer={
            <>
              <button type="button" className={uiStyles.btn} onClick={() => setBudgetAsk(null)}>
                {t('detect.budget.ask.cancel')}
              </button>
              <button
                type="button"
                className={`${uiStyles.btn} ${uiStyles.primary}`}
                onClick={() => runDetectAll(budgetAsk.ids)}
              >
                {t('detect.budget.ask.action')}
              </button>
            </>
          }
        >
          <p>
            {t('detect.budget.confirm', {
              needed: budgetAsk.needed,
              budget: budgetAsk.budget,
              shortfall: budgetAsk.shortfall,
            })}
          </p>
        </Dialog>
      ) : null}
      {/* 右下角提示面板：挂在应用根上、整应用只有一份，数据在 components/ui/toastStore 里。 */}
      <Toast />
    </>
  )
}

export default App
