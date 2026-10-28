// 组件不直接调用 Wails 绑定，只走这一层；这里也不写业务规则。
// 后端已经接好 11 个命令：原来那 4 个读命令，加上队列那 7 个。
// 这一层只登记界面真正用得上的 4 个——队列 7 个还没重新生成前端绑定，
// 队列视图接进来时再补；其余 7 个（检测、应用、还原等）仍是契约，不为它们写空壳。
import {
  GetAppStatus,
  GetItems,
  GetSettings,
  SaveSettings,
} from '../../bindings/github.com/xiaokentrl/github-net/internal/app/service'
import type { Patch } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { AppStatus, ItemsSnapshot } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Config } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'

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
