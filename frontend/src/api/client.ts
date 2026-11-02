// 组件不直接调用 Wails 绑定，只走这一层；这里也不写业务规则。
// 后端 28 条命令，这层登记界面用得上的 25 条（另有 Ping、RuntimeInfo、SettingsStore 三条没用到）。
// 登记不等于界面都在用：加速方法这 2 条从本轮起由详情页的选择区调用。
import {
  ApplyItems,
  CancelTask,
  ClearFinishedTasks,
  ClearSourcesCache,
  DetectBudget,
  ClearQueue,
  DetectItems,
  GetAppStatus,
  GetAutomationStatus,
  GetCandidatePool,
  GetDetectResult,
  GetItems,
  GetSettings,
  GetStrategies,
  GetTasks,
  CheckForUpdates,
  DownloadUpdate,
  OpenHosts,
  PauseQueue,
  RestoreAllHosts,
  RetryFailedTasks,
  RestoreItems,
  ResumeQueue,
  SaveSettings,
  SelectStrategy,
} from '../../bindings/github.com/xiaokentrl/github-net/internal/app/service'
import type { Patch } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { AppStatus, DetectBudgetPreview, DetectResultView, ItemsSnapshot, PoolEntryView, SourcesCacheResult, StrategySnapshot } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Config } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { AutomationStatus } from '../../bindings/github.com/xiaokentrl/github-net/internal/event/models'
import type { RetryResult, Snapshot, Task } from '../../bindings/github.com/xiaokentrl/github-net/internal/task/models'
import type { Info as UpdateInfo } from '../../bindings/github.com/xiaokentrl/github-net/internal/updater/models'
import type { UpdateDownloadResult } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'

export type NetworkProfile = 'low' | 'standard' | 'full'

// 设置对话框那几屏会改的值：档位、每轮候选上限、侧栏宽度、队列高度、任务列表宽度、缩放、主题、
// 两个焦点跟随开关。
export type SettingsPatch = {
  networkProfile?: NetworkProfile
  language?: string
  maxCandidateProbesPerRun?: number
  sidebarWidth?: number
  queueHeight?: number
  queueListWidth?: number
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
    queueListWidth: patch.queueListWidth ?? null,
    closeToTray: patch.closeToTray ?? null,
    startMinimized: patch.startMinimized ?? null,
    autoStart: patch.autoStart ?? null,
    detectFocusFollow: patch.detectFocusFollow ?? null,
    applyFocusFollow: patch.applyFocusFollow ?? null,
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
// 已在队列里的项目返回同一条任务，不会重复排队。
export function detectItems(itemIds: string[], strategyId = ''): Promise<Task[] | null> {
  return DetectItems(itemIds, strategyId)
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

export function getTasks(): Promise<Snapshot> {
  return GetTasks()
}

export function pauseQueue(): Promise<Snapshot> {
  return PauseQueue()
}

export function resumeQueue(): Promise<Snapshot> {
  return ResumeQueue()
}

export function cancelTask(taskId: string): Promise<Snapshot> {
  return CancelTask(taskId)
}

export function clearFinishedTasks(): Promise<Snapshot> {
  return ClearFinishedTasks()
}

export function clearQueue(): Promise<Snapshot> {
  return ClearQueue()
}

// 重试失败/已回滚的应用任务：后端建 retryOf 指回原任务的新任务，原记录保持终态。
export function retryFailedTasks(taskIds: string[]): Promise<RetryResult[] | null> {
  return RetryFailedTasks(taskIds)
}

// 批量检测前的预算预估：Needed 盖不全时界面先请用户确认再发起。
export function detectBudget(itemIds: string[]): Promise<DetectBudgetPreview> {
  return DetectBudget(itemIds)
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
