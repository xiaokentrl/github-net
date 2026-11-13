// 后端错误的唯一整理出口：稳定错误码来自 Go 侧 internal/app/errors.go，
// 线上文本形状是 `code: 可展示消息[: 内部原因]`，前端按码判断，不解析中文。
import { getLocale, isMissingTranslation, t } from '../i18n'

// 后端 internal/app 声明的稳定错误码，整份抄一遍——漏登的码摘不掉前缀，
// 界面上就会直接印出「hosts_failed: 没能打开 Hosts 文件」这种机器话。
// 两边不一致由 internal/app/error_codes_parity_test.go 这条门抓着。
export const APP_ERROR_CODES = [
  // 目录与配置
  'empty_token',
  'catalog_invalid',
  'internal_error',
  'invalid_settings',
  'revision_conflict',
  'settings_save_failed',
  'config_schema_newer',
  // 加速方法选择
  'unknown_scope',
  'strategy_unavailable',
  // 队列
  'invalid_task_id',
  'queue_running',
  'queue_busy',
  // 检测与应用/还原
  'invalid_item_id',
  'no_strategy_selected',
  'no_detect_result',
  'helper_unavailable',
  'hosts_failed',
  // 启动自启与平台能力
  'autostart_failed',
  'platform_unsupported',
  // 更新
  'update_bad_data',
  'update_handoff_failed',
  'update_rate_limited',
  'update_network',
  'update_no_release',
  'update_verify_failed',
  'update_busy',
] as const

export type AppErrorCode = (typeof APP_ERROR_CODES)[number] | 'unknown'

export type AppError = {
  code: AppErrorCode
  message: string
  cause: unknown
}

function textOf(value: unknown): string {
  if (value instanceof Error) return value.message
  if (typeof value === 'string') return value
  return String(value)
}

// toAppError 把调用异常整理成错误码和可展示文案；整理不出来时保留原文，
// 不能改成成功，也不能返回空对象假装没事。
export function toAppError(value: unknown): AppError {
  const text = textOf(value)
  // 先按行首匹配：码是这段文本的唯一开头，嵌在中间的「xxx: 」不该被认成码
  // （否则正文里引用另一个码会串认，按码翻译还会放大这个错）。
  // 全文兜底匹配留着接「外层包了一层前缀」的历史形状。
  for (const code of APP_ERROR_CODES) {
    if (text.startsWith(`${code}: `)) {
      const message = text.slice(code.length + 2).trim()
      return { code, message: message === '' ? code : message, cause: value }
    }
  }
  for (const code of APP_ERROR_CODES) {
    const at = text.indexOf(`${code}: `)
    if (at !== -1) {
      const message = text.slice(at + code.length + 2).trim()
      return { code, message: message === '' ? code : message, cause: value }
    }
  }
  return { code: 'unknown', message: text === '' ? t('error.unknown') : text, cause: value }
}

// getErrorMessage 按界面语言给一句可展示的话（对照《多语言实现方案》「用户可见错误从语言包出」）：
// 简体用户直接看后端带细节的原文（后端文案本就是简体，且同一码多种细节，转写反而丢信息）；
// 其余语言查 error.code.<码> 的语言包文案，缺键时回退后端原文——绝不显示「缺文案:」。
// errorText 给「只有码没有错误值」的场景（自动检查失败事件）按语言出文案：
// 简体直接用后端给的原文（同 getErrorMessage 的口径），其余语言查语言包，缺键回原文。
export function errorText(code: string, fallback: string): string {
  if (getLocale() === 'zh-CN' || code === '') return fallback
  const localized = t(`error.code.${code}`)
  return isMissingTranslation(localized) ? fallback : localized
}

export function getErrorMessage(value: unknown): string {
  const app = toAppError(value)
  if (app.code === 'unknown' || getLocale() === 'zh-CN') return app.message
  const localized = t(`error.code.${app.code}`)
  return isMissingTranslation(localized) ? app.message : localized
}
