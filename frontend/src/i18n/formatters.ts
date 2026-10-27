// 数字和日期统一走 Intl，用当前语言环境格式化，不在组件里自己拼。
import { getLocale } from './index'

export function formatInteger(value: number): string {
  return new Intl.NumberFormat(getLocale()).format(value)
}

// 后端给的时间是 RFC3339（带时区的绝对时间），显示按当前语言环境排，不改数值本身。
export function formatDateTime(iso: string): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  return new Intl.DateTimeFormat(getLocale(), { dateStyle: 'short', timeStyle: 'medium' }).format(date)
}
