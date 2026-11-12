// 组件不直接调用 Wails 绑定，只走这一层；这里也不写业务规则。
// 后端命令按需登记，界面用不上的（Ping、RuntimeInfo、SettingsStore）不在这里列。
import {
  ApplyItems,
  ClearAllIPCache,
  ClearFinishedTasks,
  ClearSourcesCache,
  DetectBudget,
  DetectItems,
  GetAppStatus,
  GetAutomationStatus,
  GetCandidatePool,
  GetDetectResult,
  GetItems,
  GetLastDetectProfile,
  GetSettings,
  GetStrategies,
  GetTasks,
  CheckForUpdates,
  DownloadUpdate,
  OpenHosts,
  PreviewIPCache,
  PreviewRestoreHosts,
  PreviewRestoreHostsItem,
  PreviewRestoreHostsItems,
  PreviewSourcesCache,
  ReprobeIP,
  RestoreAllHosts,
  RestoreItems,
  SaveSettings,
  SelectStrategy,
  SetLastDetectProfile,
} from '../../bindings/github.com/xiaokentrl/github-net/internal/app/service'
import type { Patch } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type {
  AppStatus,
  DetectBudgetPreview,
  DetectResultView,
  HostsRestorePreview,
  IPCacheClearResult,
  IPCachePreview,
  ItemsSnapshot,
  PoolEntryView,
  SourceCachePreview,
  SourcesCacheResult,
  StrategySnapshot,
} from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
// 预览相关的两个返回类型由组件直接使用，从 bindings 透出，组件只认这一层。
export type { HostsRestorePreview, IPCachePreview, PoolEntryView, SourceCachePreview } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Config } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { AutomationStatus } from '../../bindings/github.com/xiaokentrl/github-net/internal/event/models'
import type { Snapshot, Task } from '../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import type { Info as UpdateInfo } from '../../bindings/github.com/xiaokentrl/github-net/internal/updater/models'
import type { UpdateDownloadResult } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'

export type NetworkProfile = 'low' | 'standard' | 'full'

// 设置对话框那几屏会改的值：档位、每轮候选上限、侧栏宽度、队列高度、任务列表宽度、缩放、主题、
// 两个焦点跟随开关，以及右下角提示面板的启用开关（界面说「显示」，配置里存「禁用」，取反一次）。
export type SettingsPatch = {
  networkProfile?: NetworkProfile
  language?: string
  maxCandidateProbesPerRun?: number
  sidebarWidth?: number
  queueHeight?: number
  queueHeightLocked?: boolean
  queueListWidth?: number
  toastDisabled?: boolean
  zoom?: number
  theme?: string
  closeToTray?: boolean
  startMinimized?: boolean
  autoStart?: boolean
  autoDetectMenus?: string[]
  autoApplyOnStartup?: boolean
  autoCloseAfterApply?: boolean
  detectFocusFollow?: boolean
  applyFocusFollow?: boolean
  restartBrowserAfterApply?: boolean
  checkedItemIds?: string[]
}

export function getAppStatus(): Promise<AppStatus> {
  return GetAppStatus()
}

export function getItems(): Promise<ItemsSnapshot> {
  return GetItems()
}

export function getSettings(): Promise<Config> {
  return GetSettings()
}

// 「记住选项」的档位：后端单独一份小文件，不进 config.json、不动设置中心。
export function getLastDetectProfile(): Promise<string> {
  return GetLastDetectProfile()
}

export function setLastDetectProfile(profile: string): Promise<void> {
  return SetLastDetectProfile(profile)
}

// 启动自动化最近一次的结论。没跑过时后端给的是 stage 为空串的一份空值，
// 由调用方判空——界面不该把「还没发生」显示成一句失败。
export function getAutomationStatus(): Promise<AutomationStatus> {
  return GetAutomationStatus()
}

// 保存必须带「你手上那份的 revision」和幂等键；后端比完版本才写，失败会明确 reject。
// 没给的字段在 Patch 里是 null，后端按「这次不改」处理，不会清零。
export function saveSettings(patch: SettingsPatch, expectedRevision: number, mutationID: string): Promise<Config> {
  const body: Patch = {
    networkProfile: patch.networkProfile ?? null,
    language: patch.language ?? null,
    maxCandidateProbesPerRun: patch.maxCandidateProbesPerRun ?? null,
    confirmUpdateDownloadOnMetered: null,
    defaultStrategyByScope: null,
    theme: patch.theme ?? null,
    zoom: patch.zoom ?? null,
    sidebarWidth: patch.sidebarWidth ?? null,
    queueHeight: patch.queueHeight ?? null,
    queueHeightLocked: patch.queueHeightLocked ?? null,
    queueListWidth: patch.queueListWidth ?? null,
    toastDisabled: patch.toastDisabled ?? null,
    closeToTray: patch.closeToTray ?? null,
    startMinimized: patch.startMinimized ?? null,
    autoStart: patch.autoStart ?? null,
    detectFocusFollow: patch.detectFocusFollow ?? null,
    applyFocusFollow: patch.applyFocusFollow ?? null,
    restartBrowserAfterApply: patch.restartBrowserAfterApply ?? null,
    checkedItemIds: patch.checkedItemIds ?? null,
    autoDetectMenus: patch.autoDetectMenus ?? null,
    autoApplyOnStartup: patch.autoApplyOnStartup ?? null,
    autoCloseAfterApply: patch.autoCloseAfterApply ?? null,
  }
  return SaveSettings(body, expectedRevision, mutationID)
}

// ===== 加速方法选择 =====
// 两条都只是记账：列这个范围注册表里真有的方法、把用户选的存进配置服务。
// 都不启动检测、不写 Hosts、不碰系统代理；保存沿用设置的并发规则（版本 + 幂等键）。

export function getStrategies(scopeId: string): Promise<StrategySnapshot> {
  return GetStrategies(scopeId)
}

// expectedRevision 用上一次快照带回的版本号（它来自同一份配置），保存成功返回的就是最新快照。
export function selectStrategy(scopeId: string, strategyId: string, expectedRevision: number, mutationID: string): Promise<StrategySnapshot> {
  return SelectStrategy(scopeId, strategyId, expectedRevision, mutationID)
}

// ===== 检测与队列 =====

// 发起检测：后端整批校验（项目在目录里、范围有可用方法、方法适用），不合格整批拒绝。
// strategyId 给空按范围默认（配置里存的 → 出厂默认「全部公共源」）；给了就整批用它。
// profile 是本次检测档位：空串沿用设置里的档位；指定值只在这一轮生效，不写回配置。
export function detectItems(itemIds: string[], strategyId = '', profile = ''): Promise<Task[] | null> {
  return DetectItems(itemIds, strategyId, profile)
}

// 取整份候选池（缓存：每个域名最近一轮的地址），后端已按延迟升序排好、行带二级菜单归属。
// 没检测过时是空数组，不是错误。
export function getCandidatePool(): Promise<PoolEntryView[]> {
  return GetCandidatePool().then((rows) => rows ?? [])
}

// 取一个项目最近一轮的检测结论；没测过时后端给空形状，不是错误。
export function getDetectResult(itemId: string): Promise<DetectResultView> {
  return GetDetectResult(itemId)
}

// 清掉勾中项目所涉及的那几份公共源的「当天已下载」记录：清完下一轮检测才会重新下载源。
// 勾了哪个菜单就清那个菜单的源；回执里的 cleared 是真删掉的份数，可能比 sources 少（今天还没下载过）。
export function clearSourcesCache(itemIds: string[]): Promise<SourcesCacheResult> {
  return ClearSourcesCache(itemIds)
}

// 清掉全部检测出来的 IP 缓存：轮底账、逐轮候选、地址池和内存里最近一轮结论。
// 回执的 rounds / addresses 是真删掉的行数；配置、已生效的 Hosts 和还原依据不动。
export function clearAllIPCache(): Promise<IPCacheClearResult> {
  return ClearAllIPCache()
}

// ===== 清理弹框预览 =====
// 三项都只读：勾了哪一项才拉哪一项，让用户先看清楚要清／还原的是什么再点「立即清理」。
// 后端同名命令只算不删、不写盘；绑定由 wails 生成，函数名与 Go 方法一致。

// 已选缓存源：勾中项目涉及的公共源，以及每份今天是否已下载（只有已下载的才会被真删）。
// wails 对切片返回类型带 null，这里统一兜底成空数组。
export function previewSourcesCache(itemIds: string[]): Promise<SourceCachePreview[]> {
  return PreviewSourcesCache(itemIds).then((rows) => rows ?? [])
}

// 全部 IP 缓存：轮数、地址数、去重域名数、逐轮候选数、内存结论数，以及候选池明细。
export function previewIPCache(): Promise<IPCachePreview> {
  return PreviewIPCache()
}

// 清除已写入的 Hosts（全量还原）：每个受管理域名恢复前／后的 IP 对比，以及涉及的项目数。
export function previewRestoreHosts(): Promise<HostsRestorePreview> {
  return PreviewRestoreHosts()
}

// 只还原一个二级菜单：这个项目的域名还原前／后分别指向哪个 IP。
// 与全量预览分开走后端单项目算法：共享域名在其他项目还认领时 IP 不变，两者结论不一样。
export function previewRestoreHostsItem(itemId: string): Promise<HostsRestorePreview> {
  return PreviewRestoreHostsItem(itemId)
}

// 「还原本菜单」：按传入顺序一次还原这 N 个二级菜单后的前后对比。
// 必须整体算，不能拿 N 份单项目预览去拼——共享域名一起释放后的结果和单独释放不同。
export function previewRestoreHostsItems(itemIds: string[]): Promise<HostsRestorePreview> {
  return PreviewRestoreHostsItems(itemIds)
}

// 单条复测：现场重探候选池里的一条地址，后端把结论刷回那一行并返回刷新后的整行。
// 同步等结果，不进任务队列；失败直接 reject，界面按错误码显示原因。
export function reprobeIP(hostname: string, ip: string): Promise<PoolEntryView> {
  return ReprobeIP(hostname, ip)
}

export function getTasks(): Promise<Snapshot> {
  return GetTasks()
}

export function clearFinishedTasks(): Promise<Snapshot> {
  return ClearFinishedTasks()
}

// 批量检测前的预算预估：按本次钉定的方法与选中的档位算，Needed 盖不全时界面先请用户确认再发起。
// strategyId 与 detectItems 同口径：「使用已有储存」不联网，预估为 0，不弹确认框。
export function detectBudget(itemIds: string[], strategyId = '', profile = ''): Promise<DetectBudgetPreview> {
  return DetectBudget(itemIds, strategyId, profile)
}

// ===== 应用与还原 =====
// 这批命令的真正落盘在特权辅助服务那边；入口只校验形状，失败会带稳定错误码回来。

// 应用：把该项目最近一轮检测的可用候选写进系统 Hosts。没检测过会在入口被拒。
export function applyItems(itemIds: string[]): Promise<Task[] | null> {
  return ApplyItems(itemIds)
}

// 单项还原：把该项目退回应用前的映射，进队列执行。
export function restoreItems(itemIds: string[]): Promise<Task[] | null> {
  return RestoreItems(itemIds)
}

// 全部还原：服务侧一笔原子操作，返回实际还原的项目数。
export function restoreAllHosts(): Promise<number> {
  return RestoreAllHosts()
}

// 打开 Hosts：请系统默认编辑器打开，失败带原因。
export function openHosts(): Promise<void> {
  return OpenHosts()
}

// ===== 更新检查 =====
// 只比较和显示：下载、校验、安装属安全更新流程，这里一步都不做。

export function checkForUpdates(): Promise<UpdateInfo> {
  return CheckForUpdates()
}

// 下载并校验当前平台的安装包，通过后交给系统桌面安装流程；包路径和大小回给界面展示。
// 只做用户确认过的这一次下载；失败（摘要/签名不过）时安装包已作废，绝不交给系统。
export function downloadUpdate(info: UpdateInfo): Promise<UpdateDownloadResult> {
  return DownloadUpdate(info)
}
