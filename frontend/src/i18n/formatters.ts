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

// 相对时间（「5 分钟前」），用 Intl.RelativeTimeFormat 按语言环境出文案；
// 超过 7 天就不说相对值了（没人读得懂「43200 分钟前」），回落到绝对时间。
export function formatRelativeTime(iso: string): string {
  if (!iso) return ''
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  const seconds = Math.round((date.getTime() - Date.now()) / 1000)
  if (Math.abs(seconds) >= 7 * 24 * 3600) return formatDateTime(iso)
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ['second', 60],
    ['minute', 60],
    ['hour', 24],
    ['day', 0],
  ]
  let value = seconds
  for (const [unit, step] of units) {
    if (step === 0 || Math.abs(value) < step) {
      return new Intl.RelativeTimeFormat(getLocale(), { numeric: 'auto' }).format(Math.round(value), unit)
    }
    value /= step
  }
  return formatDateTime(iso)
}
