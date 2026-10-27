// 组件不直接调用 Wails 绑定，只走这一层；这里也不写业务规则。
// 只登记后端已经接好的 4 个命令，其余 14 个仍是契约，不为它们写空壳。
import {
  GetAppStatus,
  GetItems,
  GetSettings,
  SaveSettings,
} from '../../bindings/github.com/xiaokentrl/github-net/internal/app/service'
import { Patch } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'
import type { AppStatus, ItemsSnapshot } from '../../bindings/github.com/xiaokentrl/github-net/internal/app/models'
import type { Config } from '../../bindings/github.com/xiaokentrl/github-net/internal/config/models'

export type NetworkProfile = 'low' | 'standard' | 'full'

// 目前界面上真正会改的设置只有流量档位；设置对话框那一屏再加其它字段。
export type SettingsPatch = {
  networkProfile?: NetworkProfile
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
export function saveSettings(patch: SettingsPatch, expectedRevision: number, mutationID: string): Promise<Config> {
  const body = new Patch({ networkProfile: patch.networkProfile })
  return SaveSettings(body, expectedRevision, mutationID)
}
