// 后端错误的唯一整理出口：稳定错误码来自 Go 侧 internal/app/errors.go，
// 线上文本形状是 `code: 可展示消息[: 内部原因]`，前端按码判断，不解析中文。
import { t } from '../i18n'

export const APP_ERROR_CODES = [
  'empty_token',
  'catalog_invalid',
  'internal_error',
  'invalid_settings',
  'revision_conflict',
  'settings_save_failed',
  'config_schema_newer',
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
  for (const code of APP_ERROR_CODES) {
    const at = text.indexOf(`${code}: `)
    if (at !== -1) {
      const message = text.slice(at + code.length + 2).trim()
      return { code, message: message === '' ? code : message, cause: value }
    }
  }
  return { code: 'unknown', message: text === '' ? t('error.unknown') : text, cause: value }
}

export function getErrorMessage(value: unknown): string {
  return toAppError(value).message
}
