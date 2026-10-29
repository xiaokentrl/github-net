// 组件不直接调用 Wails 绑定，只走这一层；这里也不写业务规则。
// 后端已接 13 个命令：4 个读/设命令、检测 2 条、队列 7 条；绑定都已生成入库。
// 这一层只登记界面用得上的命令；应用、还原、打开 Hosts、检查更新在后端仍是契约，不为它们写空壳。
import {
  ApplyItems,
  CancelTask,
  ClearFinishedTasks,
  ClearQueue,
  DetectItems,
  GetAppStatus,
  GetDetectResult,
  GetItems,
  GetSettings,
  GetTasks,
  OpenHosts,
  PauseQueue,
  RestoreAllHosts,
  RestoreItems,
  ResumeQueue,
  SaveSettings,
} from '../../bindings/github.com/xiaokentrl/github-net/internal/app/service'
import type { Patch } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { AppStatus, DetectResultView, ItemsSnapshot } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Config } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { Snapshot, Task } from '../../bindings/github.com/xiaokentrl/github-net/internal/task/models'

export type NetworkProfile = 'low' | 'standard' | 'full'

// 设置对话框那几屏会改的值：档位、每轮候选上限、侧栏宽度、队列高度、缩放、主题。
// 没列出来的字段（语言、开机自启等）后端已接但界面还没有能真正改的控件，这里不加。
export type SettingsPatch = {
  networkProfile?: NetworkProfile
  maxCandidateProbesPerRun?: number
  sidebarWidth?: number
  queueHeight?: number
  zoom?: number
  theme?: string
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

// 保存必须带「你手上那份的 revision」和幂等键；后端比完版本才写，失败会明确 reject。
// 没给的字段在 Patch 里是 null，后端按「这次不改」处理，不会清零。
export function saveSettings(patch: SettingsPatch, expectedRevision: number, mutationID: string): Promise<Config> {
  const body: Patch = {
    networkProfile: patch.networkProfile ?? null,
    maxCandidateProbesPerRun: patch.maxCandidateProbesPerRun ?? null,
    confirmUpdateDownloadOnMetered: null,
    defaultStrategyByScope: null,
    language: null,
    theme: patch.theme ?? null,
    zoom: patch.zoom ?? null,
    sidebarWidth: patch.sidebarWidth ?? null,
    queueHeight: patch.queueHeight ?? null,
    detectFocusFollow: null,
    applyFocusFollow: null,
    autoStart: null,
    startMinimized: null,
    autoDetectOnStartup: null,
    autoApplyOnStartup: null,
  }
  return SaveSettings(body, expectedRevision, mutationID)
}

// ===== 检测与队列 =====

// 发起检测：后端整批校验（项目在目录里、范围已选方法、方法可用），不合格整批拒绝。
// 已在队列里的项目返回同一条任务，不会重复排队。
export function detectItems(itemIds: string[]): Promise<Task[] | null> {
  return DetectItems(itemIds)
}

// 取一个项目最近一轮的检测结论；没测过时后端给空形状，不是错误。
export function getDetectResult(itemId: string): Promise<DetectResultView> {
  return GetDetectResult(itemId)
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
