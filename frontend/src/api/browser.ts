// 打开系统浏览器只走这一层：组件不直接 import Wails 运行时。
// 与 api/client.ts 同一个规矩——运行时调用集中在这里，页面只拿函数。
import { Browser } from '@wailsio/runtime'

// 外链安全（安全边界方案）：调用点只允许传编译期写死的 https:// 常量（运行时断言），
// 直通系统默认浏览器打开。失败会 reject，调用方必须接住并显示出来。
// 不引入「运行时任意 URL」的调用面——以后要开动态地址，必须先在 Go 侧加 scheme 白名单。
export function openExternal(url: string): Promise<void> {
  if (!url.startsWith("https://")) {
    return Promise.reject(new Error(`只允许打开 https:// 外链，收到 ${url.split(":")[0] || "非 https"} 地址`))
  }
  return Browser.OpenURL(url)
}
