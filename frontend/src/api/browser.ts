// 打开系统浏览器只走这一层：组件不直接 import Wails 运行时。
// 与 api/client.ts 同一个规矩——运行时调用集中在这里，页面只拿函数。
import { Browser } from '@wailsio/runtime'

// 后端会先校验并清洗地址（javascript:/data:/file:/ftp: 一律拒），
// 再由系统在默认浏览器里打开。失败会 reject，调用方必须接住并显示出来。
export function openExternal(url: string): Promise<void> {
  return Browser.OpenURL(url)
}
