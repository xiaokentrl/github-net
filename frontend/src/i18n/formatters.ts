// 数字和日期统一走 Intl，用当前语言环境格式化，不在组件里自己拼。
import { getLocale } from './index'

export function formatInteger(value: number): string {
  return new Intl.NumberFormat(getLocale()).format(value)
}
