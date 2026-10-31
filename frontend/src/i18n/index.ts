// 可见文案的唯一出口：组件只用 t()，不自己拼句子。
// 登记的语言 = messages/ 目录下有 JSON 且配置校验放行的；新增语言是
// 「再加一个 JSON 并登记一行 + 后端 allowedLanguages 放行」，不改组件。
import zhCN from './messages/zh-CN.json'
import zhTW from './messages/zh-TW.json'

type Params = Record<string, string | number>

const bundles: Record<string, Readonly<Record<string, string>>> = {
  'zh-CN': zhCN,
  'zh-TW': zhTW,
}

let currentLocale = 'zh-CN'

export function setLocale(locale: string): boolean {
  if (!bundles[locale]) return false
  currentLocale = locale
  return true
}

export function getLocale(): string {
  return currentLocale
}

export function t(key: string, params?: Params): string {
  const bundle = bundles[currentLocale] ?? bundles['zh-CN']
  const template = bundle[key]
  // 缺键时把键名显示出来，不能显示成空文本或编一句兜底话。
  if (template === undefined) return `缺文案:${key}`
  if (!params) return template
  return template.replace(/\{(\w+)\}/g, (placeholder, name: string) =>
    name in params ? String(params[name]) : placeholder,
  )
}
